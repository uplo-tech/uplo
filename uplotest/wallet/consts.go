package wallet

import (
	"os"

	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
)

// walletTestDir creates a temporary testing directory for a wallet test. This
// should only every be called once per test. Otherwise it will delete the
// directory again.
func walletTestDir(testName string) string {
	path := uplotest.TestDir("wallet", testName)
	if err := os.MkdirAll(path, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return path
}
