package renter

import (
	"strconv"
	"testing"

	"github.com/uplo-tech/uplo/modules"
)

// TestStuckStack probes the implementation of the stuck stack
func TestStuckStack(t *testing.T) {
	stack := stuckStack{
		stack:    make([]modules.UploPath, 0, maxSuccessfulStuckRepairFiles),
		uploPaths: make(map[modules.UploPath]struct{}),
	}

	// Check stack initialized as expected
	if stack.managedLen() != 0 {
		t.Fatal("Expected length of 0 got", stack.managedLen())
	}

	// Create some UploPaths to add to the stack
	sp1, _ := modules.NewUploPath("uploPath1")
	sp2, _ := modules.NewUploPath("uploPath2")

	// Test pushing 1 uplopath onto stack
	stack.managedPush(sp1)
	if stack.managedLen() != 1 {
		t.Fatal("Expected length of 1 got", stack.managedLen())
	}
	uploPath := stack.managedPop()
	if !uploPath.Equals(sp1) {
		t.Log("uploPath:", uploPath)
		t.Log("sp1:", sp1)
		t.Fatal("UploPaths not equal")
	}
	if stack.managedLen() != 0 {
		t.Fatal("Expected length of 0 got", stack.managedLen())
	}

	// Test adding multiple uploPaths to stack
	stack.managedPush(sp1)
	stack.managedPush(sp2)
	if stack.managedLen() != 2 {
		t.Fatal("Expected length of 2 got", stack.managedLen())
	}
	// Last uplopath added should be returned
	uploPath = stack.managedPop()
	if !uploPath.Equals(sp2) {
		t.Log("uploPath:", uploPath)
		t.Log("sp2:", sp2)
		t.Fatal("UploPaths not equal")
	}

	// Pushing first uplopath again should result in moving it to the top
	stack.managedPush(sp2)
	stack.managedPush(sp1)
	if stack.managedLen() != 2 {
		t.Fatal("Expected length of 2 got", stack.managedLen())
	}
	uploPath = stack.managedPop()
	if !uploPath.Equals(sp1) {
		t.Log("uploPath:", uploPath)
		t.Log("sp1:", sp1)
		t.Fatal("UploPaths not equal")
	}

	// Length should never exceed maxSuccessfulStuckRepairFiles
	for i := 0; i < 2*maxSuccessfulStuckRepairFiles; i++ {
		sp, _ := modules.NewUploPath(strconv.Itoa(i))
		stack.managedPush(sp)
		if stack.managedLen() > maxSuccessfulStuckRepairFiles {
			t.Fatalf("Length exceeded %v, %v", maxSuccessfulStuckRepairFiles, stack.managedLen())
		}
	}
}
