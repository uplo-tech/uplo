package hostdb

import (
	"os"

	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
)

// hostdbTestDir creates a temporary testing directory for a hostdb test. This
// should only every be called once per test. Otherwise it will delete the
// directory again.
func hostdbTestDir(testName string) string {
	path := uplotest.TestDir("renter/hostdb", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}
