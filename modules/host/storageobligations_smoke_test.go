package host

// storageobligations_smoke_test.go performs smoke testing on the the storage
// obligation management. This includes adding valid storage obligations, and
// waiting until they expire, to see if the failure modes are all handled
// correctly.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/uplo-tech/bolt"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/consensus"
	"github.com/uplo-tech/uplo/modules/gateway"
	"github.com/uplo-tech/uplo/modules/transactionpool"
	"github.com/uplo-tech/uplo/modules/wallet"
	"github.com/uplo-tech/uplo/uplotest/dependencies"
	"github.com/uplo-tech/uplo/types"
)

// newTestTPool returns a tpool with custom dependencies for testing
func newTestTPool(name string, deps modules.Dependencies) (func() error, *transactionpool.TransactionPool, error) {
	testdir := build.TempDir(modules.HostDir, name)
	// Create the modules needed.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir))
	if err != nil {
		return nil, nil, err
	}
	cs, errChan := consensus.New(g, false, filepath.Join(testdir, modules.ConsensusDir))
	if err := <-errChan; err != nil {
		return nil, nil, err
	}
	// Create the tpool.
	tp, err := transactionpool.NewCustomTPool(cs, g, filepath.Join(testdir, modules.TransactionPoolDir), deps)
	if err != nil {
		return nil, nil, err
	}
	closefn := func() error {
		return errors.Compose(tp.Close(), cs.Close(), g.Close())
	}
	return closefn, tp, nil
}

// randSector creates a random sector, returning the sector along with the
// Merkle root of the sector.
func randSector() (crypto.Hash, []byte) {
	sectorData := fastrand.Bytes(int(modules.SectorSize))
	sectorRoot := crypto.MerkleRoot(sectorData)
	return sectorRoot, sectorData
}

// newTesterStorageObligation uses the wallet to create and fund a file
// contract that will form the foundation of a storage obligation.
func (ht *hostTester) newTesterStorageObligation() (storageObligation, error) {
	// Create the file contract that will be used in the obligation.
	builder, err := ht.wallet.StartTransaction()
	if err != nil {
		return storageObligation{}, err
	}
	// Fund the file contract with a payout. The payout needs to be big enough
	// that the expected revenue is larger than the fee that the host may end
	// up paying.
	payout := types.UplocoinPrecision.Mul64(10e3)
	err = builder.FundUplocoins(payout)
	if err != nil {
		return storageObligation{}, err
	}
	// Add the file contract that consumes the funds.
	_ = builder.AddFileContract(types.FileContract{
		// Because this file contract needs to be able to accept file contract
		// revisions, the expiration is put more than
		// 'revisionSubmissionBuffer' blocks into the future.
		WindowStart: ht.host.blockHeight + revisionSubmissionBuffer + 2,
		WindowEnd:   ht.host.blockHeight + revisionSubmissionBuffer + modules.DefaultWindowSize + 2,

		Payout: payout,
		ValidProofOutputs: []types.UplocoinOutput{
			{
				Value: types.PostTax(ht.host.blockHeight, payout),
			},
			{
				Value: types.ZeroCurrency,
			},
		},
		MissedProofOutputs: []types.UplocoinOutput{
			{
				Value: types.PostTax(ht.host.blockHeight, payout),
			},
			{
				Value: types.ZeroCurrency,
			},
			{
				Value: types.ZeroCurrency,
			},
		},
		UnlockHash:     (types.UnlockConditions{}).UnlockHash(),
		RevisionNumber: 0,
	})
	// Sign the transaction.
	tSet, err := builder.Sign(true)
	if err != nil {
		return storageObligation{}, err
	}

	// Assemble and return the storage obligation.
	so := storageObligation{
		OriginTransactionSet: tSet,

		h: ht.host,
	}
	return so, nil
}

