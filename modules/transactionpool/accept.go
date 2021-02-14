package transactionpool

// TODO: It seems like the transaction pool is not properly detecting conflicts
// between a file contract revision and a file contract.

import (
	"fmt"
	"math"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/uplo/types/typesutil"
	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
)

var (
	errEmptySet     = errors.New("transaction set is empty")
	errLowMinerFees = errors.New("transaction set needs more miner fees to be accepted")

	// ErrTxnSetNotAccepted is the error returned when the dependency
	// DoNotAcceptTxnSet is used
	ErrTxnSetNotAccepted = errors.New("transaction set was not accepted")
)

// relatedObjectIDs determines all of the object ids related to a transaction.
func relatedObjectIDs(ts []types.Transaction) []ObjectID {
	oidMap := make(map[ObjectID]struct{})
	for _, t := range ts {
		for _, sci := range t.UplocoinInputs {
			oidMap[ObjectID(sci.ParentID)] = struct{}{}
		}
		for i := range t.UplocoinOutputs {
			oidMap[ObjectID(t.UplocoinOutputID(uint64(i)))] = struct{}{}
		}
		for i := range t.FileContracts {
			oidMap[ObjectID(t.FileContractID(uint64(i)))] = struct{}{}
		}
		for _, fcr := range t.FileContractRevisions {
			oidMap[ObjectID(fcr.ParentID)] = struct{}{}
		}
		for _, sp := range t.StorageProofs {
			oidMap[ObjectID(sp.ParentID)] = struct{}{}
		}
		for _, sfi := range t.UplofundInputs {
			oidMap[ObjectID(sfi.ParentID)] = struct{}{}
		}
		for i := range t.UplofundOutputs {
			oidMap[ObjectID(t.UplofundOutputID(uint64(i)))] = struct{}{}
		}
	}

	var oids []ObjectID
	for oid := range oidMap {
		oids = append(oids, oid)
	}
	return oids
}

// requiredFeesToExtendTpoolAtSize returns the fees that should be required to
// extend the transaction pool for a given size of transaction pool.
//
// NOTE: This function ignores the minimum transaction pool size required for a
// fee.
func requiredFeesToExtendTpoolAtSize(size int) types.Currency {
	// Calculate the fee required to bump out the size of the transaction pool.
	ratioToTarget := float64(size) / TransactionPoolSizeTarget
	feeFactor := math.Pow(ratioToTarget, TransactionPoolExponentiation)
	return types.UplocoinPrecision.MulFloat(feeFactor).Div64(1000) // Divide by 1000 to get UPLO/ kb
}

// requiredFeesToExtendTpool returns the amount of fees required to extend the
// transaction pool to fit another transaction set. The amount returned has the
// unit 'currency per byte'.
func (tp *TransactionPool) requiredFeesToExtendTpool() types.Currency {
	// If the transaction pool is nearly empty, it can be extended even if there
	// are no fees.
	if tp.transactionListSize < TransactionPoolSizeForFee {
		return types.ZeroCurrency
	}

	return requiredFeesToExtendTpoolAtSize(tp.transactionListSize)
}

// checkTransactionSetComposition checks if the transaction set is valid given
// the state of the pool. It does not check that each individual transaction
// would be legal in the next block, but does check things like miner fees and
// IsStandard.
func (tp *TransactionPool) checkTransactionSetComposition(ts []types.Transaction) (uint64, error) {
	// Check that the transaction set is not already known.
	setID := modules.TransactionSetID(crypto.HashObject(ts))
	_, exists := tp.transactionSets[setID]
	if exists {
		return 0, modules.ErrDuplicateTransactionSet
	}

	// All checks after this are expensive.
	//
	// TODO: There is no DoS prevention mechanism in place to prevent repeated
	// expensive verifications of invalid transactions that are created on the
	// fly.

	// Check that all transactions follow 'Standard.md' guidelines.
	setSize, err := isStandardTransactionSet(ts)
	if err != nil {
		return 0, err
	}

	return setSize, nil
}

