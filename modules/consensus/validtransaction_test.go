package consensus

import (
	"testing"

	"github.com/uplo-tech/bolt"
	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/types"
)

// TestTryValidTransactionSet submits a valid transaction set to the
// TryTransactionSet method.
func TestTryValidTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	initialHash := cst.cs.dbConsensusChecksum()

	// Try a valid transaction.
	_, err = cst.wallet.SendUplocoins(types.NewCurrency64(1), types.UnlockHash{})
	if err != nil {
		t.Fatal(err)
	}
	txns := cst.tpool.TransactionList()
	cc, err := cst.cs.TryTransactionSet(txns)
	if err != nil {
		t.Error(err)
	}
	if cst.cs.dbConsensusChecksum() != initialHash {
		t.Error("TryTransactionSet did not resotre order")
	}
	if len(cc.UplocoinOutputDiffs) == 0 {
		t.Error("consensus change is missing diffs after verifying a transction clump")
	}
}

// TestTryInvalidTransactionSet submits an invalid transaction set to the
// TryTransaction method.
func TestTryInvalidTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	initialHash := cst.cs.dbConsensusChecksum()

	// Try a valid transaction followed by an invalid transaction.
	_, err = cst.wallet.SendUplocoins(types.NewCurrency64(1), types.UnlockHash{})
	if err != nil {
		t.Fatal(err)
	}
	txns := cst.tpool.TransactionList()
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{{}},
	}
	txns = append(txns, txn)
	cc, err := cst.cs.TryTransactionSet(txns)
	if err == nil {
		t.Error("bad transaction survived filter")
	}
	if cst.cs.dbConsensusChecksum() != initialHash {
		t.Error("TryTransactionSet did not restore order")
	}
	if len(cc.UplocoinOutputDiffs) != 0 {
		t.Error("consensus change was not empty despite an error being returned")
	}
}

