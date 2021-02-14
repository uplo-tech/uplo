package wallet

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/node"
	"github.com/uplo-tech/uplo/uplotest"
	"github.com/uplo-tech/uplo/uplotest/dependencies"
	"github.com/uplo-tech/uplo/types"
	mnemonics "github.com/uplo-tech/entropy-mnemonics"
	"github.com/uplo-tech/fastrand"
)

// TestTransactionReorg makes sure that a processedTransaction isn't returned
// by the API after being reverted.
func TestTransactionReorg(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create testing directory.
	testdir := walletTestDir(t.Name())

	// Create two miners
	miner1, err := uplotest.NewNode(node.Miner(filepath.Join(testdir, "miner1")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := miner1.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	miner2, err := uplotest.NewNode(node.Miner(filepath.Join(testdir, "miner2")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := miner2.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// miner1 sends a txn to itself and mines it.
	uc, err := miner1.WalletAddressGet()
	if err != nil {
		t.Fatal(err)
	}
	wsp, err := miner1.WalletUplocoinsPost(types.UplocoinPrecision, uc.Address, false)
	if err != nil {
		t.Fatal(err)
	}
	blocks := 1
	for i := 0; i < blocks; i++ {
		if err := miner1.MineBlock(); err != nil {
			t.Fatal(err)
		}
	}
	// wait until the transaction from before shows up as processed.
	txn := wsp.TransactionIDs[len(wsp.TransactionIDs)-1]
	err = build.Retry(100, 100*time.Millisecond, func() error {
		cg, err := miner1.ConsensusGet()
		if err != nil {
			return err
		}
		wtg, err := miner1.WalletTransactionsGet(1, cg.Height)
		if err != nil {
			return err
		}
		for _, t := range wtg.ConfirmedTransactions {
			if t.TransactionID == txn {
				return nil
			}
		}
		return errors.New("txn isn't processed yet")
	})
	if err != nil {
		t.Fatal(err)
	}
	// miner2 mines 2 blocks now to create a longer chain than miner1.
	for i := 0; i < blocks+1; i++ {
		if err := miner2.MineBlock(); err != nil {
			t.Fatal(err)
		}
	}
	// miner1 and miner2 connect. This should cause a reorg that reverts the
	// transaction from before.
	if err := miner1.GatewayConnectPost(miner2.GatewayAddress()); err != nil {
		t.Fatal(err)
	}
	err = build.Retry(100, 100*time.Millisecond, func() error {
		cg, err := miner1.ConsensusGet()
		if err != nil {
			return err
		}
		wtg, err := miner1.WalletTransactionsGet(1, cg.Height)
		if err != nil {
			return err
		}
		for _, t := range wtg.ConfirmedTransactions {
			if t.TransactionID == txn {
				return errors.New("txn is still processed")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestSignTransaction is a integration test for signing transaction offline
// using the API.
func TestSignTransaction(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a new server
	testNode, err := uplotest.NewNode(node.AllModules(walletTestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := testNode.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// get two outputs to spend
	unspentResp, err := testNode.WalletUnspentGet()
	if err != nil {
		t.Fatal("failed to get spendable outputs:", err)
	}
	outputs := unspentResp.Outputs
	wucg1, err := testNode.WalletUnlockConditionsGet(outputs[0].UnlockHash)
	if err != nil {
		t.Fatal("failed to get unlock conditions:", err)
	}
	wucg2, err := testNode.WalletUnlockConditionsGet(outputs[1].UnlockHash)
	if err != nil {
		t.Fatal("failed to get unlock conditions:", err)
	}

	// create a transaction that sends the outputs to the void, with no
	// signatures
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{
			{
				ParentID:         types.UplocoinOutputID(outputs[0].ID),
				UnlockConditions: wucg1.UnlockConditions,
			},
			{
				ParentID:         types.UplocoinOutputID(outputs[1].ID),
				UnlockConditions: wucg2.UnlockConditions,
			},
		},
		UplocoinOutputs: []types.UplocoinOutput{{
			Value:      outputs[0].Value.Add(outputs[1].Value),
			UnlockHash: types.UnlockHash{},
		}},
		TransactionSignatures: []types.TransactionSignature{
			{ParentID: crypto.Hash(outputs[0].ID), CoveredFields: types.CoveredFields{WholeTransaction: true}},
			{ParentID: crypto.Hash(outputs[1].ID), CoveredFields: types.CoveredFields{WholeTransaction: true}},
		},
	}

	// sign the first input
	signResp, err := testNode.WalletSignPost(txn, []crypto.Hash{txn.TransactionSignatures[0].ParentID})
	if err != nil {
		t.Fatal("failed to sign the transaction:", err)
	}
	txn = signResp.Transaction

	// txn should now have one signature
	if len(txn.TransactionSignatures[0].Signature) == 0 {
		t.Fatal("transaction was not signed")
	} else if len(txn.TransactionSignatures[1].Signature) != 0 {
		t.Fatal("second input was also signed")
	}

	// sign the second input
	signResp, err = testNode.WalletSignPost(txn, []crypto.Hash{txn.TransactionSignatures[1].ParentID})
	if err != nil {
		t.Fatal("failed to sign the transaction:", err)
	}
	txn = signResp.Transaction

	// txn should now have both signatures
	if len(txn.TransactionSignatures[0].Signature) == 0 || len(txn.TransactionSignatures[1].Signature) == 0 {
		t.Fatal("transaction was not signed")
	}

	// the resulting transaction should be valid; submit it to the tpool and
	// mine a block to confirm it
	if err := testNode.TransactionPoolRawPost(txn, nil); err != nil {
		t.Fatal("failed to add transaction to pool:", err)
	}
	if err := testNode.MineBlock(); err != nil {
		t.Fatal("failed to mine block", err)
	}

	// the wallet should no longer list the resulting output as spendable
	unspentResp, err = testNode.WalletUnspentGet()
	if err != nil {
		t.Fatal("failed to get spendable outputs")
	}
	for _, output := range unspentResp.Outputs {
		if output.ID == types.OutputID(txn.UplocoinInputs[0].ParentID) {
			t.Fatal("spent output still listed as spendable")
		}
	}
}

// TestWatchOnly tests the ability of the wallet to track addresses that it
// does not own.
func TestWatchOnly(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a new server
	testNode, err := uplotest.NewNode(node.AllModules(walletTestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := testNode.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// create an address manually and send coins to it
	sk, pk := crypto.GenerateKeyPair()
	uc := types.UnlockConditions{
		PublicKeys:         []types.UploPublicKey{types.Ed25519PublicKey(pk)},
		SignaturesRequired: 1,
	}
	addr := uc.UnlockHash()

	_, err = testNode.WalletUplocoinsPost(types.UplocoinPrecision.Mul64(77), addr, false)
	if err != nil {
		t.Fatal(err)
	}
	err = testNode.MineBlock()
	if err != nil {
		t.Fatal(err)
	}

	// the output should not show up in UnspentOutputs, because the address is
	// not being tracked yet
	unspentResp, err := testNode.WalletUnspentGet()
	if err != nil {
		t.Fatal("failed to get spendable outputs:", err)
	} else if len(unspentResp.Outputs) == 0 {
		t.Fatal("expected at least one unspent output")
	}
	for _, o := range unspentResp.Outputs {
		if o.UnlockHash == addr {
			t.Fatal("shouldn't see addr in UnspentOutputs yet")
		}
		if o.IsWatchOnly {
			t.Error("no outputs should be marked watch-only yet")
		}
	}

	// track the address
	err = testNode.WalletWatchAddPost([]types.UnlockHash{addr}, false)
	if err != nil {
		t.Fatal(err)
	}

	// output should now show up
	unspentResp, err = testNode.WalletUnspentGet()
	if err != nil {
		t.Fatal("failed to get spendable outputs:", err)
	}
	var output modules.UnspentOutput
	for _, o := range unspentResp.Outputs {
		if o.UnlockHash == addr {
			output = o
			break
		}
	}
	if output.ID == (types.OutputID{}) {
		t.Fatal("addr not present in UnspentOutputs after WatchAddresses")
	}
	if !output.IsWatchOnly {
		t.Error("output should be marked watch-only")
	}

	// create a transaction that sends an output to the void
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{{
			ParentID:         types.UplocoinOutputID(output.ID),
			UnlockConditions: uc,
		}},
		UplocoinOutputs: []types.UplocoinOutput{{
			Value:      output.Value,
			UnlockHash: types.UnlockHash{},
		}},
		TransactionSignatures: []types.TransactionSignature{{
			ParentID:      crypto.Hash(output.ID),
			CoveredFields: types.CoveredFields{WholeTransaction: true},
		}},
	}

	// sign the transaction
	cg, err := testNode.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	sig := crypto.SignHash(txn.SigHash(0, cg.Height), sk)
	txn.TransactionSignatures[0].Signature = sig[:]

	// the resulting transaction should be valid; submit it to the tpool and
	// mine a block to confirm it
	err = testNode.TransactionPoolRawPost(txn, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = testNode.MineBlock()
	if err != nil {
		t.Fatal(err)
	}

	// the wallet should no longer list the resulting output as spendable
	unspentResp, err = testNode.WalletUnspentGet()
	if err != nil {
		t.Fatal("failed to get spendable outputs:", err)
	}
	for _, o := range unspentResp.Outputs {
		if o.UnlockHash == addr {
			t.Fatal("spent output still listed as spendable")
		}
	}
}

// TestUnspentOutputs tests the UnspentOutputs method of the wallet.
func TestUnspentOutputs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a new server
	testNode, err := uplotest.NewNode(node.AllModules(walletTestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := testNode.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// create a dummy address and send coins to it
	addr := types.UnlockHash{1}

	_, err = testNode.WalletUplocoinsPost(types.UplocoinPrecision.Mul64(77), addr, false)
	if err != nil {
		t.Fatal(err)
	}
	err = testNode.MineBlock()
	if err != nil {
		t.Fatal(err)
	}

	// define a helper function to check whether addr appears in
	// UnspentOutputs
	addrIsPresent := func() bool {
		wug, err := testNode.WalletUnspentGet()
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range wug.Outputs {
			if o.UnlockHash == addr {
				return true
			}
		}
		return false
	}

	// initially, the output should not show up in UnspentOutputs, because the
	// address is not being tracked yet
	if addrIsPresent() {
		t.Fatal("shouldn't see addr in UnspentOutputs yet")
	}

	// add the address, but tell the wallet it hasn't been used yet. The
	// wallet won't rescan, so it still won't see any outputs.
	err = testNode.WalletWatchAddPost([]types.UnlockHash{addr}, true)
	if err != nil {
		t.Fatal(err)
	}
	if addrIsPresent() {
		t.Fatal("shouldn't see addr in UnspentOutputs yet")
	}

	// remove the address, then add it again, this time telling the wallet
	// that it has been used.
	err = testNode.WalletWatchRemovePost([]types.UnlockHash{addr}, true)
	if err != nil {
		t.Fatal(err)
	}
	err = testNode.WalletWatchAddPost([]types.UnlockHash{addr}, false)
	if err != nil {
		t.Fatal(err)
	}

	// output should now show up
	if !addrIsPresent() {
		t.Fatal("addr not present in UnspentOutputs after AddWatchAddresses")
	}

	// remove the address, but tell the wallet that the address hasn't been
	// used. The wallet won't rescan, so the output should still show up.
	err = testNode.WalletWatchRemovePost([]types.UnlockHash{addr}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !addrIsPresent() {
		t.Fatal("addr should still be present in UnspentOutputs")
	}

	// add and remove the address again, this time triggering a rescan. The
	// output should no longer appear.
	err = testNode.WalletWatchAddPost([]types.UnlockHash{addr}, true)
	if err != nil {
		t.Fatal(err)
	}
	err = testNode.WalletWatchRemovePost([]types.UnlockHash{addr}, false)
	if err != nil {
		t.Fatal(err)
	}
	if addrIsPresent() {
		t.Fatal("shouldn't see addr in UnspentOutputs")
	}
}

// TestFileContractUnspentOutputs tests that outputs created from file
// contracts are properly handled by the wallet.
func TestFileContractUnspentOutputs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	gp := uplotest.GroupParams{
		Hosts:   1,
		Miners:  1,
		Renters: 1,
	}
	testDir := uplotest.TestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// pick a renter contract
	renter := tg.Renters()[0]
	rc, err := renter.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	contract := rc.ActiveContracts[0]

	// mine until the contract has ended
	miner := tg.Miners()[0]
	for i := types.BlockHeight(0); i < contract.EndHeight; i++ {
		miner.MineBlock()
	}

	// wallet should report the unspent output (the storage proof is missed
	// cause the contract was renewed and therefore no proof needs to be
	// submitted)
	err = build.Retry(100, 100*time.Millisecond, func() error {
		outputID := contract.ID.StorageProofOutputID(types.ProofMissed, 0)
		wug, err := renter.WalletUnspentGet()
		if err != nil {
			return err
		}
		var found bool
		for _, o := range wug.Outputs {
			if types.UplocoinOutputID(o.ID) == outputID {
				found = true
			}
		}
		if !found {
			return errors.New("wallet's spendable outputs did not contain file contract output")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestWalletLastAddresses tests the /wallet/addresses endpoint with a
// specified count.
func TestWalletLastAddresses(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a new server
	testNode, err := uplotest.NewCleanNode(node.AllModules(uplotest.TestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := testNode.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// The wallet should have 0 addresses.
	wag, err := testNode.WalletAddressesGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(wag.Addresses) != 0 {
		t.Fatal("Wallet should have 0 addresses but had", len(wag.Addresses))
	}
	// Generate n addresses.
	n := 10
	addresses := make([]types.UnlockHash, 0, n)
	for i := 0; i < n; i++ {
		wag, err := testNode.WalletAddressGet()
		if err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, wag.Address)
	}
	// The wallet should have n addresses now.
	wag, err = testNode.WalletAddressesGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(wag.Addresses) != n {
		t.Fatal("Wallet should have 100 addresses but had", len(wag.Addresses))
	}
	// Get the n addresses in reverse order.
	wlag, err := testNode.WalletLastAddressesGet(uint64(n))
	if err != nil {
		t.Fatal(err)
	}
	if len(addresses) != len(wlag.Addresses) {
		t.Fatalf("Expected %v addresses but got %v",
			len(addresses), len(wlag.Addresses))
	}
	// Make sure the returned addresses are the same and have the reversed
	// order of the created ones.
	for i := range wag.Addresses {
		if addresses[i] != wlag.Addresses[len(wlag.Addresses)-1-i] {
			t.Fatal("addresses don't match for i =", i)
		}
	}
	// Get MaxUint64 addresses in reverse order. This should still only return
	// n addresses.
	wlag, err = testNode.WalletLastAddressesGet(math.MaxUint64)
	if err != nil {
		t.Fatal(err)
	}
	// Make sure the returned addresses are the same and have the reversed
	// order of the created ones.
	for i := range wag.Addresses {
		if addresses[i] != wlag.Addresses[len(wlag.Addresses)-1-i] {
			t.Fatal("addresses don't match for i =", i)
		}
	}
}

// TestWalletSend tests sending Uplocoins with and without fees included.
func TestWalletSend(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a testgroup
	groupParams := uplotest.GroupParams{
		Hosts:  0,
		Miners: 1,
	}
	tg, err := uplotest.NewGroupFromTemplate(walletTestDir(t.Name()), groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add 2 renters.
	rp := node.RenterTemplate
	rp.RenterDeps = &dependencies.DependencyPreventEARefill{}
	_, err = tg.AddNodeN(rp, 2)
	if err != nil {
		t.Fatal(err)
	}

	renters := tg.Renters()
	renter1, renter2 := renters[0], renters[1]
	miner := tg.Miners()[0]

	// Get the original balances.
	wg, err := renter1.WalletGet()
	if err != nil {
		t.Fatal(err)
	}
	originalBalance1 := wg.ConfirmedUplocoinBalance
	wg, err = renter2.WalletGet()
	if err != nil {
		t.Fatal(err)
	}
	originalBalance2 := wg.ConfirmedUplocoinBalance

	// Send coins to renter2 without fees included, mine blocks.
	uc, err := renter2.WalletAddressGet()
	if err != nil {
		t.Fatal(err)
	}
	sentAmount := originalBalance1.Div64(2)
	_, err = renter1.WalletUplocoinsPost(sentAmount, uc.Address, false)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(100, 100*time.Millisecond, func() error {
		err = miner.MineBlock()
		if err != nil {
			t.Fatal(err)
		}
		// Check the balance of renter2.
		wg, err = renter2.WalletGet()
		if err != nil {
			t.Fatal(err)
		}
		newBalance2 := wg.ConfirmedUplocoinBalance
		if newBalance2.Cmp(originalBalance2) > 0 {
			return nil
		}

		return errors.New("renter2 hasn't received transaction yet")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Get the wallet and confirm more than what was sent was spent.
	wg, err = renter1.WalletGet()
	if err != nil {
		t.Fatal(err)
	}
	newBalance1 := wg.ConfirmedUplocoinBalance
	if originalBalance1.Sub(newBalance1).Cmp(sentAmount) <= 0 {
		t.Fatal("more than what was sent should have been spent")
	}

	originalBalance1 = newBalance1
	wg, err = renter2.WalletGet()
	if err != nil {
		t.Fatal(err)
	}
	originalBalance2 = wg.ConfirmedUplocoinBalance

	// Send entire balance to renter1 with fees included.
	uc, err = renter1.WalletAddressGet()
	if err != nil {
		t.Fatal(err)
	}
	sentAmount = originalBalance2
	_, err = renter2.WalletUplocoinsPost(sentAmount, uc.Address, true)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(100, 100*time.Millisecond, func() error {
		err = miner.MineBlock()
		if err != nil {
			t.Fatal(err)
		}
		// Check the balance of renter1.
		wg, err = renter1.WalletGet()
		if err != nil {
			t.Fatal(err)
		}
		newBalance1 := wg.ConfirmedUplocoinBalance
		if newBalance1.Cmp(originalBalance1) > 0 {
			return nil
		}

		return errors.New("renter1 hasn't received transaction yet")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Get the wallet and confirm renter2 has no balance remaining.
	wg, err = renter2.WalletGet()
	if err != nil {
		t.Fatal(err)
	}
	newBalance2 := wg.ConfirmedUplocoinBalance
	if newBalance2.Cmp(types.ZeroCurrency) != 0 {
		t.Fatal("an exact amount wasn't spend")
	}
}

// TestWalletSendUnsynced confirms that the wallet will return an error when
// trying to send Uplocoins or uplofunds if the consensus is not fully synced
func TestWalletSendUnsynced(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a wallet with an unsynced consensus dependency
	testDir := walletTestDir(t.Name())
	walletTemplate := node.Wallet(testDir + "/wallet")
	walletTemplate.WalletDeps = &dependencies.DependencyUnsyncedConsensus{}
	walletTemplate.CreateMiner = true
	wallet, err := uplotest.NewNode(walletTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// Check error returned from Uplocoins multi post
	_, err = wallet.WalletUplocoinsMultiPost([]types.UplocoinOutput{})
	if err == nil {
		t.Fatal("expected an error to be returned for not being synced")
	}
	if !strings.Contains(err.Error(), "cannot send Uplocoin until fully synced") {
		t.Fatal("expected to get synced error but got:", err)
	}

	// Check error returned from single Uplocoin post
	_, err = wallet.WalletUplocoinsPost(types.ZeroCurrency, types.UnlockHash{}, false)
	if err == nil {
		t.Fatal("expected an error to be returned for not being synced")
	}
	if !strings.Contains(err.Error(), "cannot send Uplocoin until fully synced") {
		t.Fatal("expected to get synced error but got:", err)
	}

	// Check error returned from uplofund post
	_, err = wallet.WalletUplofundsPost(types.ZeroCurrency, types.UnlockHash{})
	if err == nil {
		t.Fatal("expected an error to be returned for not being synced")
	}
	if !strings.Contains(err.Error(), "cannot send uplofunds until fully synced") {
		t.Fatal("expected to get synced error but got:", err)
	}
}

// TestWalletChangePasswordWithSeed initializes a wallet with a custom password
// and uses the primary seed to change that password.
func TestWalletChangePasswordWithSeed(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a new server
	testNode, err := uplotest.NewNode(node.AllModules(walletTestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := testNode.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Reinit the wallet by using a specific password.
	seed := modules.Seed{}
	fastrand.Read(seed[:])
	seedStr, err := modules.SeedToString(seed, mnemonics.DictionaryID("english"))
	if err != nil {
		t.Fatal(err)
	}
	password := "password"
	if err := testNode.WalletInitSeedPost(seedStr, password, true); err != nil {
		t.Fatal(err)
	}
	// Change the password again without using the password.
	newPassword := "newpassword"
	if err := testNode.WalletChangePasswordWithSeedPost(seed, newPassword); err != nil {
		t.Fatal(err)
	}
	// Try unlocking the wallet using the old password.
	if err := testNode.WalletUnlockPost(password); err == nil {
		t.Fatal("Shouldn't be able to unlock the wallet with the old password")
	}
	// Unlock the wallet using the new password.
	if err := testNode.WalletUnlockPost(newPassword); err != nil {
		t.Fatal("Failed to unlock wallet")
	}
}

// TestWalletForceInit confirms that the force flag can be set to true even if
// there wasn't a wallet previously created and encrypted
func TestWalletForceInit(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create Wallet without the wallet initialized
	walletParams := node.Wallet(filepath.Join(walletTestDir(t.Name()), "wallet"))
	walletParams.SkipWalletInit = true
	wallet, err := uplotest.NewCleanNode(walletParams)
	if err != nil {
		t.Fatal(err)
	}

	// Force initialize the wallet, this should still worked even if a wallet
	// wasn't initialized and encrypted yet
	wip, err := wallet.WalletInitPost("", true)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that we can unlock the wallet
	err = wallet.WalletUnlockPost(wip.PrimarySeed)
	if err != nil {
		t.Fatal(err)
	}

	// Force initialize a new wallet
	wip, err = wallet.WalletInitPost("", true)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that we can unlock the wallet
	err = wallet.WalletUnlockPost(wip.PrimarySeed)
	if err != nil {
		t.Fatal(err)
	}
}

// TestWalletUnsyncedNewAddress confirms that a wallet can create a new address
// after unlocking it but before being synced with consensus.
func TestWalletUnsyncedNewAddress(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a wallet with a disable async unlock dependency
	testDir := walletTestDir(t.Name())
	walletTemplate := node.Wallet(testDir + "/wallet")
	walletTemplate.WalletDeps = &dependencies.DependencyDisableAsyncUnlock{}
	walletTemplate.CreateMiner = true
	wallet, err := uplotest.NewNode(walletTemplate)
	if err != nil {
		t.Fatal(err)
	}
	_, err = wallet.WalletAddressGet()
	if err != nil {
		t.Fatal(err)
	}
}

// TestWalletVerifyPassword initializes a wallet with a custom password and
// verifies it through the API.
func TestWalletVerifyPassword(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create a new server
	wallet, err := uplotest.NewNode(node.AllModules(walletTestDir(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wallet.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check that verifying a password when one is not set will fail
	wvpg, err := wallet.WalletVerifyPasswordGet("wrong")
	if err != nil {
		t.Error(err)
	}
	if wvpg.Valid {
		t.Error("Password should not be valid")
	}

	// Verify that the Primary Seed will return valid
	wsg, err := wallet.WalletSeedsGet()
	if err != nil {
		t.Error(err)
	}
	wvpg, err = wallet.WalletVerifyPasswordGet(wsg.PrimarySeed)
	if err != nil {
		t.Error(err)
	}
	if !wvpg.Valid {
		t.Error("Primary Seed should be valid")
	}

	// Check primary seed with Seed Endpoint
	seed, err := modules.StringToSeed(wsg.PrimarySeed, "english")
	if err != nil {
		t.Error(err)
	}
	wvpg, err = wallet.WalletVerifyPasswordSeedGet(seed)
	if err != nil {
		t.Error(err)
	}
	if !wvpg.Valid {
		t.Error("Primary Seed should be valid")
	}

	// Reinit the wallet by using a specific password.
	seed = modules.Seed{}
	fastrand.Read(seed[:])
	seedStr, err := modules.SeedToString(seed, mnemonics.DictionaryID("english"))
	if err != nil {
		t.Fatal(err)
	}
	password := "password"
	if err := wallet.WalletInitSeedPost(seedStr, password, true); err != nil {
		t.Fatal(err)
	}

	// Verify that the password is the one used to secure the wallet
	wvpg, err = wallet.WalletVerifyPasswordGet(password)
	if err != nil {
		t.Error(err)
	}
	if !wvpg.Valid {
		t.Error("Password is not valid")
	}

	// Try and verify an incorrect password
	wvpg, err = wallet.WalletVerifyPasswordGet("wrong")
	if err != nil {
		t.Error(err)
	}
	if wvpg.Valid {
		t.Error("Password should not be valid")
	}
}