// handleConflicts will return a transaction set which contains all unconfirmed
// transactions which are related (descendent or ancestor) in some way to any of
// the input transaction set.
func (tp *TransactionPool) handleConflicts(ts []types.Transaction, conflicts []modules.TransactionSetID, txnFn func([]types.Transaction) (modules.ConsensusChange, error)) ([]types.Transaction, error) {
	// Create a list of all the transaction ids that compose the set of
	// conflicts.
	conflictMap := make(map[types.TransactionID]modules.TransactionSetID)
	for _, conflict := range conflicts {
		conflictSet := tp.transactionSets[conflict]
		for _, conflictTxn := range conflictSet {
			conflictMap[conflictTxn.ID()] = conflict
		}
	}

	// Discard all duplicate transactions from the input transaction set.
	var dedupSet []types.Transaction
	for _, t := range ts {
		_, exists := conflictMap[t.ID()]
		if exists {
			continue
		}
		dedupSet = append(dedupSet, t)
	}
	if len(dedupSet) == 0 {
		return nil, modules.ErrDuplicateTransactionSet
	}
	// If transactions were pruned, it's possible that the set of
	// dependencies/conflicts has also reduced. To minimize computational load
	// on the consensus set, we want to prune out all of the conflicts that are
	// no longer relevant. As an example, consider the transaction set {A}, the
	// set {B}, and the new set {A, C}, where C is dependent on B. {A} and {B}
	// are both conflicts, but after deduplication {A} is no longer a conflict.
	// This is recursive, but it is guaranteed to run only once as the first
	// deduplication is guaranteed to be complete.
	if len(dedupSet) < len(ts) {
		oids := relatedObjectIDs(dedupSet)
		var conflicts []modules.TransactionSetID
		for _, oid := range oids {
			conflict, exists := tp.knownObjects[oid]
			if exists {
				conflicts = append(conflicts, conflict)
			}
		}
		return tp.handleConflicts(dedupSet, conflicts, txnFn)
	}

	// Merge all of the conflict sets with the input set (input set goes last
	// to preserve dependency ordering), and see if the set as a whole is both
	// small enough to be legal and valid as a set. If no, return an error. If
	// yes, add the new set to the pool, and eliminate the old set. The output
	// diff objects can be repeated, (no need to remove those). Just need to
	// remove the conflicts from tp.transactionSets.
	var superset []types.Transaction
	supersetMap := make(map[modules.TransactionSetID]struct{})
	for _, conflict := range conflictMap {
		supersetMap[conflict] = struct{}{}
	}
	for conflict := range supersetMap {
		superset = append(superset, tp.transactionSets[conflict]...)
	}
	superset = append(superset, dedupSet...)

	// Check the composition of the transaction set, including fees and
	// IsStandard rules (this is a new set, the rules must be rechecked).
	setSize, err := tp.checkTransactionSetComposition(superset)
	if err != nil {
		return nil, err
	}

	// Check that the transaction set has enough fees to justify adding it to
	// the transaction list.
	requiredFees := tp.requiredFeesToExtendTpool().Mul64(setSize)
	var setFees types.Currency
	for _, txn := range superset {
		for _, fee := range txn.MinerFees {
			setFees = setFees.Add(fee)
		}
	}
	if requiredFees.Cmp(setFees) > 0 {
		// TODO: check if there is an existing set with lower fees that we can
		// kick out.
		return nil, errLowMinerFees
	}

	// Check that the transaction set is valid.
	cc, err := txnFn(superset)
	if err != nil {
		return nil, modules.NewConsensusConflict("provided transaction set has prereqs, but is still invalid: " + err.Error())
	}

	// Remove the conflicts from the transaction pool.
	for conflict := range supersetMap {
		conflictSet := tp.transactionSets[conflict]
		tp.transactionListSize -= len(encoding.Marshal(conflictSet))
		delete(tp.transactionSets, conflict)
		delete(tp.transactionSetDiffs, conflict)
	}

	// Add the transaction set to the pool.
	setID := modules.TransactionSetID(crypto.HashObject(superset))
	tp.transactionSets[setID] = superset
	for _, diff := range cc.UplocoinOutputDiffs {
		tp.knownObjects[ObjectID(diff.ID)] = setID
	}
	for _, diff := range cc.FileContractDiffs {
		tp.knownObjects[ObjectID(diff.ID)] = setID
	}
	for _, diff := range cc.UplofundOutputDiffs {
		tp.knownObjects[ObjectID(diff.ID)] = setID
	}
	tp.transactionSetDiffs[setID] = &cc
	tsetSize := len(encoding.Marshal(superset))
	tp.transactionListSize += tsetSize
	for _, txn := range superset {
		if _, exists := tp.transactionHeights[txn.ID()]; !exists {
			tp.transactionHeights[txn.ID()] = tp.blockHeight
		}
	}

	// debug logging
	if build.DEBUG {
		txLogs := ""
		for i, t := range superset {
			txLogs += fmt.Sprintf("superset transaction %v size: %vB\n", i, len(encoding.Marshal(t)))
		}
		tp.log.Debugf("accepted transaction superset %v, size: %vB\ntpool size is %vB after accpeting transaction superset\ntransactions: \n%v\n", setID, tsetSize, tp.transactionListSize, txLogs)
	}

	return superset, nil
}

