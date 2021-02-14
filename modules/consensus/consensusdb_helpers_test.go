package consensus

// database_test.go contains a bunch of legacy functions to preserve
// compatibility with the test suite.

import (
	"github.com/uplo-tech/bolt"

	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

// dbBlockHeight is a convenience function allowing blockHeight to be called
// without a bolt.Tx.
func (cs *ConsensusSet) dbBlockHeight() (bh types.BlockHeight) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		bh = blockHeight(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return bh
}

// dbCurrentProcessedBlock is a convenience function allowing
// currentProcessedBlock to be called without a bolt.Tx.
func (cs *ConsensusSet) dbCurrentProcessedBlock() (pb *processedBlock) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		pb = currentProcessedBlock(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return pb
}

// dbGetPath is a convenience function allowing getPath to be called without a
// bolt.Tx.
func (cs *ConsensusSet) dbGetPath(bh types.BlockHeight) (id types.BlockID, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		id, err = getPath(tx, bh)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return id, err
}

// dbPushPath is a convenience function allowing pushPath to be called without a
// bolt.Tx.
func (cs *ConsensusSet) dbPushPath(bid types.BlockID) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		pushPath(tx, bid)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbGetBlockMap is a convenience function allowing getBlockMap to be called
// without a bolt.Tx.
func (cs *ConsensusSet) dbGetBlockMap(id types.BlockID) (pb *processedBlock, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		pb, err = getBlockMap(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return pb, err
}

// dbGetUplocoinOutput is a convenience function allowing getUplocoinOutput to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbGetUplocoinOutput(id types.UplocoinOutputID) (sco types.UplocoinOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		sco, err = getUplocoinOutput(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return sco, err
}

// getArbUplocoinOutput is a convenience function fetching a single random
// Uplocoin output from the database.
func (cs *ConsensusSet) getArbUplocoinOutput() (scoid types.UplocoinOutputID, sco types.UplocoinOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(UplocoinOutputs).Cursor()
		scoidBytes, scoBytes := cursor.First()
		copy(scoid[:], scoidBytes)
		return encoding.Unmarshal(scoBytes, &sco)
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return scoid, sco, nil
}

// dbGetFileContract is a convenience function allowing getFileContract to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbGetFileContract(id types.FileContractID) (fc types.FileContract, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		fc, err = getFileContract(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return fc, err
}

// dbAddFileContract is a convenience function allowing addFileContract to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbAddFileContract(id types.FileContractID, fc types.FileContract) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		addFileContract(tx, id, fc)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbRemoveFileContract is a convenience function allowing removeFileContract
// to be called without a bolt.Tx.
func (cs *ConsensusSet) dbRemoveFileContract(id types.FileContractID) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		removeFileContract(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbGetUplofundOutput is a convenience function allowing getUplofundOutput to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbGetUplofundOutput(id types.UplofundOutputID) (sfo types.UplofundOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		sfo, err = getUplofundOutput(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return sfo, err
}

// dbAddUplofundOutput is a convenience function allowing addUplofundOutput to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbAddUplofundOutput(id types.UplofundOutputID, sfo types.UplofundOutput) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		addUplofundOutput(tx, id, sfo)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbGetUplofundPool is a convenience function allowing getUplofundPool to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbGetUplofundPool() (uplofundPool types.Currency) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		uplofundPool = getUplofundPool(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return uplofundPool
}

// dbGetDSCO is a convenience function allowing a delayed Uplocoin output to be
// fetched without a bolt.Tx. An error is returned if the delayed output is not
// found at the maturity height indicated by the input.
func (cs *ConsensusSet) dbGetDSCO(height types.BlockHeight, id types.UplocoinOutputID) (dsco types.UplocoinOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		dscoBucketID := append(prefixDSCO, encoding.Marshal(height)...)
		dscoBucket := tx.Bucket(dscoBucketID)
		if dscoBucket == nil {
			err = errNilItem
			return nil
		}
		dscoBytes := dscoBucket.Get(id[:])
		if dscoBytes == nil {
			err = errNilItem
			return nil
		}
		err = encoding.Unmarshal(dscoBytes, &dsco)
		if err != nil {
			panic(err)
		}
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return dsco, err
}

// dbStorageProofSegment is a convenience function allowing
// 'storageProofSegment' to be called during testing without a tx.
func (cs *ConsensusSet) dbStorageProofSegment(fcid types.FileContractID) (index uint64, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		index, err = storageProofSegment(tx, fcid)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return index, err
}

// dbValidStorageProofs is a convenience function allowing 'validStorageProofs'
// to be called during testing without a tx.
func (cs *ConsensusSet) dbValidStorageProofs(t types.Transaction) (err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		err = validStorageProofs(tx, t)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return err
}

// dbValidFileContractRevisions is a convenience function allowing
// 'validFileContractRevisions' to be called during testing without a tx.
func (cs *ConsensusSet) dbValidFileContractRevisions(t types.Transaction) (err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		err = validFileContractRevisions(tx, t)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return err
}
