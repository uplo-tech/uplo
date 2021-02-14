package host

import (
	"os"

	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
)

// hostTestDir creates a temporary testing directory for a host. This should
// only every be called once per test. Otherwise it will delete the directory
// again.
func hostTestDir(testName string) string {
	path := uplotest.TestDir("host", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}
