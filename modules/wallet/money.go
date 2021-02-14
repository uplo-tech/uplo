package wallet

import (
	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
)

// estimatedTransactionSize is the estimated size of a transaction used to send
// Uplocoins.
const estimatedTransactionSize = 750

// sortedOutputs is a struct containing a slice of Uplocoin outputs and their
// corresponding ids. sortedOutputs can be sorted using the sort package.
type sortedOutputs struct {
	ids     []types.UplocoinOutputID
	outputs []types.UplocoinOutput
}

// DustThreshold returns the quantity per byte below which a Currency is
// considered to be Dust.
func (w *Wallet) DustThreshold() (types.Currency, error) {
	if err := w.tg.Add(); err != nil {
		return types.Currency{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	minFee, _ := w.tpool.FeeEstimation()
	return minFee.Mul64(3), nil
}

// ConfirmedBalance returns the balance of the wallet according to all of the
// confirmed transactions.
func (w *Wallet) ConfirmedBalance() (UplocoinBalance types.Currency, uplofundBalance types.Currency, uplofundClaimBalance types.Currency, err error) {
	if err := w.tg.Add(); err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// ensure durability of reported balance
	if err = w.syncDB(); err != nil {
		return
	}

	dbForEachUplocoinOutput(w.dbTx, func(_ types.UplocoinOutputID, sco types.UplocoinOutput) {
		if sco.Value.Cmp(dustThreshold) > 0 {
			UplocoinBalance = UplocoinBalance.Add(sco.Value)
		}
	})

	uplofundPool, err := dbGetUplofundPool(w.dbTx)
	if err != nil {
		return
	}
	dbForEachUplofundOutput(w.dbTx, func(_ types.UplofundOutputID, sfo types.UplofundOutput) {
		uplofundBalance = uplofundBalance.Add(sfo.Value)
		if sfo.ClaimStart.Cmp(uplofundPool) > 0 {
			// Skip claims larger than the uplofund pool. This should only
			// occur if the uplofund pool has not been initialized yet.
			w.log.Debugf("skipping claim with start value %v because uplofund pool is only %v", sfo.ClaimStart, uplofundPool)
			return
		}
		uplofundClaimBalance = uplofundClaimBalance.Add(uplofundPool.Sub(sfo.ClaimStart).Mul(sfo.Value).Div(types.UplofundCount))
	})
	return
}

// UnconfirmedBalance returns the number of outgoing and incoming Uplocoins in
// the unconfirmed transaction set. Refund outputs are included in this
// reporting.
func (w *Wallet) UnconfirmedBalance() (outgoingUplocoins types.Currency, incomingUplocoins types.Currency, err error) {
	if err := w.tg.Add(); err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, upt := range w.unconfirmedProcessedTransactions {
		for _, input := range upt.Inputs {
			if input.FundType == types.SpecifierUplocoinInput && input.WalletAddress {
				outgoingUplocoins = outgoingUplocoins.Add(input.Value)
			}
		}
		for _, output := range upt.Outputs {
			if output.FundType == types.SpecifierUplocoinOutput && output.WalletAddress && output.Value.Cmp(dustThreshold) > 0 {
				incomingUplocoins = incomingUplocoins.Add(output.Value)
			}
		}
	}
	return
}

// SendUplocoins creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned. Fees
// are added to the amount sent.
func (w *Wallet) SendUplocoins(amount types.Currency, dest types.UnlockHash) ([]types.Transaction, error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()

	_, fee := w.tpool.FeeEstimation()
	fee = fee.Mul64(estimatedTransactionSize)
	return w.managedSendUplocoins(amount, fee, dest)
}

// SendUplocoinsFeeIncluded creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned. Fees
// are subtracted from the amount sent.
func (w *Wallet) SendUplocoinsFeeIncluded(amount types.Currency, dest types.UnlockHash) ([]types.Transaction, error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()

	_, fee := w.tpool.FeeEstimation()
	fee = fee.Mul64(estimatedTransactionSize)
	// Don't allow sending an amount equal to the fee, as zero spending is not
	// allowed and would error out later.
	if amount.Cmp(fee) <= 0 {
		w.log.Println("Attempt to send coins has failed - not enough to cover fee")
		return nil, errors.AddContext(modules.ErrLowBalance, "not enough coins to cover fee")
	}
	return w.managedSendUplocoins(amount.Sub(fee), fee, dest)
}

// managedSendUplocoins creates a transaction sending 'amount' to 'dest'. The
// transaction is submitted to the transaction pool and is also returned.
func (w *Wallet) managedSendUplocoins(amount, fee types.Currency, dest types.UnlockHash) (txns []types.Transaction, err error) {
	// Check if consensus is synced
	if !w.cs.Synced() || w.deps.Disrupt("UnsyncedConsensus") {
		return nil, errors.New("cannot send Uplocoin until fully synced")
	}

	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		w.log.Println("Attempt to send coins has failed - wallet is locked")
		return nil, modules.ErrLockedWallet
	}

	output := types.UplocoinOutput{
		Value:      amount,
		UnlockHash: dest,
	}

	txnBuilder, err := w.StartTransaction()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			txnBuilder.Drop()
		}
	}()
	err = txnBuilder.FundUplocoins(amount.Add(fee))
	if err != nil {
		w.log.Println("Attempt to send coins has failed - failed to fund transaction:", err)
		return nil, build.ExtendErr("unable to fund transaction", err)
	}
	txnBuilder.AddMinerFee(fee)
	txnBuilder.AddUplocoinOutput(output)
	txnSet, err := txnBuilder.Sign(true)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - failed to sign transaction:", err)
		return nil, build.ExtendErr("unable to sign transaction", err)
	}
	if w.deps.Disrupt("SendUplocoinsInterrupted") {
		return nil, errors.New("failed to accept transaction set (SendUplocoinsInterrupted)")
	}
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - transaction pool rejected transaction:", err)
		return nil, build.ExtendErr("unable to get transaction accepted", err)
	}
	w.log.Println("Submitted a Uplocoin transfer transaction set for value", amount.HumanString(), "with fees", fee.HumanString(), "IDs:")
	for _, txn := range txnSet {
		w.log.Println("\t", txn.ID())
	}
	return txnSet, nil
}

