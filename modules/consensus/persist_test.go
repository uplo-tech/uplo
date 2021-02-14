package consensus

import (
	"path/filepath"
	"testing"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/gateway"
)

// TestSaveLoad populates a blockchain, saves it, loads it, and checks
// the consensus set hash before and after
func TestSaveLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	cst.testBlockSuite()
	oldHash := cst.cs.dbConsensusChecksum()
	cst.cs.Close()

	// Reassigning this will lose subscribers and such, but we
	// just want to call load and get a hash
	g, err := gateway.New("localhost:0", false, build.TempDir(modules.ConsensusDir, t.Name(), modules.GatewayDir))
	if err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(build.UploTestingDir, modules.ConsensusDir, t.Name(), modules.ConsensusDir)
	var errChan <-chan error
	cst.cs, errChan = New(g, false, d)
	if err := <-errChan; err != nil {
		t.Fatal(err)
	}
	newHash := cst.cs.dbConsensusChecksum()
	if oldHash != newHash {
		t.Fatal("consensus set hash changed after load")
	}
}
