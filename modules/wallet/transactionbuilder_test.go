package wallet

import (
	"sync"
	"testing"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
)

// addBlockNoPayout adds a block to the wallet tester that does not have any
// payouts.
func (wt *walletTester) addBlockNoPayout() error {
	block, target, err := wt.miner.BlockForWork()
	if err != nil {
		return err
	}
	// Clear the miner payout so that the wallet is not getting additional
	// outputs from these blocks.
	for i := range block.MinerPayouts {
		block.MinerPayouts[i].UnlockHash = types.UnlockHash{}
	}

	// Solve and submit the block.
	solvedBlock, _ := wt.miner.SolveBlock(block, target)
	err = wt.cs.AcceptBlock(solvedBlock)
	if err != nil {
		return err
	}
	return nil
}

// TestViewAdded checks that 'ViewAdded' returns sane-seeming values when
// indicating which elements have been added automatically to a transaction
// set.
func TestViewAdded(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Mine an extra block to get more outputs - the wallet is going to be
	// loading two transactions at the same time.
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Create a transaction, add money to it, spend the money in a miner fee
	// but do not sign the transaction. The format of this test mimics the way
	// that the host-renter protocol behaves when building a file contract
	// transaction.
	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(100e9)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}
	_ = b.AddMinerFee(txnFund)
	_ = b.AddUplocoinOutput(types.UplocoinOutput{Value: txnFund})
	unfinishedTxn, unfinishedParents := b.View()

	// Create a second builder that extends the first, unsigned transaction. Do
	// not sign the transaction, but do give the extensions to the original
	// builder.
	b2, err := wt.wallet.RegisterTransaction(unfinishedTxn, unfinishedParents)
	if err != nil {
		t.Fatal(err)
	}
	err = b2.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}
	unfinishedTxn2, unfinishedParents2 := b2.View()
	newParentIndices, newInputIndices, _, _ := b2.ViewAdded()

	// Add the new elements from b2 to b and sign the transaction, fetching the
	// signature for b.
	for _, parentIndex := range newParentIndices {
		b.AddParents([]types.Transaction{unfinishedParents2[parentIndex]})
	}
	for _, inputIndex := range newInputIndices {
		b.AddUplocoinInput(unfinishedTxn2.UplocoinInputs[inputIndex])
	}
	// Signing with WholeTransaction=true makes the transaction more brittle to
	// construction mistakes, meaning that an error is more likely to turn up.
	set1, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	if set1[len(set1)-1].ID() == unfinishedTxn.ID() {
		t.Error("seems like there's memory sharing happening between txn calls")
	}
	// Set1 should be missing some signatures.
	err = wt.tpool.AcceptTransactionSet(set1)
	if err == nil {
		t.Fatal(err)
	}
	unfinishedTxn3, _ := b.View()
	// Only the new signatures are needed because the previous call to 'View'
	// included everything else.
	_, _, _, newTxnSignaturesIndices := b.ViewAdded()

	// Add the new signatures to b2, and then sign b2's inputs. The resulting
	// set from b2 should be valid.
	for _, sigIndex := range newTxnSignaturesIndices {
		b2.AddTransactionSignature(unfinishedTxn3.TransactionSignatures[sigIndex])
	}
	set2, err := b2.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(set2)
	if err != nil {
		t.Fatal(err)
	}
	finishedTxn, _ := b2.View()
	_, _, _, newTxnSignaturesIndices3 := b2.ViewAdded()

	// Add the new signatures from b2 to the b1 transaction, which should
	// complete the transaction and create a transaction set in 'b' that is
	// identical to the transaction set that is in b2.
	for _, sigIndex := range newTxnSignaturesIndices3 {
		b.AddTransactionSignature(finishedTxn.TransactionSignatures[sigIndex])
	}
	set3Txn, set3Parents := b.View()
	err = wt.tpool.AcceptTransactionSet(append(set3Parents, set3Txn))
	if !errors.Contains(err, modules.ErrDuplicateTransactionSet) {
		t.Fatal(err)
	}
}

// TestDoubleSignError checks that an error is returned if there is a problem
// when trying to call 'Sign' on a transaction twice.
func TestDoubleSignError(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a transaction, add money to it, and then call sign twice.
	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(100e9)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}
	_ = b.AddMinerFee(txnFund)
	txnSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	txnSet2, err := b.Sign(true)
	if !errors.Contains(err, errBuilderAlreadySigned) {
		t.Error("the wrong error is being returned after a double call to sign")
	}
	if err != nil && txnSet2 != nil {
		t.Error("errored call to sign did not return a nil txn set")
	}
	err = wt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Fatal(err)
	}
}

