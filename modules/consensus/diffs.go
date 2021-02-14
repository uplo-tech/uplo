package consensus

import (
	"errors"

	"github.com/uplo-tech/bolt"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

var (
	errApplyUplofundPoolDiffMismatch  = errors.New("committing a uplofund pool diff with an invalid 'previous' field")
	errDiffsNotGenerated             = errors.New("applying diff set before generating errors")
	errInvalidSuccessor              = errors.New("generating diffs for a block that's an invalid successsor to the current block")
	errNegativePoolAdjustment        = errors.New("committing a uplofund pool diff with a negative adjustment")
	errNonApplyUplofundPoolDiff       = errors.New("committing a uplofund pool diff that doesn't have the 'apply' direction")
	errRevertUplofundPoolDiffMismatch = errors.New("committing a uplofund pool diff with an invalid 'adjusted' field")
	errWrongAppliedDiffSet           = errors.New("applying a diff set that isn't the current block")
	errWrongRevertDiffSet            = errors.New("reverting a diff set that isn't the current block")
)

// commitDiffSetSanity performs a series of sanity checks before committing a
// diff set.
func commitDiffSetSanity(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	// This function is purely sanity checks.
	if !build.DEBUG {
		return
	}

	// Diffs should have already been generated for this node.
	if !pb.DiffsGenerated {
		panic(errDiffsNotGenerated)
	}

	// Current node must be the input node's parent if applying, and
	// current node must be the input node if reverting.
	if dir == modules.DiffApply {
		parent, err := getBlockMap(tx, pb.Block.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		if parent.Block.ID() != currentBlockID(tx) {
			panic(errWrongAppliedDiffSet)
		}
	} else {
		if pb.Block.ID() != currentBlockID(tx) {
			panic(errWrongRevertDiffSet)
		}
	}
}

// commitUplocoinOutputDiff applies or reverts a UplocoinOutputDiff.
func commitUplocoinOutputDiff(tx *bolt.Tx, scod modules.UplocoinOutputDiff, dir modules.DiffDirection) {
	if scod.Direction == dir {
		addUplocoinOutput(tx, scod.ID, scod.UplocoinOutput)
	} else {
		removeUplocoinOutput(tx, scod.ID)
	}
}

// commitFileContractDiff applies or reverts a FileContractDiff.
func commitFileContractDiff(tx *bolt.Tx, fcd modules.FileContractDiff, dir modules.DiffDirection) {
	if fcd.Direction == dir {
		addFileContract(tx, fcd.ID, fcd.FileContract)
	} else {
		removeFileContract(tx, fcd.ID)
	}
}

// commitUplofundOutputDiff applies or reverts a Uplofund output diff.
func commitUplofundOutputDiff(tx *bolt.Tx, sfod modules.UplofundOutputDiff, dir modules.DiffDirection) {
	if sfod.Direction == dir {
		addUplofundOutput(tx, sfod.ID, sfod.UplofundOutput)
	} else {
		removeUplofundOutput(tx, sfod.ID)
	}
}

// commitDelayedUplocoinOutputDiff applies or reverts a delayedUplocoinOutputDiff.
func commitDelayedUplocoinOutputDiff(tx *bolt.Tx, dscod modules.DelayedUplocoinOutputDiff, dir modules.DiffDirection) {
	if dscod.Direction == dir {
		addDSCO(tx, dscod.MaturityHeight, dscod.ID, dscod.UplocoinOutput)
	} else {
		removeDSCO(tx, dscod.MaturityHeight, dscod.ID)
	}
}

// commitUplofundPoolDiff applies or reverts a UplofundPoolDiff.
func commitUplofundPoolDiff(tx *bolt.Tx, sfpd modules.UplofundPoolDiff, dir modules.DiffDirection) {
	// Sanity check - uplofund pool should only ever increase.
	if build.DEBUG {
		if sfpd.Adjusted.Cmp(sfpd.Previous) < 0 {
			panic(errNegativePoolAdjustment)
		}
		if sfpd.Direction != modules.DiffApply {
			panic(errNonApplyUplofundPoolDiff)
		}
	}

	if dir == modules.DiffApply {
		// Sanity check - sfpd.Previous should equal the current uplofund pool.
		if build.DEBUG && !getUplofundPool(tx).Equals(sfpd.Previous) {
			panic(errApplyUplofundPoolDiffMismatch)
		}
		setUplofundPool(tx, sfpd.Adjusted)
	} else {
		// Sanity check - sfpd.Adjusted should equal the current uplofund pool.
		if build.DEBUG && !getUplofundPool(tx).Equals(sfpd.Adjusted) {
			panic(errRevertUplofundPoolDiffMismatch)
		}
		setUplofundPool(tx, sfpd.Previous)
	}
}

// createUpcomingDelayeOutputdMaps creates the delayed Uplocoin output maps that
// will be used when applying delayed Uplocoin outputs in the diff set.
func createUpcomingDelayedOutputMaps(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		createDSCOBucket(tx, pb.Height+types.MaturityDelay)
	} else if pb.Height >= types.MaturityDelay {
		createDSCOBucket(tx, pb.Height)
	}
}

// commitNodeDiffs commits all of the diffs in a block node.
func commitNodeDiffs(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		for _, scod := range pb.UplocoinOutputDiffs {
			commitUplocoinOutputDiff(tx, scod, dir)
		}
		for _, fcd := range pb.FileContractDiffs {
			commitFileContractDiff(tx, fcd, dir)
		}
		for _, sfod := range pb.UplofundOutputDiffs {
			commitUplofundOutputDiff(tx, sfod, dir)
		}
		for _, dscod := range pb.DelayedUplocoinOutputDiffs {
			commitDelayedUplocoinOutputDiff(tx, dscod, dir)
		}
		for _, sfpd := range pb.UplofundPoolDiffs {
			commitUplofundPoolDiff(tx, sfpd, dir)
		}
	} else {
		for i := len(pb.UplocoinOutputDiffs) - 1; i >= 0; i-- {
			commitUplocoinOutputDiff(tx, pb.UplocoinOutputDiffs[i], dir)
		}
		for i := len(pb.FileContractDiffs) - 1; i >= 0; i-- {
			commitFileContractDiff(tx, pb.FileContractDiffs[i], dir)
		}
		for i := len(pb.UplofundOutputDiffs) - 1; i >= 0; i-- {
			commitUplofundOutputDiff(tx, pb.UplofundOutputDiffs[i], dir)
		}
		for i := len(pb.DelayedUplocoinOutputDiffs) - 1; i >= 0; i-- {
			commitDelayedUplocoinOutputDiff(tx, pb.DelayedUplocoinOutputDiffs[i], dir)
		}
		for i := len(pb.UplofundPoolDiffs) - 1; i >= 0; i-- {
			commitUplofundPoolDiff(tx, pb.UplofundPoolDiffs[i], dir)
		}
	}
}