// acceptTransactionSet verifies that a transaction set is allowed to be in the
// transaction pool, and then adds it to the transaction pool.
func (tp *TransactionPool) acceptTransactionSet(ts []types.Transaction, txnFn func([]types.Transaction) (modules.ConsensusChange, error)) (superset []types.Transaction, err error) {
	if len(ts) == 0 {
		return nil, errEmptySet
	}

	// Remove all transactions that have been confirmed in the transaction set.
	oldTS := ts
	ts = []types.Transaction{}
	for _, txn := range oldTS {
		if !tp.transactionConfirmed(tp.dbTx, txn.ID()) {
			ts = append(ts, txn)
		}
	}
	// If no transactions remain, return a dublicate error.
	if len(ts) == 0 {
		return nil, modules.ErrDuplicateTransactionSet
	}

	// Check the composition of the transaction set.
	setSize, err := tp.checkTransactionSetComposition(ts)
	if err != nil {
		return nil, err
	}

	// Check that the transaction set has enough fees to justify adding it to
	// the transaction list.
	requiredFees := tp.requiredFeesToExtendTpool().Mul64(setSize)
	var setFees types.Currency
	for _, txn := range ts {
		for _, fee := range txn.MinerFees {
			setFees = setFees.Add(fee)
		}
	}
	if requiredFees.Cmp(setFees) > 0 {
		// TODO: check if there is an existing set with lower fees that we can
		// kick out.
		tp.log.Debugln("Incoming transaction was rejected for having low fees", requiredFees, setFees)
		for _, txn := range ts {
			tp.log.Debugln(txn.ID())
		}
		return nil, errLowMinerFees
	}

	// Check for conflicts with other transactions, which would indicate a
	// double-spend. Legal children of a transaction set will also trigger the
	// conflict-detector.
	oids := relatedObjectIDs(ts)
	var conflicts []modules.TransactionSetID
	for _, oid := range oids {
		conflict, exists := tp.knownObjects[oid]
		if exists {
			conflicts = append(conflicts, conflict)
		}
	}
	if len(conflicts) > 0 {
		return tp.handleConflicts(ts, conflicts, txnFn)
	}
	cc, err := txnFn(ts)
	if err != nil {
		return nil, modules.NewConsensusConflict("provided transaction set is invalid: " + err.Error())
	}

	// Add the transaction set to the pool.
	setID := modules.TransactionSetID(crypto.HashObject(ts))
	tp.transactionSets[setID] = ts
	for _, oid := range oids {
		tp.knownObjects[oid] = setID
	}
	tp.transactionSetDiffs[setID] = &cc
	tsetSize := len(encoding.Marshal(ts))
	tp.transactionListSize += tsetSize
	for _, txn := range ts {
		if _, exists := tp.transactionHeights[txn.ID()]; !exists {
			tp.transactionHeights[txn.ID()] = tp.blockHeight
		}
	}

	// debug logging
	if build.DEBUG {
		txLogs := ""
		for i, t := range ts {
			txLogs += fmt.Sprintf("transaction %v size: %vB\n", i, len(encoding.Marshal(t)))
		}
		tp.log.Debugf("accepted transaction set %v, size: %vB\ntpool size is %vB after accpeting transaction set\ntransactions: \n%v\n", setID, tsetSize, tp.transactionListSize, txLogs)
	}
	return ts, nil
}

