package wallet

import (
	"bytes"
	"sort"

	"github.com/uplo-tech/bolt"
	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

var (
	// errBuilderAlreadySigned indicates that the transaction builder has
	// already added at least one successful signature to the transaction,
	// meaning that future calls to Sign will result in an invalid transaction.
	errBuilderAlreadySigned = errors.New("sign has already been called on this transaction builder, multiple calls can cause issues")

	// errDustOutput indicates an output is not spendable because it is dust.
	errDustOutput = errors.New("output is too small")

	// errOutputTimelock indicates an output's timelock is still active.
	errOutputTimelock = errors.New("wallet consensus set height is lower than the output timelock")

	// errSpendHeightTooHigh indicates an output's spend height is greater than
	// the allowed height.
	errSpendHeightTooHigh = errors.New("output spend height exceeds the allowed height")

	// errReplaceIndexOutOfBounds indicated that the output index is out of
	// bounds.
	errReplaceIndexOutOfBounds = errors.New("replacement output index out of bounds")
)

// transactionBuilder allows transactions to be manually constructed, including
// the ability to fund transactions with Uplocoins and uplofunds from the wallet.
type transactionBuilder struct {
	// 'signed' indicates that at least one transaction signature has been
	// added to the wallet, meaning that future calls to 'Sign' will fail.
	parents     []types.Transaction
	signed      bool
	transaction types.Transaction

	newParents            []int
	UplocoinInputs         []int
	uplofundInputs         []int
	transactionSignatures []int

	wallet *Wallet
}

// addSignatures will sign a transaction using a spendable key, with support
// for multisig spendable keys. Because of the restricted input, the function
// is compatible with both Uplocoin inputs and uplofund inputs.
func addSignatures(txn *types.Transaction, cf types.CoveredFields, uc types.UnlockConditions, parentID crypto.Hash, spendKey spendableKey, height types.BlockHeight) (newSigIndices []int) {
	// Try to find the matching secret key for each public key - some public
	// keys may not have a match. Some secret keys may be used multiple times,
	// which is why public keys are used as the outer loop.
	totalSignatures := uint64(0)
	for i, uploPubKey := range uc.PublicKeys {
		// Search for the matching secret key to the public key.
		for j := range spendKey.SecretKeys {
			pubKey := spendKey.SecretKeys[j].PublicKey()
			if !bytes.Equal(uploPubKey.Key, pubKey[:]) {
				continue
			}

			// Found the right secret key, add a signature.
			sig := types.TransactionSignature{
				ParentID:       parentID,
				CoveredFields:  cf,
				PublicKeyIndex: uint64(i),
			}
			newSigIndices = append(newSigIndices, len(txn.TransactionSignatures))
			txn.TransactionSignatures = append(txn.TransactionSignatures, sig)
			sigIndex := len(txn.TransactionSignatures) - 1
			sigHash := txn.SigHash(sigIndex, height)
			encodedSig := crypto.SignHash(sigHash, spendKey.SecretKeys[j])
			txn.TransactionSignatures[sigIndex].Signature = encodedSig[:]

			// Count that the signature has been added, and break out of the
			// secret key loop.
			totalSignatures++
			break
		}

		// If there are enough signatures to satisfy the unlock conditions,
		// break out of the outer loop.
		if totalSignatures == uc.SignaturesRequired {
			break
		}
	}
	return newSigIndices
}

// checkOutput is a helper function used to determine if an output is usable.
func (w *Wallet) checkOutput(tx *bolt.Tx, currentHeight types.BlockHeight, id types.UplocoinOutputID, output types.UplocoinOutput, dustThreshold types.Currency) error {
	// Check that an output is not dust
	if output.Value.Cmp(dustThreshold) < 0 {
		return errDustOutput
	}
	// Check that this output has not recently been spent by the wallet.
	spendHeight, err := dbGetSpentOutput(tx, types.OutputID(id))
	if err == nil {
		if spendHeight+RespendTimeout > currentHeight {
			return errSpendHeightTooHigh
		}
	}
	outputUnlockConditions := w.keys[output.UnlockHash].UnlockConditions
	if currentHeight < outputUnlockConditions.Timelock {
		return errOutputTimelock
	}

	return nil
}

// Copy creates a deep copy of the current transactionBuilder that can be used to
// extend the transaction in an alternate way (i.e. create a double spend
// transaction).
func (tb *transactionBuilder) Copy() modules.TransactionBuilder {
	copyBuilder := tb.wallet.registerTransaction(tb.transaction, tb.parents)

	// Copy the non-transaction fields over to the new builder.
	copyBuilder.newParents = make([]int, len(tb.newParents))
	copy(copyBuilder.newParents, tb.newParents)

	copyBuilder.UplocoinInputs = make([]int, len(tb.UplocoinInputs))
	copy(copyBuilder.UplocoinInputs, tb.UplocoinInputs)

	copyBuilder.uplofundInputs = make([]int, len(tb.uplofundInputs))
	copy(copyBuilder.uplofundInputs, tb.uplofundInputs)

	copyBuilder.transactionSignatures = make([]int, len(tb.transactionSignatures))
	copy(copyBuilder.transactionSignatures, tb.transactionSignatures)

	copyBuilder.signed = tb.signed
	return copyBuilder
}

// MarkWalletInputs updates transactionBuilder state by inferring which inputs
// belong to this wallet. This allows those inputs to be signed. Returns true if
// and only if any inputs belonging to the wallet are found.
func (tb *transactionBuilder) MarkWalletInputs() bool {
	markedAnyInputs := false
	for i, scInput := range tb.transaction.UplocoinInputs {
		unlockHash := scInput.UnlockConditions.UnlockHash()
		if !tb.wallet.managedCanSpendUnlockHash(unlockHash) {
			continue
		}

		// Only add un-marked inputs, making MarkWalletInputs idempotent.
		alreadyMarked := false
		for _, storedIdx := range tb.UplocoinInputs {
			if i == storedIdx {
				alreadyMarked = true
				break
			}
		}
		if !alreadyMarked {
			tb.UplocoinInputs = append(tb.UplocoinInputs, i)
			markedAnyInputs = true
		}
	}

	for i, sfInput := range tb.transaction.UplofundInputs {
		unlockHash := sfInput.UnlockConditions.UnlockHash()
		if !tb.wallet.managedCanSpendUnlockHash(unlockHash) {
			continue
		}

		// Only add un-marked inputs, making MarkWalletInputs idempotent.
		alreadyMarked := false
		for _, storedIdx := range tb.uplofundInputs {
			if i == storedIdx {
				alreadyMarked = true
				break
			}
		}
		if !alreadyMarked {
			tb.UplocoinInputs = append(tb.uplofundInputs, i)
			markedAnyInputs = true
		}
	}
	return markedAnyInputs
}

// FundUplocoins will add a Uplocoin input of exactly 'amount' to the
// transaction. A parent transaction may be needed to achieve an input with the
// correct value. The Uplocoin input will not be signed until 'Sign' is called
// on the transaction builder.
func (tb *transactionBuilder) FundUplocoins(amount types.Currency) error {
	if amount.IsZero() {
		return nil
	}
	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := tb.wallet.DustThreshold()
	if err != nil {
		return err
	}

	tb.wallet.mu.Lock()
	defer tb.wallet.mu.Unlock()

	consensusHeight, err := dbGetConsensusHeight(tb.wallet.dbTx)
	if err != nil {
		return err
	}

	// Collect a value-sorted set of Uplocoin outputs.
	var so sortedOutputs
	err = dbForEachUplocoinOutput(tb.wallet.dbTx, func(scoid types.UplocoinOutputID, sco types.UplocoinOutput) {
		so.ids = append(so.ids, scoid)
		so.outputs = append(so.outputs, sco)
	})
	if err != nil {
		return err
	}
	// Add all of the unconfirmed outputs as well.
	for _, upt := range tb.wallet.unconfirmedProcessedTransactions {
		for i, sco := range upt.Transaction.UplocoinOutputs {
			// Determine if the output belongs to the wallet.
			_, exists := tb.wallet.keys[sco.UnlockHash]
			if !exists {
				continue
			}
			so.ids = append(so.ids, upt.Transaction.UplocoinOutputID(uint64(i)))
			so.outputs = append(so.outputs, sco)
		}
	}
	sort.Sort(sort.Reverse(so))

	// Create and fund a parent transaction that will add the correct amount of
	// Uplocoins to the transaction.
	var fund types.Currency
	// potentialFund tracks the balance of the wallet including outputs that
	// have been spent in other unconfirmed transactions recently. This is to
	// provide the user with a more useful error message in the event that they
	// are overspending.
	var potentialFund types.Currency
	parentTxn := types.Transaction{}
	var spentScoids []types.UplocoinOutputID
	for i := range so.ids {
		scoid := so.ids[i]
		sco := so.outputs[i]
		// Check that the output can be spent.
		if err := tb.wallet.checkOutput(tb.wallet.dbTx, consensusHeight, scoid, sco, dustThreshold); err != nil {
			if errors.Contains(err, errSpendHeightTooHigh) {
				potentialFund = potentialFund.Add(sco.Value)
			}
			continue
		}

		// Add a Uplocoin input for this output.
		sci := types.UplocoinInput{
			ParentID:         scoid,
			UnlockConditions: tb.wallet.keys[sco.UnlockHash].UnlockConditions,
		}
		parentTxn.UplocoinInputs = append(parentTxn.UplocoinInputs, sci)
		spentScoids = append(spentScoids, scoid)

		// Add the output to the total fund
		fund = fund.Add(sco.Value)
		potentialFund = potentialFund.Add(sco.Value)
		if fund.Cmp(amount) >= 0 {
			break
		}
	}
	if potentialFund.Cmp(amount) >= 0 && fund.Cmp(amount) < 0 {
		return modules.ErrIncompleteTransactions
	}
	if fund.Cmp(amount) < 0 {
		return modules.ErrLowBalance
	}

	// Create and add the output that will be used to fund the standard
	// transaction.
	parentUnlockConditions, err := tb.wallet.nextPrimarySeedAddress(tb.wallet.dbTx)
	if err != nil {
		return err
	}

	exactOutput := types.UplocoinOutput{
		Value:      amount,
		UnlockHash: parentUnlockConditions.UnlockHash(),
	}
	parentTxn.UplocoinOutputs = append(parentTxn.UplocoinOutputs, exactOutput)

	// Create a refund output if needed.
	if !amount.Equals(fund) {
		refundUnlockConditions, err := tb.wallet.nextPrimarySeedAddress(tb.wallet.dbTx)
		if err != nil {
			return err
		}
		refundOutput := types.UplocoinOutput{
			Value:      fund.Sub(amount),
			UnlockHash: refundUnlockConditions.UnlockHash(),
		}
		parentTxn.UplocoinOutputs = append(parentTxn.UplocoinOutputs, refundOutput)
	}

	// Sign all of the inputs to the parent transaction.
	for _, sci := range parentTxn.UplocoinInputs {
		addSignatures(&parentTxn, types.FullCoveredFields, sci.UnlockConditions, crypto.Hash(sci.ParentID), tb.wallet.keys[sci.UnlockConditions.UnlockHash()], consensusHeight)
	}
	// Mark the parent output as spent. Must be done after the transaction is
	// finished because otherwise the txid and output id will change.
	err = dbPutSpentOutput(tb.wallet.dbTx, types.OutputID(parentTxn.UplocoinOutputID(0)), consensusHeight)
	if err != nil {
		return err
	}

	// Add the exact output.
	newInput := types.UplocoinInput{
		ParentID:         parentTxn.UplocoinOutputID(0),
		UnlockConditions: parentUnlockConditions,
	}
	tb.newParents = append(tb.newParents, len(tb.parents))
	tb.parents = append(tb.parents, parentTxn)
	tb.UplocoinInputs = append(tb.UplocoinInputs, len(tb.transaction.UplocoinInputs))
	tb.transaction.UplocoinInputs = append(tb.transaction.UplocoinInputs, newInput)

	// Mark all outputs that were spent as spent.
	for _, scoid := range spentScoids {
		err = dbPutSpentOutput(tb.wallet.dbTx, types.OutputID(scoid), consensusHeight)
		if err != nil {
			return err
		}
	}
	return nil
}

// FundUplofunds will add a uplofund input of exactly 'amount' to the
// transaction. A parent transaction may be needed to achieve an input with the
// correct value. The uplofund input will not be signed until 'Sign' is called
// on the transaction builder.
func (tb *transactionBuilder) FundUplofunds(amount types.Currency) error {
	if amount.IsZero() {
		return nil
	}

	tb.wallet.mu.Lock()
	defer tb.wallet.mu.Unlock()

	consensusHeight, err := dbGetConsensusHeight(tb.wallet.dbTx)
	if err != nil {
		return err
	}

	// Create and fund a parent transaction that will add the correct amount of
	// uplofunds to the transaction.
	var fund types.Currency
	var potentialFund types.Currency
	parentTxn := types.Transaction{}
	var spentSfoids []types.UplofundOutputID
	c := tb.wallet.dbTx.Bucket(bucketUplofundOutputs).Cursor()
	for idBytes, sfoBytes := c.First(); idBytes != nil; idBytes, sfoBytes = c.Next() {
		var sfoid types.UplofundOutputID
		var sfo types.UplofundOutput
		if err := encoding.Unmarshal(idBytes, &sfoid); err != nil {
			return err
		} else if err := encoding.Unmarshal(sfoBytes, &sfo); err != nil {
			return err
		}

		// Check that this output has not recently been spent by the wallet.
		spendHeight, err := dbGetSpentOutput(tb.wallet.dbTx, types.OutputID(sfoid))
		if err != nil {
			// mimic map behavior: no entry means zero value
			spendHeight = 0
		}
		// Prevent an underflow error.
		allowedHeight := consensusHeight - RespendTimeout
		if consensusHeight < RespendTimeout {
			allowedHeight = 0
		}
		if spendHeight > allowedHeight {
			potentialFund = potentialFund.Add(sfo.Value)
			continue
		}
		outputUnlockConditions := tb.wallet.keys[sfo.UnlockHash].UnlockConditions
		if consensusHeight < outputUnlockConditions.Timelock {
			continue
		}

		// Add a uplofund input for this output.
		parentClaimUnlockConditions, err := tb.wallet.nextPrimarySeedAddress(tb.wallet.dbTx)
		if err != nil {
			return err
		}
		sfi := types.UplofundInput{
			ParentID:         sfoid,
			UnlockConditions: outputUnlockConditions,
			ClaimUnlockHash:  parentClaimUnlockConditions.UnlockHash(),
		}
		parentTxn.UplofundInputs = append(parentTxn.UplofundInputs, sfi)
		spentSfoids = append(spentSfoids, sfoid)

		// Add the output to the total fund
		fund = fund.Add(sfo.Value)
		potentialFund = potentialFund.Add(sfo.Value)
		if fund.Cmp(amount) >= 0 {
			break
		}
	}
	if potentialFund.Cmp(amount) >= 0 && fund.Cmp(amount) < 0 {
		return modules.ErrIncompleteTransactions
	}
	if fund.Cmp(amount) < 0 {
		return modules.ErrLowBalance
	}

	// Create and add the output that will be used to fund the standard
	// transaction.
	parentUnlockConditions, err := tb.wallet.nextPrimarySeedAddress(tb.wallet.dbTx)
	if err != nil {
		return err
	}
	exactOutput := types.UplofundOutput{
		Value:      amount,
		UnlockHash: parentUnlockConditions.UnlockHash(),
	}
	parentTxn.UplofundOutputs = append(parentTxn.UplofundOutputs, exactOutput)

	// Create a refund output if needed.
	if !amount.Equals(fund) {
		refundUnlockConditions, err := tb.wallet.nextPrimarySeedAddress(tb.wallet.dbTx)
		if err != nil {
			return err
		}
		refundOutput := types.UplofundOutput{
			Value:      fund.Sub(amount),
			UnlockHash: refundUnlockConditions.UnlockHash(),
		}
		parentTxn.UplofundOutputs = append(parentTxn.UplofundOutputs, refundOutput)
	}

	// Sign all of the inputs to the parent transaction.
	for _, sfi := range parentTxn.UplofundInputs {
		addSignatures(&parentTxn, types.FullCoveredFields, sfi.UnlockConditions, crypto.Hash(sfi.ParentID), tb.wallet.keys[sfi.UnlockConditions.UnlockHash()], consensusHeight)
	}

	// Add the exact output.
	claimUnlockConditions, err := tb.wallet.nextPrimarySeedAddress(tb.wallet.dbTx)
	if err != nil {
		return err
	}
	newInput := types.UplofundInput{
		ParentID:         parentTxn.UplofundOutputID(0),
		UnlockConditions: parentUnlockConditions,
		ClaimUnlockHash:  claimUnlockConditions.UnlockHash(),
	}
	tb.newParents = append(tb.newParents, len(tb.parents))
	tb.parents = append(tb.parents, parentTxn)
	tb.uplofundInputs = append(tb.uplofundInputs, len(tb.transaction.UplofundInputs))
	tb.transaction.UplofundInputs = append(tb.transaction.UplofundInputs, newInput)

	// Mark all outputs that were spent as spent.
	for _, sfoid := range spentSfoids {
		err = dbPutSpentOutput(tb.wallet.dbTx, types.OutputID(sfoid), consensusHeight)
		if err != nil {
			return err
		}
	}
	return nil
}

// Sweep creates a funded txn that sends the inputs of this transactionBuilder
// to the specified output if submitted to the blockchain.
func (tb *transactionBuilder) Sweep(output types.UplocoinOutput) (txn types.Transaction, parents []types.Transaction) {
	builder := tb.Copy()
	builder.AddUplocoinOutput(output)
	return builder.View()
}

// UnconfirmedParents returns the unconfirmed parents of the transaction set
// that is being constructed by the transaction builder.
func (tb *transactionBuilder) UnconfirmedParents() (parents []types.Transaction, err error) {
	addedParents := make(map[types.TransactionID]struct{})
	for _, p := range tb.parents {
		for _, sci := range p.UplocoinInputs {
			tSet := tb.wallet.tpool.TransactionSet(crypto.Hash(sci.ParentID))
			for _, txn := range tSet {
				// Add the transaction to the parents.
				txnID := txn.ID()
				if _, exists := addedParents[txnID]; exists {
					continue
				}
				addedParents[txnID] = struct{}{}
				parents = append(parents, txn)

				// When we found the transaction that contains the output that
				// is spent by sci we stop to avoid adding child transactions.
				for i := range txn.UplocoinOutputs {
					if txn.UplocoinOutputID(uint64(i)) == sci.ParentID {
						break
					}
				}
			}
		}
	}
	return
}

// AddParents adds a set of parents to the transaction.
func (tb *transactionBuilder) AddParents(newParents []types.Transaction) {
	tb.parents = append(tb.parents, newParents...)
}

// AddMinerFee adds a miner fee to the transaction, returning the index of the
// miner fee within the transaction.
func (tb *transactionBuilder) AddMinerFee(fee types.Currency) uint64 {
	tb.transaction.MinerFees = append(tb.transaction.MinerFees, fee)
	return uint64(len(tb.transaction.MinerFees) - 1)
}

// AddUplocoinInput adds a Uplocoin input to the transaction, returning the index
// of the Uplocoin input within the transaction. When 'Sign' gets called, this
// input will be left unsigned.
func (tb *transactionBuilder) AddUplocoinInput(input types.UplocoinInput) uint64 {
	tb.transaction.UplocoinInputs = append(tb.transaction.UplocoinInputs, input)
	return uint64(len(tb.transaction.UplocoinInputs) - 1)
}

// AddUplocoinOutput adds a Uplocoin output to the transaction, returning the
// index of the Uplocoin output within the transaction.
func (tb *transactionBuilder) AddUplocoinOutput(output types.UplocoinOutput) uint64 {
	tb.transaction.UplocoinOutputs = append(tb.transaction.UplocoinOutputs, output)
	return uint64(len(tb.transaction.UplocoinOutputs) - 1)
}

// ReplaceUplocoinOutput replaces the Uplocoin output in the transaction at the
// given index.
func (tb *transactionBuilder) ReplaceUplocoinOutput(index uint64, output types.UplocoinOutput) error {
	if index >= uint64(len(tb.transaction.UplocoinOutputs)) {
		return errReplaceIndexOutOfBounds
	}
	tb.transaction.UplocoinOutputs[index] = output
	return nil
}

// AddFileContract adds a file contract to the transaction, returning the index
// of the file contract within the transaction.
func (tb *transactionBuilder) AddFileContract(fc types.FileContract) uint64 {
	tb.transaction.FileContracts = append(tb.transaction.FileContracts, fc)
	return uint64(len(tb.transaction.FileContracts) - 1)
}

// AddFileContractRevision adds a file contract revision to the transaction,
// returning the index of the file contract revision within the transaction.
// When 'Sign' gets called, this revision will be left unsigned.
func (tb *transactionBuilder) AddFileContractRevision(fcr types.FileContractRevision) uint64 {
	tb.transaction.FileContractRevisions = append(tb.transaction.FileContractRevisions, fcr)
	return uint64(len(tb.transaction.FileContractRevisions) - 1)
}

// AddStorageProof adds a storage proof to the transaction, returning the index
// of the storage proof within the transaction.
func (tb *transactionBuilder) AddStorageProof(sp types.StorageProof) uint64 {
	tb.transaction.StorageProofs = append(tb.transaction.StorageProofs, sp)
	return uint64(len(tb.transaction.StorageProofs) - 1)
}

// AddUplofundInput adds a uplofund input to the transaction, returning the index
// of the uplofund input within the transaction. When 'Sign' is called, this
// input will be left unsigned.
func (tb *transactionBuilder) AddUplofundInput(input types.UplofundInput) uint64 {
	tb.transaction.UplofundInputs = append(tb.transaction.UplofundInputs, input)
	return uint64(len(tb.transaction.UplofundInputs) - 1)
}

// AddUplofundOutput adds a uplofund output to the transaction, returning the
// index of the uplofund output within the transaction.
func (tb *transactionBuilder) AddUplofundOutput(output types.UplofundOutput) uint64 {
	tb.transaction.UplofundOutputs = append(tb.transaction.UplofundOutputs, output)
	return uint64(len(tb.transaction.UplofundOutputs) - 1)
}

// AddArbitraryData adds arbitrary data to the transaction, returning the index
// of the data within the transaction.
func (tb *transactionBuilder) AddArbitraryData(arb []byte) uint64 {
	tb.transaction.ArbitraryData = append(tb.transaction.ArbitraryData, arb)
	return uint64(len(tb.transaction.ArbitraryData) - 1)
}

// AddTransactionSignature adds a transaction signature to the transaction,
// returning the index of the signature within the transaction. The signature
// should already be valid, and shouldn't sign any of the inputs that were
// added by calling 'FundUplocoins' or 'FundUplofunds'.
func (tb *transactionBuilder) AddTransactionSignature(sig types.TransactionSignature) uint64 {
	tb.transaction.TransactionSignatures = append(tb.transaction.TransactionSignatures, sig)
	return uint64(len(tb.transaction.TransactionSignatures) - 1)
}

// Drop discards all of the outputs in a transaction, returning them to the
// pool so that other transactions may use them. 'Drop' should only be called
// if a transaction is both unsigned and will not be used any further.
func (tb *transactionBuilder) Drop() {
	tb.wallet.mu.Lock()
	defer tb.wallet.mu.Unlock()

	// Iterate through all parents and the transaction itself and restore all
	// outputs to the list of available outputs.
	txns := append(tb.parents, tb.transaction)
	for _, txn := range txns {
		for _, sci := range txn.UplocoinInputs {
			dbDeleteSpentOutput(tb.wallet.dbTx, types.OutputID(sci.ParentID))
		}
	}

	tb.parents = nil
	tb.signed = false
	tb.transaction = types.Transaction{}

	tb.newParents = nil
	tb.UplocoinInputs = nil
	tb.uplofundInputs = nil
	tb.transactionSignatures = nil
}

// Sign will sign any inputs added by 'FundUplocoins' or 'FundUplofunds' and
// return a transaction set that contains all parents prepended to the
// transaction. If more fields need to be added, a new transaction builder will
// need to be created.
//
// If the whole transaction flag is set to true, then the whole transaction
// flag will be set in the covered fields object. If the whole transaction flag
// is set to false, then the covered fields object will cover all fields that
// have already been added to the transaction, but will also leave room for
// more fields to be added.
//
// Sign should not be called more than once. If, for some reason, there is an
// error while calling Sign, the builder should be dropped.
func (tb *transactionBuilder) Sign(wholeTransaction bool) ([]types.Transaction, error) {
	if tb.signed {
		return nil, errBuilderAlreadySigned
	}

	tb.wallet.mu.Lock()
	consensusHeight, err := dbGetConsensusHeight(tb.wallet.dbTx)
	tb.wallet.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// Create the coveredfields struct.
	var coveredFields types.CoveredFields
	if wholeTransaction {
		coveredFields = types.CoveredFields{WholeTransaction: true}
	} else {
		for i := range tb.transaction.MinerFees {
			coveredFields.MinerFees = append(coveredFields.MinerFees, uint64(i))
		}
		for i := range tb.transaction.UplocoinInputs {
			coveredFields.UplocoinInputs = append(coveredFields.UplocoinInputs, uint64(i))
		}
		for i := range tb.transaction.UplocoinOutputs {
			coveredFields.UplocoinOutputs = append(coveredFields.UplocoinOutputs, uint64(i))
		}
		for i := range tb.transaction.FileContracts {
			coveredFields.FileContracts = append(coveredFields.FileContracts, uint64(i))
		}
		for i := range tb.transaction.FileContractRevisions {
			coveredFields.FileContractRevisions = append(coveredFields.FileContractRevisions, uint64(i))
		}
		for i := range tb.transaction.StorageProofs {
			coveredFields.StorageProofs = append(coveredFields.StorageProofs, uint64(i))
		}
		for i := range tb.transaction.UplofundInputs {
			coveredFields.UplofundInputs = append(coveredFields.UplofundInputs, uint64(i))
		}
		for i := range tb.transaction.UplofundOutputs {
			coveredFields.UplofundOutputs = append(coveredFields.UplofundOutputs, uint64(i))
		}
		for i := range tb.transaction.ArbitraryData {
			coveredFields.ArbitraryData = append(coveredFields.ArbitraryData, uint64(i))
		}
	}
	// TransactionSignatures don't get covered by the 'WholeTransaction' flag,
	// and must be covered manually.
	for i := range tb.transaction.TransactionSignatures {
		coveredFields.TransactionSignatures = append(coveredFields.TransactionSignatures, uint64(i))
	}

	// For each Uplocoin input in the transaction that we added, provide a
	// signature.
	tb.wallet.mu.RLock()
	defer tb.wallet.mu.RUnlock()
	for _, inputIndex := range tb.UplocoinInputs {
		input := tb.transaction.UplocoinInputs[inputIndex]
		key, ok := tb.wallet.keys[input.UnlockConditions.UnlockHash()]
		if !ok {
			return nil, errors.New("transaction builder added an input that it cannot sign")
		}
		newSigIndices := addSignatures(&tb.transaction, coveredFields, input.UnlockConditions, crypto.Hash(input.ParentID), key, consensusHeight)
		tb.transactionSignatures = append(tb.transactionSignatures, newSigIndices...)
		tb.signed = true // Signed is set to true after one successful signature to indicate that future signings can cause issues.
	}
	for _, inputIndex := range tb.uplofundInputs {
		input := tb.transaction.UplofundInputs[inputIndex]
		key, ok := tb.wallet.keys[input.UnlockConditions.UnlockHash()]
		if !ok {
			return nil, errors.New("transaction builder added an input that it cannot sign")
		}
		newSigIndices := addSignatures(&tb.transaction, coveredFields, input.UnlockConditions, crypto.Hash(input.ParentID), key, consensusHeight)
		tb.transactionSignatures = append(tb.transactionSignatures, newSigIndices...)
		tb.signed = true // Signed is set to true after one successful signature to indicate that future signings can cause issues.
	}

	// Get the transaction set and delete the transaction from the registry.
	txnSet := append(tb.parents, tb.transaction)
	return txnSet, nil
}

// ViewTransaction returns a transaction-in-progress along with all of its
// parents, specified by id. An error is returned if the id is invalid.  Note
// that ids become invalid for a transaction after 'SignTransaction' has been
// called because the transaction gets deleted.
func (tb *transactionBuilder) View() (types.Transaction, []types.Transaction) {
	return tb.transaction, tb.parents
}

// ViewAdded returns all of the Uplocoin inputs, uplofund inputs, and parent
// transactions that have been automatically added by the builder.
func (tb *transactionBuilder) ViewAdded() (newParents, UplocoinInputs, uplofundInputs, transactionSignatures []int) {
	return tb.newParents, tb.UplocoinInputs, tb.uplofundInputs, tb.transactionSignatures
}

// registerTransaction takes a transaction and its parents and returns a
// wallet.TransactionBuilder which can be used to expand the transaction. The
// most typical call is 'RegisterTransaction(types.Transaction{}, nil)', which
// registers a new transaction without parents.
func (w *Wallet) registerTransaction(t types.Transaction, parents []types.Transaction) *transactionBuilder {
	// Create a deep copy of the transaction and parents by encoding them. A
	// deep copy ensures that there are no pointer or slice related errors -
	// the builder will be working directly on the transaction, and the
	// transaction may be in use elsewhere (in this case, the host is using the
	// transaction.
	pBytes := encoding.Marshal(parents)
	var pCopy []types.Transaction
	err := encoding.Unmarshal(pBytes, &pCopy)
	if err != nil {
		w.log.Critical(err)
	}
	tBytes := encoding.Marshal(t)
	var tCopy types.Transaction
	err = encoding.Unmarshal(tBytes, &tCopy)
	if err != nil {
		w.log.Critical(err)
	}
	return &transactionBuilder{
		parents:     pCopy,
		transaction: tCopy,

		wallet: w,
	}
}

// RegisterTransaction takes a transaction and its parents and returns a
// modules.TransactionBuilder which can be used to expand the transaction. The
// most typical call is 'RegisterTransaction(types.Transaction{}, nil)', which
// registers a new transaction without parents.
func (w *Wallet) RegisterTransaction(t types.Transaction, parents []types.Transaction) (modules.TransactionBuilder, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.registerTransaction(t, parents), nil
}

// StartTransaction is a convenience function that calls
// RegisterTransaction(types.Transaction{}, nil).
func (w *Wallet) StartTransaction() (modules.TransactionBuilder, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	return w.RegisterTransaction(types.Transaction{}, nil)
}
