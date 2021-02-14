package contractmanager

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
)

// TestLoadWAL tests loading an existing wal.
func TestLoadWAL(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Load legacy wal.
	wal, err := ioutil.ReadFile("../../../persist/testdata/154hostwal.wal")
	if err != nil {
		t.Fatal(err)
	}

	// Prepare a test dir.
	testdir := build.TempDir(modules.ContractManagerDir, t.Name())
	err = os.MkdirAll(testdir, persist.DefaultDiskPermissionsTest)
	if err != nil {
		t.Fatal(err)
	}

	// Store wal in persist dir.
	dstPath := filepath.Join(testdir, walFile)
	err = ioutil.WriteFile(dstPath, wal, persist.DefaultDiskPermissionsTest)
	if err != nil {
		t.Fatal(err)
	}

	// Start contract manager with existing wal.
	cm, err := New(testdir)
	if err != nil {
		t.Fatal(err)
	}
	err = cm.Close()
	if err != nil {
		t.Fatal(err)
	}
}