// TestBlankStorageObligation checks that the host correctly manages a blank
// storage obligation.
func TestBlankStorageObligation(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	ht, err := newHostTester("TestBlankStorageObligation")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ht.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// The number of contracts reported by the host should be zero.
	fm := ht.host.FinancialMetrics()
	if fm.ContractCount != 0 {
		t.Error("host does not start with 0 contracts:", fm.ContractCount)
	}

	// Start by adding a storage obligation to the host. To emulate conditions
	// of a renter creating the first contract, the storage obligation has no
	// data, but does have money.
	so, err := ht.newTesterStorageObligation()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedLockStorageObligation(so.id())
	err = ht.host.managedAddStorageObligation(so)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Storage obligation should not be marked as having the transaction
	// confirmed on the blockchain.
	if so.OriginConfirmed {
		t.Fatal("storage obligation should not yet be marked as confirmed, confirmation is on the way")
	}
	fm = ht.host.FinancialMetrics()
	if fm.ContractCount != 1 {
		t.Error("host should have 1 contract:", fm.ContractCount)
	}

	// Mine a block to confirm the transaction containing the storage
	// obligation.
	_, err = ht.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	err = ht.host.tg.Flush()
	if err != nil {
		t.Fatal(err)
	}
	// Load the storage obligation from the database, see if it updated
	// correctly.
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !so.OriginConfirmed {
		t.Fatal("origin transaction for storage obligation was not confirmed after a block was mined")
	}

	// Mine until the host would be submitting a storage proof. Check that the
	// host has cleared out the storage proof - the consensus code makes it
	// impossible to submit a storage proof for an empty file contract, so the
	// host should fail and give up by deleting the storage obligation.
	for i := types.BlockHeight(0); i <= revisionSubmissionBuffer*2+1; i++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
		err = ht.host.tg.Flush()
		if err != nil {
			t.Fatal(err)
		}
	}
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(100, 100*time.Millisecond, func() error {
		fm = ht.host.FinancialMetrics()
		if fm.ContractCount != 0 {
			return fmt.Errorf("host should have 0 contracts, the contracts were all completed: %v", fm.ContractCount)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPruneStaleStorageObligations checks that the host is able to remove stale
// storage obligations from the database and correct the financial metrics.
// Stale obligations are obligations that are in the host database whose file
// contract never made it on the blockchain. To check if a obligation is stale,
// we check if the obligation is accepted by the transactionpool. If the
// obligation is not in the transactionpool, we check if NegotiationHeight is at
// least maxTxnAge blocks behind the current block. If this is the case, we can
// be certain that the file contract will never make it to the blockchain and
// that it is safe to remove the obligation from the database.
func TestPruneStaleStorageObligations(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	ht, err := newHostTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := ht.Close()
		if err != nil {
			t.Error(err)
		}
	}()

	// The number of contracts and locked storage collateral reported
	// by the host should be zero.
	fm := ht.host.FinancialMetrics()
	if fm.ContractCount != 0 {
		t.Error("host does not start with a ContractCount of 0:", fm.ContractCount)
	}
	if !fm.LockedStorageCollateral.IsZero() {
		t.Error("host does not start with 0 LockedStorageCollateral:", fm.LockedStorageCollateral)
	}
	if !fm.PotentialContractCompensation.IsZero() {
		t.Error("host does not start with 0 PotentialContractCompensation:", fm.PotentialContractCompensation.HumanString())
	}

	// During counting of obligations in the host database, variables i, j and k are used to count
	// the number of 'total', 'good' and 'stale' storage obligations.
	var i, j, k int = 0, 0, 0

	// The following error is returned whenever we don't find the expected number
	// of obligations in the host database.
	errCountErr := errors.New("Host database does not contain the expected obligations.")

	// Define values for ContractCost (1SC) and LockedCollateral (1KS), create
	// 3 new storage obligations and add them to the host
	var contractCost types.Currency = types.NewCurrency64(1).Mul(types.UplocoinPrecision)
	var lockedCollateral types.Currency = types.NewCurrency64(1e3).Mul(types.UplocoinPrecision)
	for i := 0; i < 3; i++ {
		so, err := ht.newTesterStorageObligation()
		if err != nil {
			t.Fatal(err)
		}
		so.ContractCost = contractCost
		so.LockedCollateral = lockedCollateral
		ht.host.managedLockStorageObligation(so.id())
		err = ht.host.managedAddStorageObligation(so)
		if err != nil {
			t.Fatal(err)
		}
		ht.host.managedUnlockStorageObligation(so.id())
		_, err = ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
		err = ht.host.tg.Flush()
		if err != nil {
			t.Fatal(err)
		}
	}
	// The number of contracts reported by the host should be 3 and
	// all financial metrics should be updated accordingly.
	fm = ht.host.FinancialMetrics()
	if fm.ContractCount != 3 {
		t.Error("host does not have 3 contracts:", fm.ContractCount)
	}
	if fm.LockedStorageCollateral.Cmp(lockedCollateral.Mul64(3)) != 0 {
		t.Error("LockedStorageCollateral should be 3KS:", fm.LockedStorageCollateral.HumanString())
	}
	if fm.PotentialContractCompensation.Cmp(contractCost.Mul64(3)) != 0 {
		t.Error("PotentialContractCompensation should be 3SC:", fm.PotentialContractCompensation.HumanString())
	}

	// Replace transaction pool with one that has custom dependency.
	tp := ht.host.tpool
	closeNewTPTFn, newTPool, err := newTestTPool(filepath.Join(t.Name(), "newtpool"), &dependencies.DependencyDoNotAcceptTxnSet{})
	if err != nil {
		t.Fatal(err)
	}
	ht.host.tpool = newTPool

	// Try to add 2 more storage obligations to host. This operation should fail, the file contracts
	// of these storage obligations will not make it on the blockchain.
	for i := 0; i < 2; i++ {
		so, err := ht.newTesterStorageObligation()
		if err != nil {
			t.Fatal(err)
		}
		so.ContractCost = contractCost
		so.LockedCollateral = lockedCollateral
		ht.host.managedLockStorageObligation(so.id())
		err = ht.host.managedAddStorageObligation(so)
		if !errors.Contains(err, transactionpool.ErrTxnSetNotAccepted) {
			t.Error("Wrong error:", err)
		}
		ht.host.managedUnlockStorageObligation(so.id())
		_, err = ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
		err = ht.host.tg.Flush()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Due to a bug in managedAddStorageObligation, the contract count equals 5
	// and all lock storage collateral. In the host database we should find 5
	// storage obligations.
	fm = ht.host.FinancialMetrics()
	if fm.ContractCount != 5 {
		t.Error("Host should now have 5 contracts:", fm.ContractCount)
	}
	if fm.LockedStorageCollateral.Cmp(lockedCollateral.Mul64(5)) != 0 {
		t.Error("LockedStorageCollateral should be 5KS:", fm.LockedStorageCollateral.HumanString())
	}
	// Check that the host reports the potential contract compensation for the 5 obligations.
	if fm.PotentialContractCompensation.Cmp(contractCost.Mul64(5)) != 0 {
		t.Error("PotentialContractCompensation should be 5SC:", fm.PotentialContractCompensation.HumanString())
	}
	// Reset counter and count total number of obligations in the database.
	i = 0
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketStorageObligations).Cursor()
		for key, v := cursor.First(); key != nil; key, v = cursor.Next() {
			var so storageObligation
			err := json.Unmarshal(v, &so)
			if err != nil {
				t.Fatal(err)
			}
			i++
		}
		if i != 5 {
			t.Logf("There should be a total of 5 obligations in the database. Found %v.", i)
			return errCountErr
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Reset the transaction pool
	ht.host.tpool = tp
	err = closeNewTPTFn()
	if err != nil {
		t.Error(err)
	}

	// Mine enough blocks so that all active storage obligations succeed and we
	// know for sure the other obligations are stale, i.e. not in the transaction pool
	// and with a NegotiationHeight, RespendTimeout blocks behind the currrent block.
	endblock := ht.host.blockHeight + revisionSubmissionBuffer + modules.DefaultWindowSize + 2 + wallet.RespendTimeout + 1
	for cb := ht.host.blockHeight; cb <= endblock; cb++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
		err = ht.host.tg.Flush()
		if err != nil {
			t.Fatal(err)
		}
	}
	fm = ht.host.FinancialMetrics()
	// Check that the host reports the contract compensation for the 3 succeeded obligations.
	if fm.ContractCompensation.Cmp(contractCost.Mul64(3)) != 0 {
		t.Error("ContractCompensation should be 3SC:", fm.ContractCompensation.HumanString())
	}
	// 3 Out of 5 obligations succeeded. Since 2 obligations are stale, the contract
	// count will equal 2 and not 0. They both lock storage collateral.
	if fm.ContractCount != 2 {
		t.Error("Host should report 2 active contracts:", fm.ContractCount)
	}
	if fm.LockedStorageCollateral.Cmp(lockedCollateral.Mul64(2)) != 0 {
		t.Error("LockedStorageCollateral should be 2KS:", fm.LockedStorageCollateral.HumanString())
	}
	if fm.PotentialContractCompensation.Cmp(contractCost.Mul64(2)) != 0 {
		t.Error("PotentialContractCompensation should be 2SC:", fm.PotentialContractCompensation.HumanString())
	}
	// Proof that the host has stale storage obligations in the database.
	i = 0
	j = 0
	k = 0
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketStorageObligations).Cursor()
		for key, v := cursor.First(); key != nil; key, v = cursor.Next() {
			var so storageObligation
			err := json.Unmarshal(v, &so)
			if err != nil {
				t.Fatal(err)
			}

			i++
			if so.ObligationStatus == obligationSucceeded {
				j++
			}
			if so.ObligationStatus == obligationUnresolved {
				// Check if the obligation transaction is confirmed
				final := len(so.OriginTransactionSet) - 1
				txid := so.OriginTransactionSet[final].ID()
				found, err := ht.host.tpool.TransactionConfirmed(txid)
				if err != nil {
					t.Fatal(err)
				}
				if found {
					t.Log("Found unresolved obligation that was accepted by the transaction pool.")
					return errCountErr
				}
				// Transaction was not found on the transaction pool. Double check if
				// this obligation is in the process of being accepted.
				if so.NegotiationHeight+wallet.RespendTimeout < ht.host.blockHeight {
					// This obligation was created too far in the past and it is safe
					// to assume this is a stale obligation.
					k++
				}
			}
		}
		if i != (j + k) {
			t.Logf("There should be in total 5 obligations in the database. Found %v.", i)
			return errCountErr
		}
		if j != 3 {
			t.Logf("There should be 3 succeeded obligations in the database. Found %v.", j)
			return errCountErr
		}
		if k != 2 {
			t.Logf("There should be 2 unresolved obligations in the database. Found %v.", k)
			return errCountErr
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// These 2 stale contracts will forever lock storage collateral. Use the
	// PruneStaleStorgageObligations method to remove them.
	err = ht.host.PruneStaleStorageObligations()
	if err != nil {
		t.Error(err)
	}

	// Check the financials.
	fm = ht.host.FinancialMetrics()
	if fm.ContractCount != 0 {
		t.Error("Host should report 0 active contracts:", fm.ContractCount)
	}
	if !fm.LockedStorageCollateral.IsZero() {
		t.Error("Locked collateral should be 0:", fm.LockedStorageCollateral.HumanString())
	}
	// Check that the host still reports the contract compensation for the 3 succeeded obligations.
	if fm.ContractCompensation.Cmp(contractCost.Mul64(3)) != 0 {
		t.Error("ContractCompensation should be 3SC:", fm.ContractCompensation.HumanString())
	}
	// Finally we check the database so see if all stale obligations were successfully removed.
	// We also need to check if the ones that succeeded are still in the database and that the
	// total number of obligations equals the number of obligations that succeeded.
	i = 0
	j = 0
	k = 0
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketStorageObligations).Cursor()
		for key, v := cursor.First(); key != nil; key, v = cursor.Next() {
			var so storageObligation
			err := json.Unmarshal(v, &so)
			if err != nil {
				t.Fatal(err)
			}

			i++
			if so.ObligationStatus == obligationSucceeded {
				j++
			}
			if so.ObligationStatus == obligationUnresolved {
				// Check if the obligation transaction is confirmed
				final := len(so.OriginTransactionSet) - 1
				txid := so.OriginTransactionSet[final].ID()
				found, err := ht.host.tpool.TransactionConfirmed(txid)
				if err != nil {
					t.Fatal(err)
				}
				if found {
					t.Log("Found unresolved obligation that was accepted by the transaction pool.")
					return errCountErr
				}
				// Transaction was not found on the transaction pool. Double check if
				// this obligation is in the process of being accepted.
				if so.NegotiationHeight+wallet.RespendTimeout < ht.host.blockHeight {
					// This obligation was created too far in the past and it is safe
					// to assume this is a stale obligation.
					k++
				}
			}
		}
		if i != (j + k) {
			t.Logf("There should be a total of 3 obligations in the database. Found %v.", i)
			return errCountErr
		}
		if j != 3 {
			t.Logf("There should be 3 succeeded obligations in the database. Found %v.", j)
			return errCountErr
		}
		if k != 0 {
			t.Logf("There should not be any stale obligations in the database. Found %v.", k)
			return errCountErr
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

// TestSingleSectorObligationStack checks that the host correctly manages a
// storage obligation with a single sector, the revision is created the same
// block as the file contract.
func TestSingleSectorStorageObligationStack(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	ht, err := newHostTester("TestSingleSectorStorageObligationStack")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ht.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Start by adding a storage obligation to the host. To emulate conditions
	// of a renter creating the first contract, the storage obligation has no
	// data, but does have money.
	so, err := ht.newTesterStorageObligation()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedLockStorageObligation(so.id())
	err = ht.host.managedAddStorageObligation(so)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Storage obligation should not be marked as having the transaction
	// confirmed on the blockchain.
	if so.OriginConfirmed {
		t.Fatal("storage obligation should not yet be marked as confirmed, confirmation is on the way")
	}

	// Add a file contract revision, moving over a small amount of money to pay
	// for the file contract.
	sectorRoot, sectorData := randSector()
	so.SectorRoots = []crypto.Hash{sectorRoot}
	sectorCost := types.UplocoinPrecision.Mul64(550)
	so.PotentialStorageRevenue = so.PotentialStorageRevenue.Add(sectorCost)
	ht.host.mu.Lock()
	ht.host.financialMetrics.PotentialStorageRevenue = ht.host.financialMetrics.PotentialStorageRevenue.Add(sectorCost)
	ht.host.mu.Unlock()
	validPayouts, missedPayouts := so.payouts()
	validPayouts[0].Value = validPayouts[0].Value.Sub(sectorCost)
	validPayouts[1].Value = validPayouts[1].Value.Add(sectorCost)
	missedPayouts[0].Value = missedPayouts[0].Value.Sub(sectorCost)
	missedPayouts[1].Value = missedPayouts[1].Value.Add(sectorCost)
	revisionSet := []types.Transaction{{
		FileContractRevisions: []types.FileContractRevision{{
			ParentID:          so.id(),
			UnlockConditions:  types.UnlockConditions{},
			NewRevisionNumber: 2,

			NewFileSize:           uint64(len(sectorData)),
			NewFileMerkleRoot:     sectorRoot,
			NewWindowStart:        so.expiration(),
			NewWindowEnd:          so.proofDeadline(),
			NewValidProofOutputs:  validPayouts,
			NewMissedProofOutputs: missedPayouts,
			NewUnlockHash:         types.UnlockConditions{}.UnlockHash(),
		}},
	}}
	so.RevisionTransactionSet = revisionSet
	ht.host.managedLockStorageObligation(so.id())
	err = ht.host.managedModifyStorageObligation(so, nil, map[crypto.Hash][]byte{sectorRoot: sectorData})
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Submit the revision set to the transaction pool.
	err = ht.tpool.AcceptTransactionSet(revisionSet)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a block to confirm the transactions containing the file contract
	// and the file contract revision.
	_, err = ht.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	// Load the storage obligation from the database, see if it updated
	// correctly.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		ht.host.mu.Lock()
		err := ht.host.db.View(func(tx *bolt.Tx) error {
			so, err = ht.host.getStorageObligation(tx, so.id())
			if err != nil {
				return err
			}
			return nil
		})
		ht.host.mu.Unlock()
		if err != nil {
			return err
		}
		if !so.OriginConfirmed {
			return errors.New("origin transaction for storage obligation was not confirmed after a block was mined")
		}
		if !so.RevisionConfirmed {
			return errors.New("revision transaction for storage obligation was not confirmed after a block was mined")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine until the host submits a storage proof.
	ht.host.mu.Lock()
	bh := ht.host.blockHeight
	ht.host.mu.Unlock()
	for i := bh; i < so.expiration()+resubmissionTimeout; i++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Need Sleep for online CI, otherwise threadedHandleActionItem thread group
	// is not added in time and Flush() does not block
	time.Sleep(time.Second)

	// Flush the host - flush will block until the host has submitted the
	// storage proof to the transaction pool.
	err = ht.host.tg.Flush()
	if err != nil {
		t.Fatal(err)
	}
	// Mine another block, to get the storage proof from the transaction pool
	// into the blockchain.
	_, err = ht.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Grab the storage proof and inspect the contents.
	ht.host.mu.Lock()
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		return nil
	})
	ht.host.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !so.OriginConfirmed {
		t.Fatal("origin transaction for storage obligation was not confirmed after a block was mined")
	}
	if !so.RevisionConfirmed {
		t.Fatal("revision transaction for storage obligation was not confirmed after a block was mined")
	}
	if !so.ProofConfirmed {
		t.Fatal("storage obligation is not saying that the storage proof was confirmed on the blockchain")
	}

	// Mine blocks until the storage proof has enough confirmations that the
	// host will finalize the obligation.
	for i := 0; i <= int(modules.DefaultWindowSize); i++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	ht.host.mu.Lock()
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		if so.SectorRoots != nil {
			t.Error("sector roots were not cleared when the host finalized the obligation")
		}
		if so.ObligationStatus != obligationSucceeded {
			t.Error("obligation is not being reported as successful:", so.ObligationStatus)
		}
		return nil
	})
	ht.host.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.mu.Lock()
	storageRevenue := ht.host.financialMetrics.StorageRevenue
	ht.host.mu.Unlock()
	if !storageRevenue.Equals(sectorCost) {
		t.Fatal("the host should be reporting revenue after a successful storage proof")
	}
}

