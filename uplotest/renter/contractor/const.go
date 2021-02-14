package contractor

import (
	"os"

	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
)

// contractorTestDir creates a temporary testing directory for a contractor
// test. This should only every be called once per test. Otherwise it will
// delete the directory again.
func contractorTestDir(testName string) string {
	path := uplotest.TestDir("renter/contractor", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}