// TestConcurrentBuilders checks that multiple transaction builders can safely
// be opened at the same time, and that they will make valid transactions when
// building concurrently.
func TestConcurrentBuilders(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Mine a few more blocks so that the wallet has lots of outputs to pick
	// from.
	for i := 0; i < 5; i++ {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Get a baseline balance for the wallet.
	startingSCConfirmed, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	startingOutgoing, startingIncoming, err := wt.wallet.UnconfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingOutgoing.IsZero() {
		t.Fatal(startingOutgoing)
	}
	if !startingIncoming.IsZero() {
		t.Fatal(startingIncoming)
	}

	// Create two builders at the same time, then add money to each.
	builder1, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	builder2, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	// Fund each builder with a Uplocoin output that is smaller than all of the
	// outputs that the wallet should currently have.
	funding := types.NewCurrency64(10e3).Mul(types.UplocoinPrecision)
	err = builder1.FundUplocoins(funding)
	if err != nil {
		t.Fatal(err)
	}
	err = builder2.FundUplocoins(funding)
	if err != nil {
		t.Fatal(err)
	}

	// Get a second reading on the wallet's balance.
	fundedSCConfirmed, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingSCConfirmed.Equals(fundedSCConfirmed) {
		t.Fatal("confirmed Uplocoin balance changed when no blocks have been mined", startingSCConfirmed, fundedSCConfirmed)
	}

	// Spend the transaction funds on miner fees and the void output.
	builder1.AddMinerFee(types.NewCurrency64(25).Mul(types.UplocoinPrecision))
	builder2.AddMinerFee(types.NewCurrency64(25).Mul(types.UplocoinPrecision))
	// Send the money to the void.
	output := types.UplocoinOutput{Value: types.NewCurrency64(9975).Mul(types.UplocoinPrecision)}
	builder1.AddUplocoinOutput(output)
	builder2.AddUplocoinOutput(output)

	// Sign the transactions and verify that both are valid.
	tset1, err := builder1.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	tset2, err := builder2.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(tset1)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(tset2)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a block to get the transaction sets into the blockchain.
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
}

// TestConcurrentBuildersSingleOutput probes the behavior when multiple
// builders are created at the same time, but there is only a single wallet
// output that they end up needing to share.
func TestConcurrentBuildersSingleOutput(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Mine MaturityDelay blocks on the wallet using blocks that don't give
	// miner payouts to the wallet, so that all outputs can be condensed into a
	// single confirmed output. Currently the wallet will be getting a new
	// output per block because it has mined some blocks that haven't had their
	// outputs matured.
	for i := types.BlockHeight(0); i < types.MaturityDelay+1; i++ {
		err = wt.addBlockNoPayout()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Send all coins to a single confirmed output for the wallet.
	unlockConditions, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	scBal, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	// Use a custom builder so that there is no transaction fee.
	builder, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	err = builder.FundUplocoins(scBal)
	if err != nil {
		t.Fatal(err)
	}
	output := types.UplocoinOutput{
		Value:      scBal,
		UnlockHash: unlockConditions.UnlockHash(),
	}
	builder.AddUplocoinOutput(output)
	tSet, err := builder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(tSet)
	if err != nil {
		t.Fatal(err)
	}
	// Get the transaction into the blockchain without giving a miner payout to
	// the wallet.
	err = wt.addBlockNoPayout()
	if err != nil {
		t.Fatal(err)
	}

	// Get a baseline balance for the wallet.
	startingSCConfirmed, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	startingOutgoing, startingIncoming, err := wt.wallet.UnconfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingOutgoing.IsZero() {
		t.Fatal(startingOutgoing)
	}
	if !startingIncoming.IsZero() {
		t.Fatal(startingIncoming)
	}

	// Create two builders at the same time, then add money to each.
	builder1, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	builder2, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	// Fund each builder with a Uplocoin output.
	funding := types.NewCurrency64(10e3).Mul(types.UplocoinPrecision)
	err = builder1.FundUplocoins(funding)
	if err != nil {
		t.Fatal(err)
	}
	// This add should fail, blocking the builder from completion.
	err = builder2.FundUplocoins(funding)
	if !errors.Contains(err, modules.ErrIncompleteTransactions) {
		t.Fatal(err)
	}

	// Get a second reading on the wallet's balance.
	fundedSCConfirmed, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingSCConfirmed.Equals(fundedSCConfirmed) {
		t.Fatal("confirmed Uplocoin balance changed when no blocks have been mined", startingSCConfirmed, fundedSCConfirmed)
	}

	// Spend the transaction funds on miner fees and the void output.
	builder1.AddMinerFee(types.NewCurrency64(25).Mul(types.UplocoinPrecision))
	// Send the money to the void.
	output = types.UplocoinOutput{Value: types.NewCurrency64(9975).Mul(types.UplocoinPrecision)}
	builder1.AddUplocoinOutput(output)

	// Sign the transaction and submit it.
	tset1, err := builder1.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(tset1)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a block to get the transaction sets into the blockchain.
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
}

// TestParallelBuilders checks that multiple transaction builders can safely be
// opened at the same time, and that they will make valid transactions when
// building concurrently, using multiple gothreads to manage the builders.
func TestParallelBuilders(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Mine a few more blocks so that the wallet has lots of outputs to pick
	// from.
	outputsDesired := 10
	for i := 0; i < outputsDesired; i++ {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Add MatruityDelay blocks with no payout to make tracking the balance
	// easier.
	for i := types.BlockHeight(0); i < types.MaturityDelay+1; i++ {
		err = wt.addBlockNoPayout()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Get a baseline balance for the wallet.
	startingSCConfirmed, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	startingOutgoing, startingIncoming, err := wt.wallet.UnconfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingOutgoing.IsZero() {
		t.Fatal(startingOutgoing)
	}
	if !startingIncoming.IsZero() {
		t.Fatal(startingIncoming)
	}

	// Create several builders in parallel.
	var wg sync.WaitGroup
	funding := types.NewCurrency64(10e3).Mul(types.UplocoinPrecision)
	for i := 0; i < outputsDesired; i++ {
		wg.Add(1)
		go func() {
			// Create the builder and fund the transaction.
			builder, err := wt.wallet.StartTransaction()
			if err != nil {
				t.Fatal(err)
			}
			err = builder.FundUplocoins(funding)
			if err != nil {
				t.Fatal(err)
			}

			// Spend the transaction funds on miner fees and the void output.
			builder.AddMinerFee(types.NewCurrency64(25).Mul(types.UplocoinPrecision))
			output := types.UplocoinOutput{Value: types.NewCurrency64(9975).Mul(types.UplocoinPrecision)}
			builder.AddUplocoinOutput(output)
			// Sign the transactions and verify that both are valid.
			tset, err := builder.Sign(true)
			if err != nil {
				t.Fatal(err)
			}
			err = wt.tpool.AcceptTransactionSet(tset)
			if err != nil {
				t.Fatal(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()

	// Mine a block to get the transaction sets into the blockchain.
	err = wt.addBlockNoPayout()
	if err != nil {
		t.Fatal(err)
	}

	// Check the final balance.
	endingSCConfirmed, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	expected := startingSCConfirmed.Sub(funding.Mul(types.NewCurrency64(uint64(outputsDesired))))
	if !expected.Equals(endingSCConfirmed) {
		t.Fatal("did not get the expected ending balance", expected, endingSCConfirmed, startingSCConfirmed)
	}
}

// TestUnconfirmedParents tests the functionality of the transaction builder's
// UnconfirmedParents method.
func TestUnconfirmedParents(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), &modules.ProductionDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Send all of the wallet's available balance to itself.
	uc, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal("Failed to get address", err)
	}
	Uplocoins, _, _, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	tSet, err := wt.wallet.SendUplocoins(Uplocoins.Sub(types.UplocoinPrecision), uc.UnlockHash())
	if err != nil {
		t.Fatal("Failed to send coins", err)
	}

	// Create a transaction. That transaction should use Uplocoin outputs from
	// the unconfirmed transactions in tSet as inputs and is therefore a child
	// of tSet.
	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(1e3)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}

	// UnconfirmedParents should return the transactions of the transaction set
	// we used to send money to ourselves.
	parents, err := b.UnconfirmedParents()
	if err != nil {
		t.Fatal(err)
	}
	if len(tSet) != len(parents) {
		t.Fatal("parents should have same length as unconfirmed transaction set")
	}
	for i := 0; i < len(tSet); i++ {
		if tSet[i].ID() != parents[i].ID() {
			t.Error("returned parent doesn't match transaction of transaction set")
		}
	}
}

// TestDoubleSpendCreation tests functionality used by the renter watchdog to
// create double-spend sweep transactions.
// when trying to call 'Sign' on a transaction twice.
func TestDoubleSpendCreation(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a transaction, add money to it.
	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(100e9)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}

	// Create a copy of this builder for double-spending.
	copyBuilder := b.Copy()

	// Add an output to the original builder, and then a different output to the
	// double-spend copy.
	unlockConditions, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	output := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: unlockConditions.UnlockHash(),
	}
	b.AddUplocoinOutput(output)

	unlockConditions2, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	output2 := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: unlockConditions2.UnlockHash(),
	}
	copyBuilder.AddUplocoinOutput(output2)

	// Sign both transaction sets.
	originalSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	doubleSpendSet, err := copyBuilder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the original set is acceptable, and that the double-spend fails.
	err = wt.tpool.AcceptTransactionSet(originalSet)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(doubleSpendSet)
	if err == nil {
		t.Fatal("Expected double spend to fail", err)
	}
}

// TestReplaceOutput tests the ReplaceUplocoinOutput feature of the
// transactionbuilder. It makes sure that after swapping an output, the builder
// can still produce a valid transaction.
func TestReplaceOutput(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(100e9)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}

	unusedUC, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	unusedOutput := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: unusedUC.UnlockHash(),
	}
	b.AddUplocoinOutput(unusedOutput)

	replacementOutputUC, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	replacementOutput := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: replacementOutputUC.UnlockHash(),
	}
	b.ReplaceUplocoinOutput(0, replacementOutput)

	txnSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the unused output is not in the transaction set, and that the
	// replacement output is found.
	foundReplacementOutput := false
	for _, txn := range txnSet {
		for _, scOutput := range txn.UplocoinOutputs {
			if scOutput.UnlockHash == unusedOutput.UnlockHash {
				t.Fatal("Did not expect to find replaced output in set")
			}
			if scOutput.UnlockHash == replacementOutput.UnlockHash {
				foundReplacementOutput = true
			}
		}
	}
	if !foundReplacementOutput {
		t.Fatal("Did not find output added via replacement")
	}

	err = wt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Fatal(err)
	}
}