// SendUplocoinsMulti creates a transaction that includes the specified
// outputs. The transaction is submitted to the transaction pool and is also
// returned.
func (w *Wallet) SendUplocoinsMulti(outputs []types.UplocoinOutput) (txns []types.Transaction, err error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()
	w.log.Println("Beginning call to SendUplocoinsMulti")

	// Check if consensus is synced
	if !w.cs.Synced() || w.deps.Disrupt("UnsyncedConsensus") {
		return nil, errors.New("cannot send Uplocoin until fully synced")
	}

	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		w.log.Println("Attempt to send coins has failed - wallet is locked")
		return nil, modules.ErrLockedWallet
	}

	txnBuilder, err := w.StartTransaction()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			txnBuilder.Drop()
		}
	}()

	// Add estimated transaction fee.
	_, tpoolFee := w.tpool.FeeEstimation()
	tpoolFee = tpoolFee.Mul64(2)                              // We don't want send-to-many transactions to fail.
	tpoolFee = tpoolFee.Mul64(1000 + 60*uint64(len(outputs))) // Estimated transaction size in bytes
	txnBuilder.AddMinerFee(tpoolFee)

	// Calculate total cost to wallet.
	//
	// NOTE: we only want to call FundUplocoins once; that way, it will
	// (ideally) fund the entire transaction with a single input, instead of
	// many smaller ones.
	totalCost := tpoolFee
	for _, sco := range outputs {
		totalCost = totalCost.Add(sco.Value)
	}
	err = txnBuilder.FundUplocoins(totalCost)
	if err != nil {
		return nil, build.ExtendErr("unable to fund transaction", err)
	}

	for _, sco := range outputs {
		txnBuilder.AddUplocoinOutput(sco)
	}

	txnSet, err := txnBuilder.Sign(true)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - failed to sign transaction:", err)
		return nil, build.ExtendErr("unable to sign transaction", err)
	}
	if w.deps.Disrupt("SendUplocoinsInterrupted") {
		return nil, errors.New("failed to accept transaction set (SendUplocoinsInterrupted)")
	}
	w.log.Println("Attempting to broadcast a multi-send over the network")
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - transaction pool rejected transaction:", err)
		return nil, build.ExtendErr("unable to get transaction accepted", err)
	}

	// Log the success.
	var outputList string
	for _, output := range outputs {
		outputList = outputList + "\n\tAddress: " + output.UnlockHash.String() + "\n\tValue: " + output.Value.HumanString() + "\n"
	}
	w.log.Printf("Successfully broadcast transaction with id %v, fee %v, and the following outputs: %v", txnSet[len(txnSet)-1].ID(), tpoolFee.HumanString(), outputList)
	return txnSet, nil
}

// SendUplofunds creates a transaction sending 'amount' to 'dest'. The transaction
// is submitted to the transaction pool and is also returned.
func (w *Wallet) SendUplofunds(amount types.Currency, dest types.UnlockHash) (txns []types.Transaction, err error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()

	// Check if consensus is synced
	if !w.cs.Synced() || w.deps.Disrupt("UnsyncedConsensus") {
		return nil, errors.New("cannot send uplofunds until fully synced")
	}

	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		return nil, modules.ErrLockedWallet
	}

	_, tpoolFee := w.tpool.FeeEstimation()
	tpoolFee = tpoolFee.Mul64(750) // Estimated transaction size in bytes
	tpoolFee = tpoolFee.Mul64(5)   // use large fee to ensure uplofund transactions are selected by miners
	output := types.UplofundOutput{
		Value:      amount,
		UnlockHash: dest,
	}

	txnBuilder, err := w.StartTransaction()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			txnBuilder.Drop()
		}
	}()
	err = txnBuilder.FundUplocoins(tpoolFee)
	if err != nil {
		return nil, err
	}
	err = txnBuilder.FundUplofunds(amount)
	if err != nil {
		return nil, err
	}
	txnBuilder.AddMinerFee(tpoolFee)
	txnBuilder.AddUplofundOutput(output)
	txnSet, err := txnBuilder.Sign(true)
	if err != nil {
		return nil, err
	}
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		return nil, err
	}
	w.log.Println("Submitted a uplofund transfer transaction set for value", amount.HumanString(), "with fees", tpoolFee.HumanString(), "IDs:")
	for _, txn := range txnSet {
		w.log.Println("\t", txn.ID())
	}
	return txnSet, nil
}

// Len returns the number of elements in the sortedOutputs struct.
func (so sortedOutputs) Len() int {
	if build.DEBUG && len(so.ids) != len(so.outputs) {
		panic("sortedOutputs object is corrupt")
	}
	return len(so.ids)
}

// Less returns whether element 'i' is less than element 'j'. The currency
// value of each output is used for comparison.
func (so sortedOutputs) Less(i, j int) bool {
	return so.outputs[i].Value.Cmp(so.outputs[j].Value) < 0
}

// Swap swaps two elements in the sortedOutputs set.
func (so sortedOutputs) Swap(i, j int) {
	so.ids[i], so.ids[j] = so.ids[j], so.ids[i]
	so.outputs[i], so.outputs[j] = so.outputs[j], so.outputs[i]
}
