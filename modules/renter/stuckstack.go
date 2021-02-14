package renter

import (
	"sync"

	"github.com/uplo-tech/uplo/modules"
)

type (
	// stuckStack contains a LIFO stack of files that have had a stuck chunk
	// successfully repaired
	stuckStack struct {
		stack    []modules.UploPath
		uploPaths map[modules.UploPath]struct{}

		mu sync.Mutex
	}
)

// callNewStuckStack returns an initialized stuckStack
func callNewStuckStack() stuckStack {
	return stuckStack{
		stack:    make([]modules.UploPath, 0, maxSuccessfulStuckRepairFiles),
		uploPaths: make(map[modules.UploPath]struct{}),
	}
}

// managedLen returns the length of the stack
func (ss *stuckStack) managedLen() int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return len(ss.stack)
}

// managedPop returns the top element in the stack
func (ss *stuckStack) managedPop() (sp modules.UploPath) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Check that there are elements to return
	if len(ss.stack) == 0 {
		return
	}

	// Pop top element
	sp, ss.stack = ss.stack[len(ss.stack)-1], ss.stack[:len(ss.stack)-1]
	delete(ss.uploPaths, sp)
	return
}

// managedPush tries to add a file to the stack
func (ss *stuckStack) managedPush(uploPath modules.UploPath) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Check if there is room in the stack
	if len(ss.stack) >= maxSuccessfulStuckRepairFiles {
		// Prune oldest elements
		pruneToIndex := len(ss.stack) - maxSuccessfulStuckRepairFiles + 1
		ss.stack = ss.stack[pruneToIndex:]
	}

	// Check if the file is already being tracked
	if _, ok := ss.uploPaths[uploPath]; ok {
		// Remove the old entry from the array
		//
		// NOTE: currently just iterating over the array since the array is
		// known to be very small. If this changes in the future a heap or
		// linked list should be used in order to avoid this slow iteration
		for i, sp := range ss.stack {
			if !uploPath.Equals(sp) {
				continue
			}
			ss.stack = append(ss.stack[:i], ss.stack[i+1:]...)
			break
		}
	}

	// Add file to the stack
	ss.stack = append(ss.stack, uploPath)
	ss.uploPaths[uploPath] = struct{}{}
	return
}
