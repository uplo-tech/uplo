package host

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/uplomux"
)

const (
	// v120Host is the name of the file that contains the legacy host
	// persistence directory testdata.
	v120Host = "v120Host.tar.gz"
)

// TestV120HostUpgrade creates a host with a legacy persistence file,
// and then attempts to upgrade.
func TestV120HostUpgrade(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// ensure the host directory is empty
	persistDir := build.TempDir(modules.HostDir, t.Name())
	hostPersistDir := build.TempDir(modules.HostDir, t.Name(), modules.HostDir)
	err := os.RemoveAll(hostPersistDir)
	if err != nil {
		t.Fatal(err)
	}

	// copy the testdir legacy persistence data to the temp directory
	source := filepath.Join("testdata", v120Host)
	err = build.ExtractTarGz(source, persistDir)
	if err != nil {
		t.Fatal(err)
	}

	// simulate an existing uplomux in the persist dir.
	logFile, err := os.Create(filepath.Join(persistDir, "uplomux.log"))
	if err != nil {
		t.Fatal(err)
	}
	logger, err := persist.NewLogger(logFile)
	if err != nil {
		t.Fatal(err)
	}
	smux, err := uplomux.New("localhost:0", "localhost:0", logger.Logger, persistDir)
	if err != nil {
		t.Fatal(err)
	}
	err = smux.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = logFile.Close()
	if err != nil {
		t.Fatal(err)
	}

	// load a new host, the uplomux should be created in the uplo root.
	uploMuxDir := filepath.Join(persistDir, modules.UploMuxDir)
	closefn, host, err := loadExistingHostWithNewDeps(persistDir, uploMuxDir, hostPersistDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := closefn()
		if err != nil {
			t.Error(err)
		}
	}()

	// the old uplomux files should be gone.
	_, err1 := os.Stat(filepath.Join(persistDir, "uplomux.json"))
	_, err2 := os.Stat(filepath.Join(persistDir, "uplomux.json_temp"))
	_, err3 := os.Stat(filepath.Join(persistDir, "uplomux.log"))
	if !os.IsNotExist(err1) || !os.IsNotExist(err2) || !os.IsNotExist(err3) {
		t.Fatal("files still exist", err1, err2, err3)
	}

	// the new uplomux files should be in the right spot.
	_, err1 = os.Stat(filepath.Join(uploMuxDir, "uplomux.json"))
	_, err2 = os.Stat(filepath.Join(uploMuxDir, "uplomux.json_temp"))
	_, err3 = os.Stat(filepath.Join(uploMuxDir, "uplomux.log"))
	if err := errors.Compose(err1, err2, err3); err != nil {
		t.Fatal("files should exist", err1, err2, err3)
	}

	// verify the upgrade properly decorated the ephemeral account related
	// settings onto the persistence object
	his := host.InternalSettings()
	if his.EphemeralAccountExpiry != modules.DefaultEphemeralAccountExpiry {
		t.Fatal("EphemeralAccountExpiry not properly decorated on the persistence object after upgrade")
	}

	if !his.MaxEphemeralAccountBalance.Equals(modules.DefaultMaxEphemeralAccountBalance) {
		t.Fatal("MaxEphemeralAccountBalance not properly decorated on the persistence object after upgrade")
	}

	if !his.MaxEphemeralAccountRisk.Equals(defaultMaxEphemeralAccountRisk) {
		t.Fatal("MaxEphemeralAccountRisk not properly decorated on the persistence object after upgrade")
	}

	// sanity check the metadata version
	err = persist.LoadJSON(modules.Hostv151PersistMetadata, struct{}{}, filepath.Join(hostPersistDir, modules.HostSettingsFile))
	if err != nil {
		t.Fatal(err)
	}
}
