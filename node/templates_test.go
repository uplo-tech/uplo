package node

import (
	"testing"
	"time"

	"github.com/uplo-tech/uplo/build"
)

// TestNew is a basic smoke test for New that uses all of the templates to
// verify a working New function.
func TestNew(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Test AllModulesTemplate.
	dir := build.TempDir("node", t.Name()+"-AllModulesTemplate")
	n, errChan := New(AllModules(dir), time.Now())
	if err := <-errChan; err != nil {
		t.Fatal(err)
	}
	if n.Gateway == nil {
		t.Error("gateway not set correctly")
	}
	if n.ConsensusSet == nil {
		t.Error("consensus set not set correctly")
	}
	if n.Explorer == nil {
		// TODO: Add the explorer to the node package.
		t.Log("Need to add the explorer to the UploNode framework.")
	}
	if n.TransactionPool == nil {
		t.Error("transaction pool not set correctly")
	}
	if n.Wallet == nil {
		t.Error("wallet not set correctly")
	}
	if n.Host == nil {
		t.Error("host not set correctly")
	}
	if n.Renter == nil {
		t.Error("renter not set correctly")
	}
	if n.Miner == nil {
		t.Error("miner not set correctly")
	}
	err := n.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Test WalletTemplate.
	dir = build.TempDir("node", t.Name()+"-WalletTemplate")
	n, errChan = New(Wallet(dir), time.Now())
	if err := <-errChan; err != nil {
		t.Fatal(err)
	}
	if n.Gateway == nil {
		t.Error("gateway not set correctly")
	}
	if n.ConsensusSet == nil {
		t.Error("consensus set not set correctly")
	}
	if n.Explorer != nil {
		t.Error("explorer should not be created when using the wallet template")
	}
	if n.TransactionPool == nil {
		t.Error("transaction pool not set correctly")
	}
	if n.Wallet == nil {
		t.Error("wallet not set correctly")
	}
	if n.Host != nil {
		t.Error("host should not be created when using the wallet template")
	}
	if n.Renter != nil {
		t.Error("renter should not be created when using the wallet template")
	}
	if n.Miner != nil {
		t.Error("miner should not be created when using the wallet template")
	}
	err = n.Close()
	if err != nil {
		t.Fatal(err)
	}
}
