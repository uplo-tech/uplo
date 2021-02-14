package explorer

import (
	"github.com/uplo-tech/bolt"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// Block takes a block ID and finds the corresponding block, provided that the
// block is in the consensus set.
func (e *Explorer) Block(id types.BlockID) (types.Block, types.BlockHeight, bool) {
	var height types.BlockHeight
	err := e.db.View(dbGetAndDecode(bucketBlockIDs, id, &height))
	if err != nil {
		return types.Block{}, 0, false
	}
	block, exists := e.cs.BlockAtHeight(height)
	if !exists {
		return types.Block{}, 0, false
	}
	return block, height, true
}

// BlockFacts returns a set of statistics about the blockchain as they appeared
// at a given block height, and a bool indicating whether facts exist for the
// given height.
func (e *Explorer) BlockFacts(height types.BlockHeight) (modules.BlockFacts, bool) {
	var bf blockFacts
	err := e.db.View(e.dbGetBlockFacts(height, &bf))
	if err != nil {
		return modules.BlockFacts{}, false
	}

	return bf.BlockFacts, true
}

// LatestBlockFacts returns a set of statistics about the blockchain as they appeared
// at the latest block height in the explorer's consensus set.
func (e *Explorer) LatestBlockFacts() modules.BlockFacts {
	var bf blockFacts
	err := e.db.View(func(tx *bolt.Tx) error {
		var height types.BlockHeight
		err := dbGetInternal(internalBlockHeight, &height)(tx)
		if err != nil {
			return err
		}
		return e.dbGetBlockFacts(height, &bf)(tx)
	})
	if err != nil {
		build.Critical(err)
	}
	return bf.BlockFacts
}

// Transaction takes a transaction ID and finds the block containing the
// transaction. Because of the miner payouts, the transaction ID might be a
// block ID. To find the transaction, iterate through the block.
func (e *Explorer) Transaction(id types.TransactionID) (types.Block, types.BlockHeight, bool) {
	var height types.BlockHeight
	err := e.db.View(dbGetAndDecode(bucketTransactionIDs, id, &height))
	if err != nil {
		return types.Block{}, 0, false
	}
	block, exists := e.cs.BlockAtHeight(height)
	if !exists {
		return types.Block{}, 0, false
	}
	return block, height, true
}

// UnlockHash returns the IDs of all the transactions that contain the unlock
// hash. An empty set indicates that the unlock hash does not appear in the
// blockchain.
func (e *Explorer) UnlockHash(uh types.UnlockHash) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketUnlockHashes, uh, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}

// UplocoinOutput returns the Uplocoin output associated with the specified ID.
func (e *Explorer) UplocoinOutput(id types.UplocoinOutputID) (types.UplocoinOutput, bool) {
	var sco types.UplocoinOutput
	err := e.db.View(dbGetAndDecode(bucketUplocoinOutputs, id, &sco))
	if err != nil {
		return types.UplocoinOutput{}, false
	}
	return sco, true
}

// UplocoinOutputID returns all of the transactions that contain the specified
// Uplocoin output ID. An empty set indicates that the Uplocoin output ID does
// not appear in the blockchain.
func (e *Explorer) UplocoinOutputID(id types.UplocoinOutputID) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketUplocoinOutputIDs, id, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}

// FileContractHistory returns the history associated with the specified file
// contract ID, which includes the file contract itself and all of the
// revisions that have been submitted to the blockchain. The first bool
// indicates whether the file contract exists, and the second bool indicates
// whether a storage proof was successfully submitted for the file contract.
func (e *Explorer) FileContractHistory(id types.FileContractID) (fc types.FileContract, fcrs []types.FileContractRevision, fcE bool, spE bool) {
	var history fileContractHistory
	err := e.db.View(dbGetAndDecode(bucketFileContractHistories, id, &history))
	fc = history.Contract
	fcrs = history.Revisions
	fcE = err == nil
	spE = history.StorageProof.ParentID == id
	return
}

// FileContractID returns all transactions that contain the specified
// file contract ID. An empty set indicates that the file contract ID does not
// appear in the blockchain.
func (e *Explorer) FileContractID(id types.FileContractID) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketFileContractIDs, id, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}

// FileContractPayouts returns all of the spendable Uplocoin outputs which are the
// result of a FileContract. An empty set indicates that the file contract is
// still open
func (e *Explorer) FileContractPayouts(id types.FileContractID) ([]types.UplocoinOutput, error) {
	var history fileContractHistory
	err := e.db.View(dbGetAndDecode(bucketFileContractHistories, id, &history))
	if err != nil {
		return []types.UplocoinOutput{}, err
	}

	fc := history.Contract
	var outputs []types.UplocoinOutput

	for i := range fc.ValidProofOutputs {
		scoid := id.StorageProofOutputID(types.ProofValid, uint64(i))

		sco, found := e.UplocoinOutput(scoid)
		if found {
			outputs = append(outputs, sco)
		}
	}
	for i := range fc.MissedProofOutputs {
		scoid := id.StorageProofOutputID(types.ProofMissed, uint64(i))

		sco, found := e.UplocoinOutput(scoid)
		if found {
			outputs = append(outputs, sco)
		}
	}

	return outputs, nil
}

// UplofundOutput returns the uplofund output associated with the specified ID.
func (e *Explorer) UplofundOutput(id types.UplofundOutputID) (types.UplofundOutput, bool) {
	var sco types.UplofundOutput
	err := e.db.View(dbGetAndDecode(bucketUplofundOutputs, id, &sco))
	if err != nil {
		return types.UplofundOutput{}, false
	}
	return sco, true
}

// UplofundOutputID returns all of the transactions that contain the specified
// uplofund output ID. An empty set indicates that the uplofund output ID does
// not appear in the blockchain.
func (e *Explorer) UplofundOutputID(id types.UplofundOutputID) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketUplofundOutputIDs, id, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}
