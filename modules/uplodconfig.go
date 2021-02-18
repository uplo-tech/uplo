package modules

import (
	"errors"
	"os"
	"sync"

	"github.com/uplo-tech/ratelimit"

	"github.com/uplo-tech/uplo/persist"
)

type (
	// uplodConfig is a helper type to manage the global uplod config.
	UplodConfig struct {
		// Ratelimit related fields
		ReadBPS            int64  `json:"readbps"`
		WriteBPSDeprecated int64  `json:"writeps,uplomismatch"`
		WriteBPS           int64  `json:"writebps"`
		PacketSize         uint64 `json:"packetsize"`

		// path of config on disk.
		path string
		mu   sync.Mutex
	}
)

var (
	// GlobalRateLimits is the global object for regulating ratelimits
	// throughout uplod. It is set using the gateway module.
	GlobalRateLimits = ratelimit.NewRateLimit(0, 0, 0)

	configMetadata = persist.Metadata{
		Header:  "uplod.config",
		Version: "1.0.0",
	}

	// ConfigName is the name of the config file on disk
	ConfigName = "uplod.config"
)

// SetRatelimit sets the ratelimit related fields in the config and persists it
// to disk.
func (cfg *UplodConfig) SetRatelimit(readBPS, writeBPS int64) error {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	// Input validation.
	if readBPS < 0 || writeBPS < 0 {
		return errors.New("download/upload rate can't be below 0")
	}
	// Check for sentinel "no limits" value.
	if readBPS == 0 && writeBPS == 0 {
		GlobalRateLimits.SetLimits(0, 0, 0)
	} else {
		GlobalRateLimits.SetLimits(readBPS, writeBPS, 0)
	}
	// Persist settings.
	cfg.ReadBPS, cfg.WriteBPS, cfg.PacketSize = GlobalRateLimits.Limits()
	return cfg.save()
}

// save saves the config to disk.
func (cfg *UplodConfig) save() error {
	return persist.SaveJSON(configMetadata, cfg, cfg.path)
}

// load loads the config from disk.
func (cfg *UplodConfig) load(path string) error {
	defer cfg.writeBPSCompat()
	return persist.LoadJSON(configMetadata, cfg, path)
}

// writeBPSCompat is compatibility code for addressing the the incorrect json
// tag upgrade from `writeps` to `writebps`
func (cfg *UplodConfig) writeBPSCompat() {
	// If the deprecated tag field is none zero and the new field is still zero,
	// set the new field
	if cfg.WriteBPSDeprecated != 0 && cfg.WriteBPS == 0 {
		cfg.WriteBPS = cfg.WriteBPSDeprecated
	}
	// Zero out the old field as to not overwrite a value in the future.
	cfg.WriteBPSDeprecated = 0
}

// NewConfig loads a config from disk or creates a new one if no config exists
// yet.
func NewConfig(path string) (*UplodConfig, error) {
	var cfg UplodConfig
	cfg.path = path
	// Try loading the config from disk first.
	err := cfg.load(cfg.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	} else if os.IsNotExist(err) {
		// Otherwise init with default values.
		cfg.ReadBPS = 0    // unlimited
		cfg.WriteBPS = 0   // unlimited
		cfg.PacketSize = 0 // unlimited
	}
	// Init the global ratelimit.
	GlobalRateLimits.SetLimits(cfg.ReadBPS, cfg.WriteBPS, cfg.PacketSize)
	return &cfg, nil
}