// TestMarkWalletInputs tests that MarkWalletInputs marks spendable inputs
// correctly by checking that a re-registered transaction is still spendable.
func TestMarkWalletInputs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}

	// Add an input and output to the transaction.
	txnFund := types.NewCurrency64(100e9)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}
	uc, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	output := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: uc.UnlockHash(),
	}
	b.AddUplocoinOutput(output)

	// Create a new builder from the View output.
	txn, parents := b.View()
	newBuilder, err := wt.wallet.RegisterTransaction(txn, parents)
	if err != nil {
		t.Fatal(err)
	}
	markedAnyInputs := newBuilder.MarkWalletInputs()
	if !markedAnyInputs {
		t.Fatal("Expected to mark some inputs")
	}

	// Call MarkWalletInputs 10 more times. None of these iterations should mark
	// any inputs again.
	for i := 0; i < 10; i++ {
		if newBuilder.MarkWalletInputs() {
			t.Fatal("Expected no inputs to be marked")
		}
	}

	// Check that the new builder is signable and that it creates a good
	// transaction set.
	txnSet, err := newBuilder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Fatal(err)
	}
}

// TestDoubleSpendAfterMarking tests functionality used by the renter
// watchdog to create double-spend sweep transactions.
func TestDoubleSpendAfterMarking(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.closeWt(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a transaction, add money to it.
	b, err := wt.wallet.StartTransaction()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(100e9)
	err = b.FundUplocoins(txnFund)
	if err != nil {
		t.Fatal(err)
	}

	// Create a copy of this builder for double-spending.
	copyBuilder := b.Copy()

	// Add an output to the original builder, and then a different output to the
	// double-spend copy.
	unlockConditions, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	output := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: unlockConditions.UnlockHash(),
	}
	b.AddUplocoinOutput(output)

	unlockConditions2, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	output2 := types.UplocoinOutput{
		Value:      txnFund,
		UnlockHash: unlockConditions2.UnlockHash(),
	}
	outputIndex := copyBuilder.AddUplocoinOutput(output2)

	// Get a view of the copyBuilder and re-register the transactions and mark the
	// spendable inputs.
	copyTxn, copyParents := copyBuilder.View()
	newCopyBuilder, err := wt.wallet.RegisterTransaction(copyTxn, copyParents)
	markedAnyInputs := newCopyBuilder.MarkWalletInputs()
	if !markedAnyInputs {
		t.Fatal("expected to mark inputs")
	}

	// Replace the output with a a completely different one, and add a fee.
	fee := types.NewCurrency64(50e9)
	unlockConditions3, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	output3 := types.UplocoinOutput{
		Value:      txnFund.Sub(fee),
		UnlockHash: unlockConditions3.UnlockHash(),
	}
	err = newCopyBuilder.ReplaceUplocoinOutput(outputIndex, output3)
	newCopyBuilder.AddMinerFee(fee)

	// Sign both transaction sets.
	originalSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	alteredSet, err := newCopyBuilder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the output we just added is there by checking the unlockhash.
	if len(alteredSet) < 1 {
		t.Fatal(err)
	}
	if len(alteredSet[1].UplocoinOutputs) < int(outputIndex) {
		t.Fatal(err)
	}
	foundUnlockHash := alteredSet[1].UplocoinOutputs[outputIndex].UnlockHash
	if foundUnlockHash != unlockConditions3.UnlockHash() {
		t.Fatal(err)
	}

	// Check that the altered set is acceptable, and that the original set fails
	// because it is a double-spend.
	err = wt.tpool.AcceptTransactionSet(alteredSet)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(originalSet)
	if err == nil {
		t.Fatal("Expected double spend to fail", err)
	}
}