// TestMultiSectorObligationStack checks that the host correctly manages a
// storage obligation with a single sector, the revision is created the same
// block as the file contract.
//
// Unlike the SingleSector test, the multi sector test attempts to spread file
// contract revisions over multiple blocks.
func TestMultiSectorStorageObligationStack(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	ht, err := newHostTester("TestMultiSectorStorageObligationStack")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ht.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Start by adding a storage obligation to the host. To emulate conditions
	// of a renter creating the first contract, the storage obligation has no
	// data, but does have money.
	so, err := ht.newTesterStorageObligation()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedLockStorageObligation(so.id())
	err = ht.host.managedAddStorageObligation(so)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Storage obligation should not be marked as having the transaction
	// confirmed on the blockchain.
	if so.OriginConfirmed {
		t.Fatal("storage obligation should not yet be marked as confirmed, confirmation is on the way")
	}
	// Deviation from SingleSector test - mine a block here to confirm the
	// storage obligation before a file contract revision is created.
	_, err = ht.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	// Load the storage obligation from the database, see if it updated
	// correctly.
	ht.host.mu.Lock()
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		return nil
	})
	ht.host.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !so.OriginConfirmed {
		t.Fatal("origin transaction for storage obligation was not confirmed after a block was mined")
	}

	// Add a file contract revision, moving over a small amount of money to pay
	// for the file contract.
	sectorRoot, sectorData := randSector()
	so.SectorRoots = []crypto.Hash{sectorRoot}
	sectorCost := types.UplocoinPrecision.Mul64(550)
	so.PotentialStorageRevenue = so.PotentialStorageRevenue.Add(sectorCost)
	ht.host.mu.Lock()
	ht.host.financialMetrics.PotentialStorageRevenue = ht.host.financialMetrics.PotentialStorageRevenue.Add(sectorCost)
	ht.host.mu.Unlock()
	validPayouts, missedPayouts := so.payouts()
	validPayouts[0].Value = validPayouts[0].Value.Sub(sectorCost)
	validPayouts[1].Value = validPayouts[1].Value.Add(sectorCost)
	missedPayouts[0].Value = missedPayouts[0].Value.Sub(sectorCost)
	missedPayouts[1].Value = missedPayouts[1].Value.Add(sectorCost)
	revisionSet := []types.Transaction{{
		FileContractRevisions: []types.FileContractRevision{{
			ParentID:          so.id(),
			UnlockConditions:  types.UnlockConditions{},
			NewRevisionNumber: 2,

			NewFileSize:           uint64(len(sectorData)),
			NewFileMerkleRoot:     sectorRoot,
			NewWindowStart:        so.expiration(),
			NewWindowEnd:          so.proofDeadline(),
			NewValidProofOutputs:  validPayouts,
			NewMissedProofOutputs: missedPayouts,
			NewUnlockHash:         types.UnlockConditions{}.UnlockHash(),
		}},
	}}
	so.RevisionTransactionSet = revisionSet
	ht.host.managedLockStorageObligation(so.id())

	err = ht.host.managedModifyStorageObligation(so, nil, map[crypto.Hash][]byte{sectorRoot: sectorData})
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Submit the revision set to the transaction pool.
	err = ht.tpool.AcceptTransactionSet(revisionSet)
	if err != nil {
		t.Fatal(err)
	}

	// Create a second file contract revision, which is going to be submitted
	// to the transaction pool after the first revision. Though, in practice
	// this should never happen, we want to check that the transaction pool is
	// correctly handling multiple file contract revisions being submitted in
	// the same block cycle. This test will additionally tell us whether or not
	// the host can correctly handle building storage proofs for files with
	// multiple sectors.
	sectorRoot2, sectorData2 := randSector()
	so.SectorRoots = []crypto.Hash{sectorRoot, sectorRoot2}
	sectorCost2 := types.UplocoinPrecision.Mul64(650)
	so.PotentialStorageRevenue = so.PotentialStorageRevenue.Add(sectorCost2)
	ht.host.mu.Lock()
	ht.host.financialMetrics.PotentialStorageRevenue = ht.host.financialMetrics.PotentialStorageRevenue.Add(sectorCost2)
	ht.host.mu.Unlock()
	validPayouts, missedPayouts = so.payouts()
	validPayouts[0].Value = validPayouts[0].Value.Sub(sectorCost2)
	validPayouts[1].Value = validPayouts[1].Value.Add(sectorCost2)
	missedPayouts[0].Value = missedPayouts[0].Value.Sub(sectorCost2)
	missedPayouts[1].Value = missedPayouts[1].Value.Add(sectorCost2)
	combinedSectors := append(sectorData, sectorData2...)
	combinedRoot := crypto.MerkleRoot(combinedSectors)
	revisionSet2 := []types.Transaction{{
		FileContractRevisions: []types.FileContractRevision{{
			ParentID:          so.id(),
			UnlockConditions:  types.UnlockConditions{},
			NewRevisionNumber: 3,

			NewFileSize:           uint64(len(sectorData) + len(sectorData2)),
			NewFileMerkleRoot:     combinedRoot,
			NewWindowStart:        so.expiration(),
			NewWindowEnd:          so.proofDeadline(),
			NewValidProofOutputs:  validPayouts,
			NewMissedProofOutputs: missedPayouts,
			NewUnlockHash:         types.UnlockConditions{}.UnlockHash(),
		}},
	}}
	ht.host.managedLockStorageObligation(so.id())
	err = ht.host.managedModifyStorageObligation(so, nil, map[crypto.Hash][]byte{sectorRoot2: sectorData2})
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Submit the revision set to the transaction pool.
	err = ht.tpool.AcceptTransactionSet(revisionSet2)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a block to confirm the transactions containing the file contract
	// and the file contract revision.
	_, err = ht.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	// Load the storage obligation from the database, see if it updated
	// correctly.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		ht.host.mu.Lock()
		err := ht.host.db.View(func(tx *bolt.Tx) error {
			so, err = ht.host.getStorageObligation(tx, so.id())
			if err != nil {
				return err
			}
			return nil
		})
		ht.host.mu.Unlock()
		if err != nil {
			return err
		}
		if !so.OriginConfirmed {
			return errors.New("origin transaction for storage obligation was not confirmed after a block was mined")
		}
		if !so.RevisionConfirmed {
			return errors.New("revision transaction for storage obligation was not confirmed after a block was mined")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine until the host submits a storage proof.
	ht.host.mu.Lock()
	bh := ht.host.blockHeight
	ht.host.mu.Unlock()
	for i := bh; i < so.expiration()+resubmissionTimeout; i++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Need Sleep for online CI, otherwise threadedHandleActionItem thread group
	// is not added in time and Flush() does not block
	time.Sleep(time.Second)

	// Flush the host - flush will block until the host has submitted the
	// storage proof to the transaction pool.
	err = ht.host.tg.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Mine another block, to get the storage proof from the transaction pool
	// into the blockchain.
	_, err = ht.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.mu.Lock()
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		return nil
	})
	ht.host.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !so.OriginConfirmed {
		t.Fatal("origin transaction for storage obligation was not confirmed after a block was mined")
	}
	if !so.RevisionConfirmed {
		t.Fatal("revision transaction for storage obligation was not confirmed after a block was mined")
	}
	if !so.ProofConfirmed {
		t.Fatal("storage obligation is not saying that the storage proof was confirmed on the blockchain")
	}

	// Mine blocks until the storage proof has enough confirmations that the
	// host will delete the file entirely.
	for i := 0; i <= int(modules.DefaultWindowSize); i++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	ht.host.mu.Lock()
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		if so.SectorRoots != nil {
			t.Error("sector roots were not cleared out when the storage proof was finalized")
		}
		if so.ObligationStatus != obligationSucceeded {
			t.Error("storage obligation was not reported as a success")
		}
		return nil
	})
	ht.host.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !ht.host.financialMetrics.StorageRevenue.Equals(sectorCost.Add(sectorCost2)) {
		t.Fatal("the host should be reporting revenue after a successful storage proof")
	}
}

