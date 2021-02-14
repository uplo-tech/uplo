package consensus

// applytransaction.go handles applying a transaction to the consensus set.
// There is an assumption that the transaction has already been verified.

import (
	"bytes"

	"github.com/uplo-tech/bolt"
	"github.com/uplo-tech/encoding"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// applyUplocoinInputs takes all of the Uplocoin inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applyUplocoinInputs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	// Remove all Uplocoin inputs from the unspent Uplocoin outputs list.
	for _, sci := range t.UplocoinInputs {
		sco, err := getUplocoinOutput(tx, sci.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		scod := modules.UplocoinOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sci.ParentID,
			UplocoinOutput: sco,
		}
		pb.UplocoinOutputDiffs = append(pb.UplocoinOutputDiffs, scod)
		commitUplocoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

// applyUplocoinOutputs takes all of the Uplocoin outputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applyUplocoinOutputs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	// Add all Uplocoin outputs to the unspent Uplocoin outputs list.
	for i, sco := range t.UplocoinOutputs {
		scoid := t.UplocoinOutputID(uint64(i))
		scod := modules.UplocoinOutputDiff{
			Direction:     modules.DiffApply,
			ID:            scoid,
			UplocoinOutput: sco,
		}
		pb.UplocoinOutputDiffs = append(pb.UplocoinOutputDiffs, scod)
		commitUplocoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

// applyFileContracts iterates through all of the file contracts in a
// transaction and applies them to the state, updating the diffs in the proccesed
// block.
func applyFileContracts(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for i, fc := range t.FileContracts {
		fcid := t.FileContractID(uint64(i))
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcid,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)

		// Get the portion of the contract that goes into the uplofund pool and
		// add it to the uplofund pool.
		sfp := getUplofundPool(tx)
		sfpd := modules.UplofundPoolDiff{
			Direction: modules.DiffApply,
			Previous:  sfp,
			Adjusted:  sfp.Add(types.Tax(blockHeight(tx), fc.Payout)),
		}
		pb.UplofundPoolDiffs = append(pb.UplofundPoolDiffs, sfpd)
		commitUplofundPoolDiff(tx, sfpd, modules.DiffApply)
	}
}

// applyFileContractRevisions iterates through all of the file contract
// revisions in a transaction and applies them to the state, updating the diffs
// in the processed block.
func applyFileContractRevisions(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for _, fcr := range t.FileContractRevisions {
		fc, err := getFileContract(tx, fcr.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}

		// Add the diff to delete the old file contract.
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           fcr.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)

		// Add the diff to add the revised file contract.
		newFC := types.FileContract{
			FileSize:           fcr.NewFileSize,
			FileMerkleRoot:     fcr.NewFileMerkleRoot,
			WindowStart:        fcr.NewWindowStart,
			WindowEnd:          fcr.NewWindowEnd,
			Payout:             fc.Payout,
			ValidProofOutputs:  fcr.NewValidProofOutputs,
			MissedProofOutputs: fcr.NewMissedProofOutputs,
			UnlockHash:         fcr.NewUnlockHash,
			RevisionNumber:     fcr.NewRevisionNumber,
		}
		fcd = modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcr.ParentID,
			FileContract: newFC,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)
	}
}

// applyTxStorageProofs iterates through all of the storage proofs in a
// transaction and applies them to the state, updating the diffs in the processed
// block.
func applyStorageProofs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for _, sp := range t.StorageProofs {
		fc, err := getFileContract(tx, sp.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}

		// Add all of the outputs in the ValidProofOutputs of the contract.
		for i, vpo := range fc.ValidProofOutputs {
			spoid := sp.ParentID.StorageProofOutputID(types.ProofValid, uint64(i))
			dscod := modules.DelayedUplocoinOutputDiff{
				Direction:      modules.DiffApply,
				ID:             spoid,
				UplocoinOutput:  vpo,
				MaturityHeight: pb.Height + types.MaturityDelay,
			}
			pb.DelayedUplocoinOutputDiffs = append(pb.DelayedUplocoinOutputDiffs, dscod)
			commitDelayedUplocoinOutputDiff(tx, dscod, modules.DiffApply)
		}

		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           sp.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)
	}
}

