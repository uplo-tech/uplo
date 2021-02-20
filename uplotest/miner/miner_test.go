package miner

import (
	"testing"

	"github.com/uplo-tech/uplo/uplotest"

	"github.com/uplo-tech/uplo/node"
)

// TestMinerEmptyBlock tests if a miner can mine and submit an empty block.
func TestMinerEmptyBlock(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	// Create a miner for testing.
	m, err := uplotest.NewNode(node.AllModules(minerTestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	// Get the current blockheight.
	bh, err := m.BlockHeight()
	if err != nil {
		t.Fatal()
	}
	// Mine an empty block and submit it.
	if err := m.MineEmptyBlock(); err != nil {
		t.Fatal(err)
	}
	// Blockheight should have increased by 1.
	newBH, err := m.BlockHeight()
	if err != nil {
		t.Fatal()
	}
	if newBH != bh+1 {
		t.Fatalf("new blockheight should be %v but was %v", bh+1, newBH)
	}
}