// TestStorageProofBoundaries creates file contracts and submits storage proofs
// for them, probing segment boundaries (first segment, last segment,
// incomplete segment, etc.).
func TestStorageProofBoundaries(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Mine enough blocks to put us beyond the testing hardfork.
	for i := 0; i < 10; i++ {
		_, err = cst.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Try storage proofs on data between 0 bytes and 128 bytes (0 segments and
	// 1 segment). Perform the operation five times because we can't control
	// which segment gets selected - it is randomly decided by the block.
	segmentRange := []int{0, 1, 2, 3, 4, 5, 15, 25, 30, 32, 62, 63, 64, 65, 66, 70, 81, 89, 90, 126, 127, 128, 129}
	for i := 0; i < 3; i++ {
		randData := fastrand.Bytes(140)

		// Create a file contract for all sizes of the data between 0 and 2
		// segments and put them in the transaction pool.
		var fcids []types.FileContractID
		for _, k := range segmentRange {
			// Create the data and the file contract around it.
			truncatedData := randData[:k]
			fc := types.FileContract{
				FileSize:           uint64(k),
				FileMerkleRoot:     crypto.MerkleRoot(truncatedData),
				WindowStart:        cst.cs.dbBlockHeight() + 2,
				WindowEnd:          cst.cs.dbBlockHeight() + 4,
				Payout:             types.NewCurrency64(500), // Too small to be subject to uplofund fee.
				ValidProofOutputs:  []types.UplocoinOutput{{Value: types.NewCurrency64(500)}},
				MissedProofOutputs: []types.UplocoinOutput{{Value: types.NewCurrency64(500)}},
			}

			// Create a transaction around the file contract and add it to the
			// transaction pool.
			b, err := cst.wallet.StartTransaction()
			if err != nil {
				t.Fatal(err)
			}
			err = b.FundUplocoins(types.NewCurrency64(500))
			if err != nil {
				t.Fatal(err)
			}
			b.AddFileContract(fc)
			txnSet, err := b.Sign(true)
			if err != nil {
				t.Fatal(err)
			}
			err = cst.tpool.AcceptTransactionSet(txnSet)
			if err != nil {
				t.Fatal(err)
			}

			// Store the file contract id for later when building the storage
			// proof.
			fcids = append(fcids, txnSet[len(txnSet)-1].FileContractID(0))
		}

		// Mine blocks to get the file contracts into the blockchain and
		// confirming.
		for j := 0; j < 2; j++ {
			_, err = cst.miner.AddBlock()
			if err != nil {
				t.Fatal(err)
			}
		}

		// Create storage proofs for the file contracts and submit the proofs
		// to the blockchain.
		for j, k := range segmentRange {
			// Build the storage proof.
			truncatedData := randData[:k]
			proofIndex, err := cst.cs.StorageProofSegment(fcids[j])
			if err != nil {
				t.Fatal(err)
			}
			base, hashSet := crypto.MerkleProof(truncatedData, proofIndex)
			sp := types.StorageProof{
				ParentID: fcids[j],
				HashSet:  hashSet,
			}
			copy(sp.Segment[:], base)

			if k > 0 {
				// Try submitting an empty storage proof, to make sure that the
				// hardfork code didn't accidentally allow empty storage proofs
				// in situations other than file sizes with 0 bytes.
				badSP := types.StorageProof{ParentID: fcids[j]}
				badTxn := types.Transaction{
					StorageProofs: []types.StorageProof{badSP},
				}
				if sp.Segment == badSP.Segment {
					continue
				}
				err = cst.tpool.AcceptTransactionSet([]types.Transaction{badTxn})
				if err == nil {
					t.Fatal("An empty storage proof got into the transaction pool with non-empty data")
				}
			}

			// Submit the storage proof to the blockchain in a transaction.
			txn := types.Transaction{
				StorageProofs: []types.StorageProof{sp},
			}
			err = cst.tpool.AcceptTransactionSet([]types.Transaction{txn})
			if err != nil {
				t.Fatal(err, "-", j, k)
			}
		}

		// Mine blocks to get the storage proofs on the blockchain.
		for j := 0; j < 2; j++ {
			_, err = cst.miner.AddBlock()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestEmptyStorageProof creates file contracts and submits storage proofs for
// them, probing segment boundaries (first segment, last segment, incomplete
// segment, etc.).
func TestEmptyStorageProof(t *testing.T) {
	if testing.Short() || !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Mine enough blocks to put us beyond the testing hardfork.
	for i := 0; i < 10; i++ {
		_, err = cst.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Try storage proofs on data between 0 bytes and 128 bytes (0 segments and
	// 1 segment). Perform the operation five times because we can't control
	// which segment gets selected - it is randomly decided by the block.
	segmentRange := []int{0, 1, 2, 3, 4, 5, 15, 25, 30, 32, 62, 63, 64, 65, 66, 70, 81, 89, 90, 126, 127, 128, 129}
	for i := 0; i < 3; i++ {
		randData := fastrand.Bytes(140)

		// Create a file contract for all sizes of the data between 0 and 2
		// segments and put them in the transaction pool.
		var fcids []types.FileContractID
		for _, k := range segmentRange {
			// Create the data and the file contract around it.
			truncatedData := randData[:k]
			fc := types.FileContract{
				FileSize:           uint64(k),
				FileMerkleRoot:     crypto.MerkleRoot(truncatedData),
				WindowStart:        cst.cs.dbBlockHeight() + 2,
				WindowEnd:          cst.cs.dbBlockHeight() + 4,
				Payout:             types.NewCurrency64(500), // Too small to be subject to uplofund fee.
				ValidProofOutputs:  []types.UplocoinOutput{{Value: types.NewCurrency64(500)}},
				MissedProofOutputs: []types.UplocoinOutput{{Value: types.NewCurrency64(500)}},
			}

			// Create a transaction around the file contract and add it to the
			// transaction pool.
			b, err := cst.wallet.StartTransaction()
			if err != nil {
				t.Fatal(err)
			}
			err = b.FundUplocoins(types.NewCurrency64(500))
			if err != nil {
				t.Fatal(err)
			}
			b.AddFileContract(fc)
			txnSet, err := b.Sign(true)
			if err != nil {
				t.Fatal(err)
			}
			err = cst.tpool.AcceptTransactionSet(txnSet)
			if err != nil {
				t.Fatal(err)
			}

			// Store the file contract id for later when building the storage
			// proof.
			fcids = append(fcids, txnSet[len(txnSet)-1].FileContractID(0))
		}

		// Mine blocks to get the file contracts into the blockchain and
		// confirming.
		for j := 0; j < 2; j++ {
			_, err = cst.miner.AddBlock()
			if err != nil {
				t.Fatal(err)
			}
		}

		// Create storage proofs for the file contracts and submit the proofs
		// to the blockchain.
		for j, k := range segmentRange {
			// Build the storage proof.
			truncatedData := randData[:k]
			proofIndex, err := cst.cs.StorageProofSegment(fcids[j])
			if err != nil {
				t.Fatal(err)
			}
			base, hashSet := crypto.MerkleProof(truncatedData, proofIndex)
			sp := types.StorageProof{
				ParentID: fcids[j],
				HashSet:  hashSet,
			}
			copy(sp.Segment[:], base)

			// Submit the storage proof to the blockchain in a transaction.
			txn := types.Transaction{
				StorageProofs: []types.StorageProof{sp},
			}
			err = cst.tpool.AcceptTransactionSet([]types.Transaction{txn})
			if err != nil {
				t.Fatal(err, "-", j, k)
			}
		}

		// Mine blocks to get the storage proofs on the blockchain.
		for j := 0; j < 2; j++ {
			_, err = cst.miner.AddBlock()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestValidUplocoins probes the validUplocoins method of the consensus set.
func TestValidUplocoins(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a transaction pointing to a nonexistent Uplocoin output.
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{{}},
	}
	err = cst.cs.db.View(func(tx *bolt.Tx) error {
		err := validUplocoins(tx, txn)
		if !errors.Contains(err, errMissingUplocoinOutput) {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a transaction with invalid unlock conditions.
	scoid, _, err := cst.cs.getArbUplocoinOutput()
	if err != nil {
		t.Fatal(err)
	}
	txn = types.Transaction{
		UplocoinInputs: []types.UplocoinInput{{
			ParentID: scoid,
		}},
	}
	err = cst.cs.db.View(func(tx *bolt.Tx) error {
		err := validUplocoins(tx, txn)
		if !errors.Contains(err, errWrongUnlockConditions) {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a txn with more outputs than inputs.
	txn = types.Transaction{
		UplocoinOutputs: []types.UplocoinOutput{{
			Value: types.NewCurrency64(1),
		}},
	}
	err = cst.cs.db.View(func(tx *bolt.Tx) error {
		err := validUplocoins(tx, txn)
		if !errors.Contains(err, errUplocoinInputOutputMismatch) {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStorageProofSegment probes the storageProofSegment method of the
// consensus set.
func TestStorageProofSegment(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Submit a file contract that is unrecognized.
	_, err = cst.cs.dbStorageProofSegment(types.FileContractID{})
	if !errors.Contains(err, errUnrecognizedFileContractID) {
		t.Error(err)
	}

	// Try to get the segment of an unfinished file contract.
	cst.cs.dbAddFileContract(types.FileContractID{}, types.FileContract{
		Payout:      types.NewCurrency64(1),
		WindowStart: 100000,
	})
	_, err = cst.cs.dbStorageProofSegment(types.FileContractID{})
	if !errors.Contains(err, errUnfinishedFileContract) {
		t.Error(err)
	}
}

// TestValidStorageProofs probes the validStorageProofs method of the consensus
// set.
func TestValidStorageProofs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// COMPATv0.4.0
	//
	// Mine 10 blocks so that the post-hardfork rules are in effect.
	for i := 0; i < 10; i++ {
		block, _ := cst.miner.FindBlock()
		err = cst.cs.AcceptBlock(block)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create a file contract for which a storage proof can be created.
	var fcid types.FileContractID
	fcid[0] = 12
	simFile := fastrand.Bytes(64 * 1024)
	root := crypto.MerkleRoot(simFile)
	fc := types.FileContract{
		FileSize:       64 * 1024,
		FileMerkleRoot: root,
		Payout:         types.NewCurrency64(1),
		WindowStart:    2,
		WindowEnd:      1200,
	}
	cst.cs.dbAddFileContract(fcid, fc)

	// Create a transaction with a storage proof.
	proofIndex, err := cst.cs.dbStorageProofSegment(fcid)
	if err != nil {
		t.Fatal(err)
	}
	base, proofSet := crypto.MerkleProof(simFile, proofIndex)
	txn := types.Transaction{
		StorageProofs: []types.StorageProof{{
			ParentID: fcid,
			HashSet:  proofSet,
		}},
	}
	copy(txn.StorageProofs[0].Segment[:], base)
	err = cst.cs.dbValidStorageProofs(txn)
	if err != nil {
		t.Error(err)
	}

	// Corrupt the proof set.
	proofSet[0][0]++
	txn = types.Transaction{
		StorageProofs: []types.StorageProof{{
			ParentID: fcid,
			HashSet:  proofSet,
		}},
	}
	copy(txn.StorageProofs[0].Segment[:], base)
	err = cst.cs.dbValidStorageProofs(txn)
	if !errors.Contains(err, errInvalidStorageProof) {
		t.Error(err)
	}

	// Try to validate a proof for a file contract that doesn't exist.
	txn.StorageProofs[0].ParentID = types.FileContractID{}
	err = cst.cs.dbValidStorageProofs(txn)
	if !errors.Contains(err, errUnrecognizedFileContractID) {
		t.Error(err)
	}

	// Try a proof set where there is padding on the last segment in the file.
	file := fastrand.Bytes(100)
	root = crypto.MerkleRoot(file)
	fc = types.FileContract{
		FileSize:       100,
		FileMerkleRoot: root,
		Payout:         types.NewCurrency64(1),
		WindowStart:    2,
		WindowEnd:      1200,
	}

	// Find a proofIndex that has the value '1'.
	for {
		fcid[0]++
		cst.cs.dbAddFileContract(fcid, fc)
		proofIndex, err = cst.cs.dbStorageProofSegment(fcid)
		if err != nil {
			t.Fatal(err)
		}
		if proofIndex == 1 {
			break
		}
	}
	base, proofSet = crypto.MerkleProof(file, proofIndex)
	txn = types.Transaction{
		StorageProofs: []types.StorageProof{{
			ParentID: fcid,
			HashSet:  proofSet,
		}},
	}
	copy(txn.StorageProofs[0].Segment[:], base)
	err = cst.cs.dbValidStorageProofs(txn)
	if err != nil {
		t.Fatal(err)
	}
}

// HARDFORK 21,000
//
// TestPreForkValidStorageProofs checks that storage proofs which are invalid
// before the hardfork (but valid afterwards) are still rejected before the
// hardfork).
func TestPreForkValidStorageProofs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Try a proof set where there is padding on the last segment in the file.
	file := fastrand.Bytes(100)
	root := crypto.MerkleRoot(file)
	fc := types.FileContract{
		FileSize:       100,
		FileMerkleRoot: root,
		Payout:         types.NewCurrency64(1),
		WindowStart:    2,
		WindowEnd:      1200,
	}

	// Find a proofIndex that has the value '1'.
	var fcid types.FileContractID
	var proofIndex uint64
	for {
		fcid[0]++
		cst.cs.dbAddFileContract(fcid, fc)
		proofIndex, err = cst.cs.dbStorageProofSegment(fcid)
		if err != nil {
			t.Fatal(err)
		}
		if proofIndex == 1 {
			break
		}
	}
	base, proofSet := crypto.MerkleProof(file, proofIndex)
	txn := types.Transaction{
		StorageProofs: []types.StorageProof{{
			ParentID: fcid,
			HashSet:  proofSet,
		}},
	}
	copy(txn.StorageProofs[0].Segment[:], base)
	err = cst.cs.dbValidStorageProofs(txn)
	if !errors.Contains(err, errInvalidStorageProof) {
		t.Log(cst.cs.dbBlockHeight())
		t.Fatal(err)
	}
}

// TestValidFileContractRevisions probes the validFileContractRevisions method
// of the consensus set.
func TestValidFileContractRevisions(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Grab an address + unlock conditions for the transaction.
	unlockConditions, err := cst.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	// Create a file contract for which a storage proof can be created.
	var fcid types.FileContractID
	fcid[0] = 12
	simFile := fastrand.Bytes(64 * 1024)
	root := crypto.MerkleRoot(simFile)
	fc := types.FileContract{
		FileSize:       64 * 1024,
		FileMerkleRoot: root,
		WindowStart:    102,
		WindowEnd:      1200,
		Payout:         types.NewCurrency64(1),
		UnlockHash:     unlockConditions.UnlockHash(),
		RevisionNumber: 1,
	}
	cst.cs.dbAddFileContract(fcid, fc)

	// Try a working file contract revision.
	txn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{
			{
				ParentID:          fcid,
				UnlockConditions:  unlockConditions,
				NewRevisionNumber: 2,
			},
		},
	}
	err = cst.cs.dbValidFileContractRevisions(txn)
	if err != nil {
		t.Error(err)
	}

	// Try a transaction with an insufficient revision number.
	txn = types.Transaction{
		FileContractRevisions: []types.FileContractRevision{
			{
				ParentID:          fcid,
				UnlockConditions:  unlockConditions,
				NewRevisionNumber: 1,
			},
		},
	}
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errLowRevisionNumber) {
		t.Error(err)
	}
	txn = types.Transaction{
		FileContractRevisions: []types.FileContractRevision{
			{
				ParentID:          fcid,
				UnlockConditions:  unlockConditions,
				NewRevisionNumber: 0,
			},
		},
	}
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errLowRevisionNumber) {
		t.Error(err)
	}

	// Submit a file contract revision pointing to an invalid parent.
	txn.FileContractRevisions[0].ParentID[0]--
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errNilItem) {
		t.Error(err)
	}
	txn.FileContractRevisions[0].ParentID[0]++

	// Submit a file contract revision for a file contract whose window has
	// already opened.
	fc, err = cst.cs.dbGetFileContract(fcid)
	if err != nil {
		t.Fatal(err)
	}
	fc.WindowStart = 0
	cst.cs.dbRemoveFileContract(fcid)
	cst.cs.dbAddFileContract(fcid, fc)
	txn.FileContractRevisions[0].NewRevisionNumber = 3
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errLateRevision) {
		t.Error(err)
	}

	// Submit a file contract revision with incorrect unlock conditions.
	fc.WindowStart = 100
	cst.cs.dbRemoveFileContract(fcid)
	cst.cs.dbAddFileContract(fcid, fc)
	txn.FileContractRevisions[0].UnlockConditions.Timelock++
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errWrongUnlockConditions) {
		t.Error(err)
	}
	txn.FileContractRevisions[0].UnlockConditions.Timelock--

	// Submit file contract revisions for file contracts with altered payouts.
	txn.FileContractRevisions[0].NewValidProofOutputs = []types.UplocoinOutput{{
		Value: types.NewCurrency64(1),
	}}
	txn.FileContractRevisions[0].NewMissedProofOutputs = []types.UplocoinOutput{{
		Value: types.NewCurrency64(1),
	}}
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errAlteredRevisionPayouts) {
		t.Error(err)
	}
	txn.FileContractRevisions[0].NewValidProofOutputs = nil
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errAlteredRevisionPayouts) {
		t.Error(err)
	}
	txn.FileContractRevisions[0].NewValidProofOutputs = []types.UplocoinOutput{{
		Value: types.NewCurrency64(1),
	}}
	txn.FileContractRevisions[0].NewMissedProofOutputs = nil
	err = cst.cs.dbValidFileContractRevisions(txn)
	if !errors.Contains(err, errAlteredRevisionPayouts) {
		t.Error(err)
	}
}

/*
// TestValidUplofunds probes the validUplofunds mthod of the consensus set.
func TestValidUplofunds(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
  	if err := cst.Close(); err != nil {
    	t.Fatal(err)
  	}
	}()

	// Create a transaction pointing to a nonexistent uplofund output.
	txn := types.Transaction{
		UplofundInputs: []types.UplofundInput{{}},
	}
	err = cst.cs.validUplofunds(txn)
	if !errors.Contains(err, ErrMissingUplofundOutput){
		t.Error(err)
	}

	// Create a transaction with invalid unlock conditions.
	var sfoid types.UplofundOutputID
	cst.cs.db.forEachUplofundOutputs(func(mapSfoid types.UplofundOutputID, sfo types.UplofundOutput) {
		sfoid = mapSfoid
		// pointless to do this but I can't think of a better way.
	})
	txn = types.Transaction{
		UplofundInputs: []types.UplofundInput{{
			ParentID:         sfoid,
			UnlockConditions: types.UnlockConditions{Timelock: 12345}, // avoid collisions with existing outputs
		}},
	}
	err = cst.cs.validUplofunds(txn)
	if !errors.Contains(err, ErrWrongUnlockConditions){
		t.Error(err)
	}

	// Create a transaction with more outputs than inputs.
	txn = types.Transaction{
		UplofundOutputs: []types.UplofundOutput{{
			Value: types.NewCurrency64(1),
		}},
	}
	err = cst.cs.validUplofunds(txn)
	if !errors.Contains(err, ErrUplofundInputOutputMismatch){
		t.Error(err)
	}
}

// TestValidTransaction probes the validTransaction method of the consensus
// set.
func TestValidTransaction(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
  		if err := cst.Close(); err != nil {
    		t.Fatal(err)
  		}
	}()

	// Create a transaction that is not standalone valid.
	txn := types.Transaction{
		FileContracts: []types.FileContract{{
			WindowStart: 0,
		}},
	}
	err = cst.cs.validTransaction(txn)
	if err == nil {
		t.Error("transaction is valid")
	}

	// Create a transaction with invalid Uplocoins.
	txn = types.Transaction{
		UplocoinInputs: []types.UplocoinInput{{}},
	}
	err = cst.cs.validTransaction(txn)
	if err == nil {
		t.Error("transaction is valid")
	}

	// Create a transaction with invalid storage proofs.
	txn = types.Transaction{
		StorageProofs: []types.StorageProof{{}},
	}
	err = cst.cs.validTransaction(txn)
	if err == nil {
		t.Error("transaction is valid")
	}

	// Create a transaction with invalid file contract revisions.
	txn = types.Transaction{
		FileContractRevisions: []types.FileContractRevision{{
			NewWindowStart: 5000,
			NewWindowEnd:   5005,
			ParentID:       types.FileContractID{},
		}},
	}
	err = cst.cs.validTransaction(txn)
	if err == nil {
		t.Error("transaction is valid")
	}

	// Create a transaction with invalid uplofunds.
	txn = types.Transaction{
		UplofundInputs: []types.UplofundInput{{}},
	}
	err = cst.cs.validTransaction(txn)
	if err == nil {
		t.Error("transaction is valid")
	}
}
*/

// TestValidArbitraryData probes the validArbitraryData function.
func TestValidArbitraryData(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	validate := func(t types.Transaction, height types.BlockHeight) error {
		return cst.cs.db.View(func(tx *bolt.Tx) error {
			return validArbitraryData(tx, t, height)
		})
	}

	// Check an empty transaction
	if err := validate(types.Transaction{}, types.FoundationHardforkHeight); err != nil {
		t.Error(err)
	}

	// Check data with an invalid prefix -- it should be ignored
	data := encoding.MarshalAll(types.Specifier{'f', 'o', 'o'}, types.FoundationUnlockHashUpdate{})
	if err := validate(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight); err != nil {
		t.Error(err)
	}

	// Check same transaction prior to hardfork -- it should be ignored
	if err := validate(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight-1); err != nil {
		t.Error(err)
	}

	// Check transaction with a valid update, but no input or signature
	data = encoding.MarshalAll(types.SpecifierFoundation, types.FoundationUnlockHashUpdate{})
	if err := validate(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight); err != errUnsignedFoundationUpdate {
		t.Error("expected errUnsignedFoundationUpdate, got", err)
	} else if err := validate(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight-1); err != nil {
		t.Error(err)
	}

	// Check transaction with a valid update
	primaryUC, _ := types.GenerateDeterministicMultisig(2, 3, types.InitialFoundationTestingSalt)
	failsafeUC, _ := types.GenerateDeterministicMultisig(3, 5, types.InitialFoundationFailsafeTestingSalt)
	data = encoding.MarshalAll(types.SpecifierFoundation, types.FoundationUnlockHashUpdate{})
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{{
			ParentID:         types.UplocoinOutputID{1, 2, 3},
			UnlockConditions: primaryUC,
		}},
		ArbitraryData: [][]byte{data},
		TransactionSignatures: []types.TransactionSignature{{
			ParentID:      crypto.Hash{1, 2, 3},
			CoveredFields: types.FullCoveredFields,
		}},
	}
	if err := validate(txn, types.FoundationHardforkHeight); err != nil {
		t.Error(err)
	}

	// Try with the failsafe
	txn.UplocoinInputs[0].UnlockConditions = failsafeUC
	if err := validate(txn, types.FoundationHardforkHeight); err != nil {
		t.Error(err)
	}

	// Try with invalid unlock conditions
	txn.UplocoinInputs[0].UnlockConditions = types.UnlockConditions{}
	if err := validate(txn, types.FoundationHardforkHeight); err != errUnsignedFoundationUpdate {
		t.Error("expected errUnsignedFoundationUpdate, got", err)
	}
}
