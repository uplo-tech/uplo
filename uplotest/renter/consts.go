package renter

import (
	"os"

	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
)

// renterTestDir creates a temporary testing directory for a renter test. This
// should only every be called once per test. Otherwise it will delete the
// directory again.
func renterTestDir(testName string) string {
	path := uplotest.TestDir("renter", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}

// fuseTestDir creates a temporary testing directory for a fuse test. This
// should only every be called once per test. Otherwise it will delete the
// directory again.
func fuseTestDir(testName string) string {
	path := uplotest.TestDir("fuse", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}