// applyTxUplofundInputs takes all of the uplofund inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applyUplofundInputs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for _, sfi := range t.UplofundInputs {
		// Calculate the volume of Uplocoins to put in the claim output.
		sfo, err := getUplofundOutput(tx, sfi.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		claimPortion := getUplofundPool(tx).Sub(sfo.ClaimStart).Div(types.UplofundCount).Mul(sfo.Value)

		// Add the claim output to the delayed set of outputs.
		sco := types.UplocoinOutput{
			Value:      claimPortion,
			UnlockHash: sfi.ClaimUnlockHash,
		}
		sfoid := sfi.ParentID.uploclaimOutputID()
		dscod := modules.DelayedUplocoinOutputDiff{
			Direction:      modules.DiffApply,
			ID:             sfoid,
			UplocoinOutput:  sco,
			MaturityHeight: pb.Height + types.MaturityDelay,
		}
		pb.DelayedUplocoinOutputDiffs = append(pb.DelayedUplocoinOutputDiffs, dscod)
		commitDelayedUplocoinOutputDiff(tx, dscod, modules.DiffApply)

		// Create the uplofund output diff and remove the output from the
		// consensus set.
		sfod := modules.UplofundOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sfi.ParentID,
			UplofundOutput: sfo,
		}
		pb.UplofundOutputDiffs = append(pb.UplofundOutputDiffs, sfod)
		commitUplofundOutputDiff(tx, sfod, modules.DiffApply)
	}
}

// applyUplofundOutputs applies a uplofund output to the consensus set.
func applyUplofundOutputs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for i, sfo := range t.UplofundOutputs {
		sfoid := t.UplofundOutputID(uint64(i))
		sfo.ClaimStart = getUplofundPool(tx)
		sfod := modules.UplofundOutputDiff{
			Direction:     modules.DiffApply,
			ID:            sfoid,
			UplofundOutput: sfo,
		}
		pb.UplofundOutputDiffs = append(pb.UplofundOutputDiffs, sfod)
		commitUplofundOutputDiff(tx, sfod, modules.DiffApply)
	}
}

// applyArbitraryData applies arbitrary data to the consensus set. ArbitraryData
// is a field of the Transaction type whose structure is not fixed. This means
// that, via hardfork, new types of transaction can be introduced with minimal
// breakage by updating consensus code to recognize and act upon values encoded
// within the ArbitraryData field.
//
// Accordingly, this function dispatches on the various ArbitraryData values
// that are recognized by consensus. Currently, types.FoundationUnlockHashUpdate
// is the only recognized value.
func applyArbitraryData(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	// No ArbitraryData values were recognized prior to the Foundation hardfork.
	if pb.Height < types.FoundationHardforkHeight {
		return
	}
	for _, arb := range t.ArbitraryData {
		if bytes.HasPrefix(arb, types.SpecifierFoundation[:]) {
			var update types.FoundationUnlockHashUpdate
			err := encoding.Unmarshal(arb[types.SpecifierLen:], &update)
			if build.DEBUG && err != nil {
				// (Transaction).StandaloneValid ensures that decoding will not fail
				panic(err)
			}
			// Apply the update. First, save a copy of the old (i.e. current)
			// unlock hashes, so that we can revert later. Then set the new
			// unlock hashes.
			//
			// Importantly, we must only do this once per block; otherwise, for
			// complicated reasons involving diffs, we would not be able to
			// revert updates safely. So if we see that a copy has already been
			// recorded, we simply ignore the update; i.e. only the first update
			// in a block will be applied.
			if tx.Bucket(FoundationUnlockHashes).Get(encoding.Marshal(pb.Height)) != nil {
				continue
			}
			setPriorFoundationUnlockHashes(tx, pb.Height)
			setFoundationUnlockHashes(tx, update.NewPrimary, update.NewFailsafe)
			transferFoundationOutputs(tx, pb.Height, update.NewPrimary)
		}
	}
}

// transferFoundationOutputs transfers all unspent subsidy outputs to
// newPrimary. This allows subsidies to be recovered in the event that the
// primary key is lost or unusable when a subsidy is created.
func transferFoundationOutputs(tx *bolt.Tx, currentHeight types.BlockHeight, newPrimary types.UnlockHash) {
	for height := types.FoundationHardforkHeight; height < currentHeight; height += types.FoundationSubsidyFrequency {
		blockID, err := getPath(tx, height)
		if err != nil {
			if build.DEBUG {
				panic(err)
			}
			continue
		}
		id := blockID.FoundationSubsidyID()
		sco, err := getUplocoinOutput(tx, id)
		if err != nil {
			continue // output has already been spent
		}
		sco.UnlockHash = newPrimary
		removeUplocoinOutput(tx, id)
		addUplocoinOutput(tx, id, sco)
	}
}

// applyTransaction applies the contents of a transaction to the ConsensusSet.
// This produces a set of diffs, which are stored in the blockNode containing
// the transaction. No verification is done by this function.
func applyTransaction(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	applyUplocoinInputs(tx, pb, t)
	applyUplocoinOutputs(tx, pb, t)
	applyFileContracts(tx, pb, t)
	applyFileContractRevisions(tx, pb, t)
	applyStorageProofs(tx, pb, t)
	applyUplofundInputs(tx, pb, t)
	applyUplofundOutputs(tx, pb, t)
	applyArbitraryData(tx, pb, t)
}