// TestAutoRevisionSubmission checks that the host correctly submits a file
// contract revision to the consensus set.
func TestAutoRevisionSubmission(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	ht, err := newHostTester("TestAutoRevisionSubmission")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ht.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Start by adding a storage obligation to the host. To emulate conditions
	// of a renter creating the first contract, the storage obligation has no
	// data, but does have money.
	so, err := ht.newTesterStorageObligation()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedLockStorageObligation(so.id())
	err = ht.host.managedAddStorageObligation(so)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	// Storage obligation should not be marked as having the transaction
	// confirmed on the blockchain.
	if so.OriginConfirmed {
		t.Fatal("storage obligation should not yet be marked as confirmed, confirmation is on the way")
	}

	// Add a file contract revision, moving over a small amount of money to pay
	// for the file contract.
	sectorRoot, sectorData := randSector()
	so.SectorRoots = []crypto.Hash{sectorRoot}
	sectorCost := types.UplocoinPrecision.Mul64(550)
	so.PotentialStorageRevenue = so.PotentialStorageRevenue.Add(sectorCost)
	ht.host.financialMetrics.PotentialStorageRevenue = ht.host.financialMetrics.PotentialStorageRevenue.Add(sectorCost)
	validPayouts, missedPayouts := so.payouts()
	validPayouts[0].Value = validPayouts[0].Value.Sub(sectorCost)
	validPayouts[1].Value = validPayouts[1].Value.Add(sectorCost)
	missedPayouts[0].Value = missedPayouts[0].Value.Sub(sectorCost)
	missedPayouts[1].Value = missedPayouts[1].Value.Add(sectorCost)
	revisionSet := []types.Transaction{{
		FileContractRevisions: []types.FileContractRevision{{
			ParentID:          so.id(),
			UnlockConditions:  types.UnlockConditions{},
			NewRevisionNumber: 1,

			NewFileSize:           uint64(len(sectorData)),
			NewFileMerkleRoot:     sectorRoot,
			NewWindowStart:        so.expiration(),
			NewWindowEnd:          so.proofDeadline(),
			NewValidProofOutputs:  validPayouts,
			NewMissedProofOutputs: missedPayouts,
			NewUnlockHash:         types.UnlockConditions{}.UnlockHash(),
		}},
	}}
	so.RevisionTransactionSet = revisionSet
	ht.host.managedLockStorageObligation(so.id())

	err = ht.host.managedModifyStorageObligation(so, nil, map[crypto.Hash][]byte{sectorRoot: sectorData})
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so.id())
	err = ht.host.tg.Flush()
	if err != nil {
		t.Fatal(err)
	}
	// Unlike the other tests, this test does not submit the file contract
	// revision to the transaction pool for the host, the host is expected to
	// do it automatically.
	count := 0
	err = build.Retry(500, 100*time.Millisecond, func() error {
		// Mine another block every 10 iterations, to get the storage proof from
		// the transaction pool into the blockchain.
		if count%10 == 0 {
			_, err = ht.miner.AddBlock()
			if err != nil {
				t.Fatal(err)
			}
			err = ht.host.tg.Flush()
			if err != nil {
				t.Fatal(err)
			}
		}
		count++
		err = ht.host.db.View(func(tx *bolt.Tx) error {
			so, err = ht.host.getStorageObligation(tx, so.id())
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !so.OriginConfirmed {
			return errors.New("origin transaction for storage obligation was not confirmed after blocks were mined")
		}
		if !so.RevisionConfirmed {
			return errors.New("revision transaction for storage obligation was not confirmed after blocks were mined")
		}
		if !so.ProofConfirmed {
			return errors.New("storage obligation is not saying that the storage proof was confirmed on the blockchain")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine blocks until the storage proof has enough confirmations that the
	// host will delete the file entirely.
	for i := 0; i <= int(modules.DefaultWindowSize); i++ {
		_, err := ht.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
		err = ht.host.tg.Flush()
		if err != nil {
			t.Fatal(err)
		}
	}
	err = ht.host.db.View(func(tx *bolt.Tx) error {
		so, err = ht.host.getStorageObligation(tx, so.id())
		if err != nil {
			return err
		}
		if so.SectorRoots != nil {
			t.Error("sector roots were not cleared out when the storage proof was finalized")
		}
		if so.ObligationStatus != obligationSucceeded {
			t.Error("storage obligation was not reported as a success")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ht.host.financialMetrics.StorageRevenue.Equals(sectorCost) {
		t.Fatal("the host should be reporting revenue after a successful storage proof")
	}
}

// TestLargeContractBlock tests that a storage obligation can still be rapidly
// updated while another storage obligation modification is blocked by the
// largeContractDelay.
func TestLargeContractBlock(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	ht, err := newHostTester("TestLargeContractBlock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ht.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create 2 storage obligations for the test and add them to the host.
	so1, err := ht.newTesterStorageObligation()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedLockStorageObligation(so1.id())
	err = ht.host.managedAddStorageObligation(so1)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so1.id())
	so2, err := ht.newTesterStorageObligation()
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedLockStorageObligation(so2.id())
	err = ht.host.managedAddStorageObligation(so2)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so2.id())

	// Add a file contract revision, increasing the filesize of the obligation
	// beyong the largeContractSize.
	validPayouts, missedPayouts := so1.payouts()
	validPayouts[0].Value = validPayouts[0].Value.Sub(types.ZeroCurrency)
	validPayouts[1].Value = validPayouts[1].Value.Add(types.ZeroCurrency)
	missedPayouts[0].Value = missedPayouts[0].Value.Sub(types.ZeroCurrency)
	missedPayouts[1].Value = missedPayouts[1].Value.Add(types.ZeroCurrency)
	revisionSet := []types.Transaction{{
		FileContractRevisions: []types.FileContractRevision{{
			ParentID:          so1.id(),
			UnlockConditions:  types.UnlockConditions{},
			NewRevisionNumber: 1,

			NewFileSize:           uint64(largeContractSize),
			NewFileMerkleRoot:     crypto.Hash{},
			NewWindowStart:        so1.expiration(),
			NewWindowEnd:          so1.proofDeadline(),
			NewValidProofOutputs:  validPayouts,
			NewMissedProofOutputs: missedPayouts,
			NewUnlockHash:         types.UnlockConditions{}.UnlockHash(),
		}},
	}}
	so1.RevisionTransactionSet = revisionSet
	ht.host.managedLockStorageObligation(so1.id())
	err = ht.host.managedModifyStorageObligation(so1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ht.host.managedUnlockStorageObligation(so1.id())
	err = ht.host.tg.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Lock so1 for the remaining test. This shouldn't block operations on so2.
	ht.host.managedLockStorageObligation(so1.id())
	defer ht.host.managedUnlockStorageObligation(so1.id())

	done := make(chan struct{})
	go func() {
		// Modify so1. This should at least take
		// largeContractUpdateDelay seconds.
		defer close(done)
		start := time.Now()
		err := ht.host.managedModifyStorageObligation(so1, nil, nil)
		delay := time.Since(start)
		if err != nil {
			t.Error(err)
		}
		if delay < largeContractUpdateDelay {
			t.Errorf("delay should be at least %v but was %v", largeContractUpdateDelay, delay)
		}
	}()
	// Lock so2 and modify it repeatedly. This simulates uploads to a different
	// contract. No modification sho
	numMods := 0
LOOP:
	for {
		select {
		case <-done:
			break LOOP
		default:
		}
		numMods++
		ht.host.managedLockStorageObligation(so2.id())
		start := time.Now()
		err := ht.host.managedModifyStorageObligation(so2, nil, nil)
		delay := time.Since(start)
		ht.host.managedUnlockStorageObligation(so2.id())
		if err != nil {
			t.Fatal(err)
		}
		if delay >= largeContractUpdateDelay {
			t.Fatal("delay was longer than largeContractDelay which means so2 got blocked by so1", delay, largeContractUpdateDelay)
		}
	}
	if numMods == 0 {
		t.Fatal("expected at least one modification to happen to so2")
	}
	t.Logf("updated so2 %v times", numMods)
}
