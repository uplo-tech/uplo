package modules

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/uplomux"
	"github.com/uplo-tech/uplomux/mux"
)

const (
	// logfile is the filename of the uplomux log file
	logfile = "uplomux.log"

	// UploMuxDir is the name of the uplomux dir
	UploMuxDir = "uplomux"
)

// NewUploMux returns a new UploMux object
func NewUploMux(uploMuxDir, uplodir, tcpaddress, wsaddress string) (*uplomux.UploMux, error) {
	// can't use relative path
	if !filepath.IsAbs(uploMuxDir) || !filepath.IsAbs(uplodir) {
		err := errors.New("paths need to be absolute")
		build.Critical(err)
		return nil, err
	}

	// ensure the persist directory exists
	err := os.MkdirAll(uploMuxDir, 0700)
	if err != nil {
		return nil, err
	}

	// CompatV143 migrate existing mux in uplodir root to uploMuxDir.
	if err := compatV143MigrateUploMux(uploMuxDir, uplodir); err != nil {
		return nil, err
	}

	// create a logger
	file, err := os.OpenFile(filepath.Join(uploMuxDir, logfile), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	logger, err := persist.NewLogger(file)
	if err != nil {
		return nil, err
	}

	// create a uplomux, if the host's persistence file is at v120 we want to
	// recycle the host's key pair to use in the uplomux
	pubKey, privKey, compat := compatLoadKeysFromHost(uplodir)
	if compat {
		return uplomux.CompatV1421NewWithKeyPair(tcpaddress, wsaddress, logger.Logger, uploMuxDir, privKey, pubKey)
	}
	return uplomux.New(tcpaddress, wsaddress, logger.Logger, uploMuxDir)
}

// UploPKToMuxPK turns a UploPublicKey into a mux.ED25519PublicKey
func UploPKToMuxPK(spk types.UploPublicKey) (mk mux.ED25519PublicKey) {
	// Sanity check key length
	if len(spk.Key) != len(mk) {
		panic("Expected the given UploPublicKey to have a length equal to the mux.ED25519PublicKey length")
	}
	copy(mk[:], spk.Key)
	return
}

// compatLoadKeysFromHost will try and load the host's keypair from its
// persistence file. It tries all host metadata versions before v143. From that
// point on, the uplomux was introduced and will already have a correct set of
// keys persisted in its persistence file. Only for hosts upgrading to v143 we
// want to recycle the host keys in the uplomux.
func compatLoadKeysFromHost(persistDir string) (pubKey mux.ED25519PublicKey, privKey mux.ED25519SecretKey, compat bool) {
	persistPath := filepath.Join(persistDir, HostDir, HostSettingsFile)

	historicMetadata := []persist.Metadata{
		Hostv120PersistMetadata,
		Hostv112PersistMetadata,
	}

	// Try to load the host's key pair from its persistence file, we try all
	// metadata version up until v143
	hk := struct {
		PublicKey types.UploPublicKey `json:"publickey"`
		SecretKey crypto.SecretKey   `json:"secretkey"`
	}{}
	for _, metadata := range historicMetadata {
		err := persist.LoadJSON(metadata, &hk, persistPath)
		if err == nil {
			copy(pubKey[:], hk.PublicKey.Key[:])
			copy(privKey[:], hk.SecretKey[:])
			compat = true
			return
		}
	}

	compat = false
	return
}

// compatV143MigrateUploMux migrates the UploMux from the root dir of the uplo data
// dir to the uplomux subdir.
func compatV143MigrateUploMux(uploMuxDir, uplodir string) error {
	oldPath := filepath.Join(uplodir, "uplomux.json")
	newPath := filepath.Join(uploMuxDir, "uplomux.json")
	oldPathTmp := filepath.Join(uplodir, "uplomux.json_temp")
	newPathTmp := filepath.Join(uploMuxDir, "uplomux.json_temp")
	oldPathLog := filepath.Join(uplodir, logfile)
	newPathLog := filepath.Join(uploMuxDir, logfile)
	_, errOld := os.Stat(oldPath)
	_, errNew := os.Stat(newPath)

	// Migrate if old file exists but no file at new location exists yet.
	migrated := false
	if errOld == nil && os.IsNotExist(errNew) {
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		migrated = true
	}
	// If no migration is necessary we are done.
	if !migrated {
		return nil
	}
	// If we migrated the main files, also migrate the tmp files if available.
	if err := os.Rename(oldPathTmp, newPathTmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Also migrate the log file.
	if err := os.Rename(oldPathLog, newPathLog); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