// deleteObsoleteDelayedOutputMaps deletes the delayed Uplocoin output maps that
// are no longer in use.
func deleteObsoleteDelayedOutputMaps(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	// There are no outputs that mature in the first MaturityDelay blocks.
	if dir == modules.DiffApply && pb.Height >= types.MaturityDelay {
		deleteDSCOBucket(tx, pb.Height)
	} else if dir == modules.DiffRevert {
		deleteDSCOBucket(tx, pb.Height+types.MaturityDelay)
	}
}

// updateCurrentPath updates the current path after applying a diff set.
func updateCurrentPath(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	// Update the current path.
	if dir == modules.DiffApply {
		pushPath(tx, pb.Block.ID())
	} else {
		popPath(tx)
	}
}

// commitFoundationUpdate updates the current Foundation unlock hashes in
// accordance with the specified block and direction.
//
// Because these updates do not have associated diffs, we cannot apply multiple
// updates per block. Instead, we apply the first update and ignore the rest.
func commitFoundationUpdate(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		for i := range pb.Block.Transactions {
			applyArbitraryData(tx, pb, pb.Block.Transactions[i])
		}
	} else {
		// Look for a set of prior unlock hashes for this height.
		primary, failsafe, exists := getPriorFoundationUnlockHashes(tx, pb.Height)
		if exists {
			setFoundationUnlockHashes(tx, primary, failsafe)
			deletePriorFoundationUnlockHashes(tx, pb.Height)
			transferFoundationOutputs(tx, pb.Height, primary)
		}
	}
}

// commitDiffSet applies or reverts the diffs in a blockNode.
func commitDiffSet(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	// Sanity checks - there are a few so they were moved to another function.
	if build.DEBUG {
		commitDiffSetSanity(tx, pb, dir)
	}

	createUpcomingDelayedOutputMaps(tx, pb, dir)
	commitNodeDiffs(tx, pb, dir)
	deleteObsoleteDelayedOutputMaps(tx, pb, dir)
	commitFoundationUpdate(tx, pb, dir)
	updateCurrentPath(tx, pb, dir)
}

// generateAndApplyDiff will verify the block and then integrate it into the
// consensus state. These two actions must happen at the same time because
// transactions are allowed to depend on each other. We can't be sure that a
// transaction is valid unless we have applied all of the previous transactions
// in the block, which means we need to apply while we verify.
func generateAndApplyDiff(tx *bolt.Tx, pb *processedBlock) error {
	// Sanity check - the block being applied should have the current block as
	// a parent.
	if build.DEBUG && pb.Block.ParentID != currentBlockID(tx) {
		panic(errInvalidSuccessor)
	}

	// Create the bucket to hold all of the delayed Uplocoin outputs created by
	// transactions this block. Needs to happen before any transactions are
	// applied.
	createDSCOBucket(tx, pb.Height+types.MaturityDelay)

	// Validate and apply each transaction in the block. They cannot be
	// validated all at once because some transactions may not be valid until
	// previous transactions have been applied.
	for _, txn := range pb.Block.Transactions {
		err := validTransaction(tx, txn)
		if err != nil {
			return err
		}
		applyTransaction(tx, pb, txn)
	}

	// After all of the transactions have been applied, 'maintenance' is
	// applied on the block. This includes adding any outputs that have reached
	// maturity, applying any contracts with missed storage proofs, and adding
	// the miner payouts and Foundation subsidy to the list of delayed outputs.
	applyMaintenance(tx, pb)

	// DiffsGenerated are only set to true after the block has been fully
	// validated and integrated. This is required to prevent later blocks from
	// being accepted on top of an invalid block - if the consensus set ever
	// forks over an invalid block, 'DiffsGenerated' will be set to 'false',
	// requiring validation to occur again. when 'DiffsGenerated' is set to
	// true, validation is skipped, therefore the flag should only be set to
	// true on fully validated blocks.
	pb.DiffsGenerated = true

	// Add the block to the current path and block map.
	bid := pb.Block.ID()
	blockMap := tx.Bucket(BlockMap)
	updateCurrentPath(tx, pb, modules.DiffApply)

	// Sanity check preparation - set the consensus hash at this height so that
	// during reverting a check can be performed to assure consistency when
	// adding and removing blocks. Must happen after the block is added to the
	// path.
	if build.DEBUG {
		pb.ConsensusChecksum = consensusChecksum(tx)
	}

	return blockMap.Put(bid[:], encoding.Marshal(*pb))
}
