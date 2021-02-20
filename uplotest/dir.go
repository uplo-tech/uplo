package uplotest

import (
	"os"
	"path/filepath"

	"github.com/uplo-tech/uplo/persist"
)

var (
	// UploTestingDir is the directory that contains all of the files and
	// folders created during testing.
	UploTestingDir = filepath.Join(os.TempDir(), "UploTesting")
)

// TestDir joins the provided directories and prefixes them with the Uplo
// testing directory, removing any files or directories that previously existed
// at that location.
func TestDir(dirs ...string) string {
	path := filepath.Join(UploTestingDir, "uplotest", filepath.Join(dirs...))
	err := os.RemoveAll(path)
	if err != nil {
		panic(err)
	}
	return path
}

// uplotestTestDir creates a testing directory for tests within the uplotest
// module.
func uplotestTestDir(testName string) string {
	path := TestDir("uplotest", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}

// DownloadDir returns the LocalDir that is the testnodes download directory
func (tn *TestNode) DownloadDir() *LocalDir {
	return tn.downloadDir
}

// FilesDir returns the LocalDir that is the testnodes upload directory
func (tn *TestNode) FilesDir() *LocalDir {
	return tn.filesDir
}

// RenterDir returns the renter directory for the renter
func (tn *TestNode) RenterDir() string {
	return filepath.Join(tn.Dir, "renter")
}

// RenterFilesDir returns the renter's files directory
func (tn *TestNode) RenterFilesDir() string {
	return filepath.Join(tn.RenterDir(), "uplofiles")
}