// submitTransactionSet will submit a transaction set to the transaction pool
// and return the minimum superset for that transaction set.
func (tp *TransactionPool) submitTransactionSet(ts []types.Transaction) ([]types.Transaction, error) {
	// assert on consensus set to get special method
	cs, ok := tp.consensusSet.(interface {
		LockedTryTransactionSet(fn func(func(txns []types.Transaction) (modules.ConsensusChange, error)) error) error
	})
	if !ok {
		return nil, errors.New("consensus set does not support LockedTryTransactionSet method")
	}

	var superset []types.Transaction
	var acceptErr error
	err := cs.LockedTryTransactionSet(func(txnFn func(txns []types.Transaction) (modules.ConsensusChange, error)) error {
		tp.mu.Lock()
		defer tp.mu.Unlock()

		// Attempt to get the transaction set into the transaction pool.
		superset, acceptErr = tp.acceptTransactionSet(ts, txnFn)
		if errors.Contains(acceptErr, modules.ErrDuplicateTransactionSet) {
			tp.log.Debugln("Transaction set is a duplicate:", acceptErr)
			return acceptErr
		}
		if acceptErr != nil {
			tp.log.Debugln("Transaction set broadcast has failed:", acceptErr)
			if build.DEBUG && modules.IsConsensusConflict(acceptErr) {
				tp.printConflicts(ts)
			}
			return acceptErr
		}
		// Notify subscribers of an accepted transaction set
		tp.updateSubscribersTransactions()
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Get the minimum required set from the superset for the input transaction
	// set.
	minSuperSet := typesutil.MinimumTransactionSet(ts, superset)
	return minSuperSet, nil
}

// AcceptTransactionSet adds a transaction to the unconfirmed set of
// transactions. If the transaction is accepted, it will be relayed to
// connected peers.
func (tp *TransactionPool) AcceptTransactionSet(ts []types.Transaction) error {
	if err := tp.tg.Add(); err != nil {
		return err
	}
	defer tp.tg.Done()

	// Drop the transaction set and return ErrTxnSetNotAccepted
	if tp.deps.Disrupt("DoNotAcceptTxnSet") {
		return ErrTxnSetNotAccepted
	}

	tp.log.Debugln("Received a transaction (internal or external), attempting to broadcast")
	minSuperSet, err := tp.submitTransactionSet(ts)
	if errors.Contains(err, modules.ErrDuplicateTransactionSet) {
		return err
	}
	if err != nil {
		tp.log.Debugln("Transaction set will not be broadcast due to an error:", err)
		return err
	}
	go tp.gateway.Broadcast("RelayTransactionSet", minSuperSet, tp.gateway.Peers())
	tp.log.Debugln("Transaction set broadcast appears to have succeeded")
	return nil
}

// relayTransactionSet is an RPC that accepts a transaction set from a peer. If
// the accept is successful, the transaction will be relayed to the gateway's
// other peers.
func (tp *TransactionPool) relayTransactionSet(conn modules.PeerConn) error {
	if err := tp.tg.Add(); err != nil {
		return err
	}
	defer tp.tg.Done()

	// Connection stability and cleanup code.
	err := conn.SetDeadline(time.Now().Add(relayTransactionSetTimeout))
	if err != nil {
		return err
	}
	// Automatically close the channel when tg.Stop() is called.
	finishedChan := make(chan struct{})
	defer close(finishedChan)
	go func() {
		select {
		case <-tp.tg.StopChan():
		case <-finishedChan:
		}
		conn.Close()
	}()

	// Read the transaction.
	var ts []types.Transaction
	err = encoding.ReadObject(conn, &ts, types.BlockSizeLimit)
	if err != nil {
		return err
	}
	return tp.AcceptTransactionSet(ts)
}
