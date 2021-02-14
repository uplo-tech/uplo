package consensus

import (
	"testing"

	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/bolt"
	"github.com/uplo-tech/encoding"
)

/*
// TestApplyUplocoinInputs probes the applyUplocoinInputs method of the consensus
// set.
func TestApplyUplocoinInputs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a consensus set and get it to 3 Uplocoin outputs. The consensus
	// set starts with 2 Uplocoin outputs, mining a block will add another.
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cst.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	b, _ := cst.miner.FindBlock()
	err = cst.cs.AcceptBlock(b)
	if err != nil {
		t.Fatal(err)
	}

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Fetch the output id's of each Uplocoin output in the consensus set.
	var ids []types.UplocoinOutputID
	cst.cs.db.forEachUplocoinOutputs(func(id types.UplocoinOutputID, sco types.UplocoinOutput) {
		ids = append(ids, id)
	})

	// Apply a transaction with a single Uplocoin input.
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{
			{ParentID: ids[0]},
		},
	}
	cst.cs.applyUplocoinInputs(pb, txn)
	exists := cst.cs.db.inUplocoinOutputs(ids[0])
	if exists {
		t.Error("Failed to conusme a Uplocoin output")
	}
	if cst.cs.db.lenUplocoinOutputs() != 2 {
		t.Error("Uplocoin outputs not correctly updated")
	}
	if len(pb.UplocoinOutputDiffs) != 1 {
		t.Error("block node was not updated for single transaction")
	}
	if pb.UplocoinOutputDiffs[0].Direction != modules.DiffRevert {
		t.Error("wrong diff direction applied when consuming a Uplocoin output")
	}
	if pb.UplocoinOutputDiffs[0].ID != ids[0] {
		t.Error("wrong id used when consuming a Uplocoin output")
	}

	// Apply a transaction with two Uplocoin inputs.
	txn = types.Transaction{
		UplocoinInputs: []types.UplocoinInput{
			{ParentID: ids[1]},
			{ParentID: ids[2]},
		},
	}
	cst.cs.applyUplocoinInputs(pb, txn)
	if cst.cs.db.lenUplocoinOutputs() != 0 {
		t.Error("failed to consume all Uplocoin outputs in the consensus set")
	}
	if len(pb.UplocoinOutputDiffs) != 3 {
		t.Error("processed block was not updated for single transaction")
	}
}

// TestMisuseApplyUplocoinInputs misuses applyUplocoinInput and checks that a
// panic was triggered.
func TestMisuseApplyUplocoinInputs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Fetch the output id's of each Uplocoin output in the consensus set.
	var ids []types.UplocoinOutputID
	cst.cs.db.forEachUplocoinOutputs(func(id types.UplocoinOutputID, sco types.UplocoinOutput) {
		ids = append(ids, id)
	})

	// Apply a transaction with a single Uplocoin input.
	txn := types.Transaction{
		UplocoinInputs: []types.UplocoinInput{
			{ParentID: ids[0]},
		},
	}
	cst.cs.applyUplocoinInputs(pb, txn)

	// Trigger the panic that occurs when an output is applied incorrectly, and
	// perform a catch to read the error that is created.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting error after corrupting database")
		}
	}()
	cst.cs.applyUplocoinInputs(pb, txn)
}

// TestApplyUplocoinOutputs probes the applyUplocoinOutput method of the
// consensus set.
func TestApplyUplocoinOutputs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with a single Uplocoin output.
	txn := types.Transaction{
		UplocoinOutputs: []types.UplocoinOutput{{}},
	}
	cst.cs.applyUplocoinOutputs(pb, txn)
	scoid := txn.UplocoinOutputID(0)
	exists := cst.cs.db.inUplocoinOutputs(scoid)
	if !exists {
		t.Error("Failed to create Uplocoin output")
	}
	if cst.cs.db.lenUplocoinOutputs() != 3 { // 3 because createConsensusSetTester has 2 initially.
		t.Error("Uplocoin outputs not correctly updated")
	}
	if len(pb.UplocoinOutputDiffs) != 1 {
		t.Error("block node was not updated for single element transaction")
	}
	if pb.UplocoinOutputDiffs[0].Direction != modules.DiffApply {
		t.Error("wrong diff direction applied when creating a Uplocoin output")
	}
	if pb.UplocoinOutputDiffs[0].ID != scoid {
		t.Error("wrong id used when creating a Uplocoin output")
	}

	// Apply a transaction with 2 Uplocoin outputs.
	txn = types.Transaction{
		UplocoinOutputs: []types.UplocoinOutput{
			{Value: types.NewCurrency64(1)},
			{Value: types.NewCurrency64(2)},
		},
	}
	cst.cs.applyUplocoinOutputs(pb, txn)
	scoid0 := txn.UplocoinOutputID(0)
	scoid1 := txn.UplocoinOutputID(1)
	exists = cst.cs.db.inUplocoinOutputs(scoid0)
	if !exists {
		t.Error("Failed to create Uplocoin output")
	}
	exists = cst.cs.db.inUplocoinOutputs(scoid1)
	if !exists {
		t.Error("Failed to create Uplocoin output")
	}
	if cst.cs.db.lenUplocoinOutputs() != 5 { // 5 because createConsensusSetTester has 2 initially.
		t.Error("Uplocoin outputs not correctly updated")
	}
	if len(pb.UplocoinOutputDiffs) != 3 {
		t.Error("block node was not updated correctly")
	}
}

// TestMisuseApplyUplocoinOutputs misuses applyUplocoinOutputs and checks that a
// panic was triggered.
func TestMisuseApplyUplocoinOutputs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with a single Uplocoin output.
	txn := types.Transaction{
		UplocoinOutputs: []types.UplocoinOutput{{}},
	}
	cst.cs.applyUplocoinOutputs(pb, txn)

	// Trigger the panic that occurs when an output is applied incorrectly, and
	// perform a catch to read the error that is created.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("no panic occurred when misusing applyUplocoinInput")
		}
	}()
	cst.cs.applyUplocoinOutputs(pb, txn)
}

// TestApplyFileContracts probes the applyFileContracts method of the
// consensus set.
func TestApplyFileContracts(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with a single file contract.
	txn := types.Transaction{
		FileContracts: []types.FileContract{{}},
	}
	cst.cs.applyFileContracts(pb, txn)
	fcid := txn.FileContractID(0)
	exists := cst.cs.db.inFileContracts(fcid)
	if !exists {
		t.Error("Failed to create file contract")
	}
	if cst.cs.db.lenFileContracts() != 1 {
		t.Error("file contracts not correctly updated")
	}
	if len(pb.FileContractDiffs) != 1 {
		t.Error("block node was not updated for single element transaction")
	}
	if pb.FileContractDiffs[0].Direction != modules.DiffApply {
		t.Error("wrong diff direction applied when creating a file contract")
	}
	if pb.FileContractDiffs[0].ID != fcid {
		t.Error("wrong id used when creating a file contract")
	}

	// Apply a transaction with 2 file contracts.
	txn = types.Transaction{
		FileContracts: []types.FileContract{
			{Payout: types.NewCurrency64(1)},
			{Payout: types.NewCurrency64(300e3)},
		},
	}
	cst.cs.applyFileContracts(pb, txn)
	fcid0 := txn.FileContractID(0)
	fcid1 := txn.FileContractID(1)
	exists = cst.cs.db.inFileContracts(fcid0)
	if !exists {
		t.Error("Failed to create file contract")
	}
	exists = cst.cs.db.inFileContracts(fcid1)
	if !exists {
		t.Error("Failed to create file contract")
	}
	if cst.cs.db.lenFileContracts() != 3 {
		t.Error("file contracts not correctly updated")
	}
	if len(pb.FileContractDiffs) != 3 {
		t.Error("block node was not updated correctly")
	}
	if cst.cs.uplofundPool.Cmp64(10e3) != 0 {
		t.Error("uplofund pool did not update correctly upon creation of a file contract")
	}
}

// TestMisuseApplyFileContracts misuses applyFileContracts and checks that a
// panic was triggered.
func TestMisuseApplyFileContracts(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with a single file contract.
	txn := types.Transaction{
		FileContracts: []types.FileContract{{}},
	}
	cst.cs.applyFileContracts(pb, txn)

	// Trigger the panic that occurs when an output is applied incorrectly, and
	// perform a catch to read the error that is created.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("no panic occurred when misusing applyUplocoinInput")
		}
	}()
	cst.cs.applyFileContracts(pb, txn)
}

// TestApplyFileContractRevisions probes the applyFileContractRevisions method
// of the consensus set.
func TestApplyFileContractRevisions(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with two file contracts - that way there is
	// something to revise.
	txn := types.Transaction{
		FileContracts: []types.FileContract{
			{},
			{Payout: types.NewCurrency64(1)},
		},
	}
	cst.cs.applyFileContracts(pb, txn)
	fcid0 := txn.FileContractID(0)
	fcid1 := txn.FileContractID(1)

	// Apply a single file contract revision.
	txn = types.Transaction{
		FileContractRevisions: []types.FileContractRevision{
			{
				ParentID:    fcid0,
				NewFileSize: 1,
			},
		},
	}
	cst.cs.applyFileContractRevisions(pb, txn)
	exists := cst.cs.db.inFileContracts(fcid0)
	if !exists {
		t.Error("Revision killed a file contract")
	}
	fc := cst.cs.db.getFileContracts(fcid0)
	if fc.FileSize != 1 {
		t.Error("file contract filesize not properly updated")
	}
	if cst.cs.db.lenFileContracts() != 2 {
		t.Error("file contracts not correctly updated")
	}
	if len(pb.FileContractDiffs) != 4 { // 2 creating the initial contracts, 1 to remove the old, 1 to add the revision.
		t.Error("block node was not updated for single element transaction")
	}
	if pb.FileContractDiffs[2].Direction != modules.DiffRevert {
		t.Error("wrong diff direction applied when revising a file contract")
	}
	if pb.FileContractDiffs[3].Direction != modules.DiffApply {
		t.Error("wrong diff direction applied when revising a file contract")
	}
	if pb.FileContractDiffs[2].ID != fcid0 {
		t.Error("wrong id used when revising a file contract")
	}
	if pb.FileContractDiffs[3].ID != fcid0 {
		t.Error("wrong id used when revising a file contract")
	}

	// Apply a transaction with 2 file contract revisions.
	txn = types.Transaction{
		FileContractRevisions: []types.FileContractRevision{
			{
				ParentID:    fcid0,
				NewFileSize: 2,
			},
			{
				ParentID:    fcid1,
				NewFileSize: 3,
			},
		},
	}
	cst.cs.applyFileContractRevisions(pb, txn)
	exists = cst.cs.db.inFileContracts(fcid0)
	if !exists {
		t.Error("Revision ate file contract")
	}
	fc0 := cst.cs.db.getFileContracts(fcid0)
	exists = cst.cs.db.inFileContracts(fcid1)
	if !exists {
		t.Error("Revision ate file contract")
	}
	fc1 := cst.cs.db.getFileContracts(fcid1)
	if fc0.FileSize != 2 {
		t.Error("Revision not correctly applied")
	}
	if fc1.FileSize != 3 {
		t.Error("Revision not correctly applied")
	}
	if cst.cs.db.lenFileContracts() != 2 {
		t.Error("file contracts not correctly updated")
	}
	if len(pb.FileContractDiffs) != 8 {
		t.Error("block node was not updated correctly")
	}
}

// TestMisuseApplyFileContractRevisions misuses applyFileContractRevisions and
// checks that a panic was triggered.
func TestMisuseApplyFileContractRevisions(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Trigger a panic from revising a nonexistent file contract.
	defer func() {
		r := recover()
		if r != errNilItem {
			t.Error("no panic occurred when misusing applyUplocoinInput")
		}
	}()
	txn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{{}},
	}
	cst.cs.applyFileContractRevisions(pb, txn)
}

// TestApplyStorageProofs probes the applyStorageProofs method of the consensus
// set.
func TestApplyStorageProofs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)
	pb.Height = cst.cs.height()

	// Apply a transaction with two file contracts - there is a reason to
	// create a storage proof.
	txn := types.Transaction{
		FileContracts: []types.FileContract{
			{
				Payout: types.NewCurrency64(300e3),
				ValidProofOutputs: []types.UplocoinOutput{
					{Value: types.NewCurrency64(290e3)},
				},
			},
			{},
			{
				Payout: types.NewCurrency64(600e3),
				ValidProofOutputs: []types.UplocoinOutput{
					{Value: types.NewCurrency64(280e3)},
					{Value: types.NewCurrency64(300e3)},
				},
			},
		},
	}
	cst.cs.applyFileContracts(pb, txn)
	fcid0 := txn.FileContractID(0)
	fcid1 := txn.FileContractID(1)
	fcid2 := txn.FileContractID(2)

	// Apply a single storage proof.
	txn = types.Transaction{
		StorageProofs: []types.StorageProof{{ParentID: fcid0}},
	}
	cst.cs.applyStorageProofs(pb, txn)
	exists := cst.cs.db.inFileContracts(fcid0)
	if exists {
		t.Error("Storage proof did not disable a file contract.")
	}
	if cst.cs.db.lenFileContracts() != 2 {
		t.Error("file contracts not correctly updated")
	}
	if len(pb.FileContractDiffs) != 4 { // 3 creating the initial contracts, 1 for the storage proof.
		t.Error("block node was not updated for single element transaction")
	}
	if pb.FileContractDiffs[3].Direction != modules.DiffRevert {
		t.Error("wrong diff direction applied when revising a file contract")
	}
	if pb.FileContractDiffs[3].ID != fcid0 {
		t.Error("wrong id used when revising a file contract")
	}
	spoid0 := fcid0.StorageProofOutputID(types.ProofValid, 0)
	exists = cst.cs.db.inDelayedUplocoinOutputsHeight(pb.Height+types.MaturityDelay, spoid0)
	if !exists {
		t.Error("storage proof output not created after applying a storage proof")
	}
	sco := cst.cs.db.getDelayedUplocoinOutputs(pb.Height+types.MaturityDelay, spoid0)
	if sco.Value.Cmp64(290e3) != 0 {
		t.Error("storage proof output was created with the wrong value")
	}

	// Apply a transaction with 2 storage proofs.
	txn = types.Transaction{
		StorageProofs: []types.StorageProof{
			{ParentID: fcid1},
			{ParentID: fcid2},
		},
	}
	cst.cs.applyStorageProofs(pb, txn)
	exists = cst.cs.db.inFileContracts(fcid1)
	if exists {
		t.Error("Storage proof failed to consume file contract.")
	}
	exists = cst.cs.db.inFileContracts(fcid2)
	if exists {
		t.Error("storage proof did not consume file contract")
	}
	if cst.cs.db.lenFileContracts() != 0 {
		t.Error("file contracts not correctly updated")
	}
	if len(pb.FileContractDiffs) != 6 {
		t.Error("block node was not updated correctly")
	}
	spoid1 := fcid1.StorageProofOutputID(types.ProofValid, 0)
	exists = cst.cs.db.inUplocoinOutputs(spoid1)
	if exists {
		t.Error("output created when file contract had no corresponding output")
	}
	spoid2 := fcid2.StorageProofOutputID(types.ProofValid, 0)
	exists = cst.cs.db.inDelayedUplocoinOutputsHeight(pb.Height+types.MaturityDelay, spoid2)
	if !exists {
		t.Error("no output created by first output of file contract")
	}
	sco = cst.cs.db.getDelayedUplocoinOutputs(pb.Height+types.MaturityDelay, spoid2)
	if sco.Value.Cmp64(280e3) != 0 {
		t.Error("first Uplocoin output created has wrong value")
	}
	spoid3 := fcid2.StorageProofOutputID(types.ProofValid, 1)
	exists = cst.cs.db.inDelayedUplocoinOutputsHeight(pb.Height+types.MaturityDelay, spoid3)
	if !exists {
		t.Error("second output not created for storage proof")
	}
	sco = cst.cs.db.getDelayedUplocoinOutputs(pb.Height+types.MaturityDelay, spoid3)
	if sco.Value.Cmp64(300e3) != 0 {
		t.Error("second Uplocoin output has wrong value")
	}
	if cst.cs.uplofundPool.Cmp64(30e3) != 0 {
		t.Error("uplofund pool not being added up correctly")
	}
}

// TestNonexistentStorageProof applies a storage proof which points to a
// nonextentent parent.
func TestNonexistentStorageProof(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Trigger a panic by applying a storage proof for a nonexistent file
	// contract.
	defer func() {
		r := recover()
		if r != errNilItem {
			t.Error("no panic occurred when misusing applyUplocoinInput")
		}
	}()
	txn := types.Transaction{
		StorageProofs: []types.StorageProof{{}},
	}
	cst.cs.applyStorageProofs(pb, txn)
}

// TestDuplicateStorageProof applies a storage proof which has already been
// applied.
func TestDuplicateStorageProof(t *testing.T) {
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

	// Create a block node.
	pb := new(processedBlock)
	pb.Height = cst.cs.height()

	// Create a file contract for the storage proof to prove.
	txn0 := types.Transaction{
		FileContracts: []types.FileContract{
			{
				Payout: types.NewCurrency64(300e3),
				ValidProofOutputs: []types.UplocoinOutput{
					{Value: types.NewCurrency64(290e3)},
				},
			},
		},
	}
	cst.cs.applyFileContracts(pb, txn0)
	fcid := txn0.FileContractID(0)

	// Apply a single storage proof.
	txn1 := types.Transaction{
		StorageProofs: []types.StorageProof{{ParentID: fcid}},
	}
	cst.cs.applyStorageProofs(pb, txn1)

	// Trigger a panic by applying the storage proof again.
	defer func() {
		r := recover()
		if r != ErrDuplicateValidProofOutput {
			t.Error("failed to trigger ErrDuplicateValidProofOutput:", r)
		}
	}()
	cst.cs.applyFileContracts(pb, txn0) // File contract was consumed by the first proof.
	cst.cs.applyStorageProofs(pb, txn1)
}

// TestApplyUplofundInputs probes the applyUplofundInputs method of the consensus
// set.
func TestApplyUplofundInputs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)
	pb.Height = cst.cs.height()

	// Fetch the output id's of each Uplocoin output in the consensus set.
	var ids []types.UplofundOutputID
	cst.cs.db.forEachUplofundOutputs(func(sfoid types.UplofundOutputID, sfo types.UplofundOutput) {
		ids = append(ids, sfoid)
	})

	// Apply a transaction with a single uplofund input.
	txn := types.Transaction{
		UplofundInputs: []types.UplofundInput{
			{ParentID: ids[0]},
		},
	}
	cst.cs.applyUplofundInputs(pb, txn)
	exists := cst.cs.db.inUplofundOutputs(ids[0])
	if exists {
		t.Error("Failed to conusme a uplofund output")
	}
	if cst.cs.db.lenUplofundOutputs() != 2 {
		t.Error("uplofund outputs not correctly updated", cst.cs.db.lenUplofundOutputs())
	}
	if len(pb.UplofundOutputDiffs) != 1 {
		t.Error("block node was not updated for single transaction")
	}
	if pb.UplofundOutputDiffs[0].Direction != modules.DiffRevert {
		t.Error("wrong diff direction applied when consuming a uplofund output")
	}
	if pb.UplofundOutputDiffs[0].ID != ids[0] {
		t.Error("wrong id used when consuming a uplofund output")
	}
	if cst.cs.db.lenDelayedUplocoinOutputsHeight(cst.cs.height()+types.MaturityDelay) != 2 { // 1 for a block subsidy, 1 for the uplofund claim.
		t.Error("uplofund claim was not created")
	}
}

// TestMisuseApplyUplofundInputs misuses applyUplofundInputs and checks that a
// panic was triggered.
func TestMisuseApplyUplofundInputs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)
	pb.Height = cst.cs.height()

	// Fetch the output id's of each Uplocoin output in the consensus set.
	var ids []types.UplofundOutputID
	cst.cs.db.forEachUplofundOutputs(func(sfoid types.UplofundOutputID, sfo types.UplofundOutput) {
		ids = append(ids, sfoid)
	})

	// Apply a transaction with a single uplofund input.
	txn := types.Transaction{
		UplofundInputs: []types.UplofundInput{
			{ParentID: ids[0]},
		},
	}
	cst.cs.applyUplofundInputs(pb, txn)

	// Trigger the panic that occurs when an output is applied incorrectly, and
	// perform a catch to read the error that is created.
	defer func() {
		r := recover()
		if r != ErrMisuseApplyUplofundInput {
			t.Error("no panic occurred when misusing applyUplocoinInput")
			t.Error(r)
		}
	}()
	cst.cs.applyUplofundInputs(pb, txn)
}

// TestApplyUplofundOutputs probes the applyUplofundOutputs method of the
// consensus set.
func TestApplyUplofundOutputs(t *testing.T) {
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
	cst.cs.uplofundPool = types.NewCurrency64(101)

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with a single uplofund output.
	txn := types.Transaction{
		UplofundOutputs: []types.UplofundOutput{{}},
	}
	cst.cs.applyUplofundOutputs(pb, txn)
	sfoid := txn.UplofundOutputID(0)
	exists := cst.cs.db.inUplofundOutputs(sfoid)
	if !exists {
		t.Error("Failed to create uplofund output")
	}
	if cst.cs.db.lenUplofundOutputs() != 4 {
		t.Error("uplofund outputs not correctly updated")
	}
	if len(pb.UplofundOutputDiffs) != 1 {
		t.Error("block node was not updated for single element transaction")
	}
	if pb.UplofundOutputDiffs[0].Direction != modules.DiffApply {
		t.Error("wrong diff direction applied when creating a uplofund output")
	}
	if pb.UplofundOutputDiffs[0].ID != sfoid {
		t.Error("wrong id used when creating a uplofund output")
	}
	if pb.UplofundOutputDiffs[0].UplofundOutput.ClaimStart.Cmp64(101) != 0 {
		t.Error("claim start set incorrectly when creating a uplofund output")
	}

	// Apply a transaction with 2 Uplocoin outputs.
	txn = types.Transaction{
		UplofundOutputs: []types.UplofundOutput{
			{Value: types.NewCurrency64(1)},
			{Value: types.NewCurrency64(2)},
		},
	}
	cst.cs.applyUplofundOutputs(pb, txn)
	sfoid0 := txn.UplofundOutputID(0)
	sfoid1 := txn.UplofundOutputID(1)
	exists = cst.cs.db.inUplofundOutputs(sfoid0)
	if !exists {
		t.Error("Failed to create uplofund output")
	}
	exists = cst.cs.db.inUplofundOutputs(sfoid1)
	if !exists {
		t.Error("Failed to create uplofund output")
	}
	if cst.cs.db.lenUplofundOutputs() != 6 {
		t.Error("uplofund outputs not correctly updated")
	}
	if len(pb.UplofundOutputDiffs) != 3 {
		t.Error("block node was not updated for single element transaction")
	}
}

// TestMisuseApplyUplofundOutputs misuses applyUplofundOutputs and checks that a
// panic was triggered.
func TestMisuseApplyUplofundOutputs(t *testing.T) {
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

	// Create a block node to use with application.
	pb := new(processedBlock)

	// Apply a transaction with a single Uplocoin output.
	txn := types.Transaction{
		UplofundOutputs: []types.UplofundOutput{{}},
	}
	cst.cs.applyUplofundOutputs(pb, txn)

	// Trigger the panic that occurs when an output is applied incorrectly, and
	// perform a catch to read the error that is created.
	defer func() {
		r := recover()
		if r != ErrMisuseApplyUplofundOutput {
			t.Error("no panic occurred when misusing applyUplofundInput")
		}
	}()
	cst.cs.applyUplofundOutputs(pb, txn)
}
*/

// TestApplyArbitraryData probes the applyArbitraryData function.
func TestApplyArbitraryData(t *testing.T) {
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

	apply := func(txn types.Transaction, height types.BlockHeight) {
		err := cst.cs.db.Update(func(tx *bolt.Tx) error {
			// applyArbitraryData expects a BlockPath entry at this height
			tx.Bucket(BlockPath).Put(encoding.Marshal(height), encoding.Marshal(types.BlockID{}))
			applyArbitraryData(tx, &processedBlock{Height: height}, txn)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	addrsChanged := func() bool {
		p, f := cst.cs.FoundationUnlockHashes()
		return p != types.InitialFoundationUnlockHash || f != types.InitialFoundationFailsafeUnlockHash
	}

	// Apply an empty transaction
	apply(types.Transaction{}, types.FoundationHardforkHeight)
	if addrsChanged() {
		t.Error("addrs should not have changed after applying empty txn")
	}

	// Apply data with an invalid prefix -- it should be ignored
	data := encoding.MarshalAll(types.Specifier{'f', 'o', 'o'}, types.FoundationUnlockHashUpdate{})
	apply(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight)
	if addrsChanged() {
		t.Error("addrs should not have changed after applying invalid txn")
	}

	// Apply a validate update before the hardfork -- it should be ignored
	update := types.FoundationUnlockHashUpdate{
		NewPrimary:  types.UnlockHash{1, 2, 3},
		NewFailsafe: types.UnlockHash{4, 5, 6},
	}
	data = encoding.MarshalAll(types.SpecifierFoundation, update)
	apply(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight-1)
	if addrsChanged() {
		t.Fatal("applying valid update before hardfork should not change unlock hashes")
	}
	// Apply the update after the hardfork
	apply(types.Transaction{ArbitraryData: [][]byte{data}}, types.FoundationHardforkHeight)
	if !addrsChanged() {
		t.Fatal("applying valid update did not change unlock hashes")
	}
	// Check that database was updated correctly
	if newPrimary, newFailsafe := cst.cs.FoundationUnlockHashes(); newPrimary != update.NewPrimary || newFailsafe != update.NewFailsafe {
		t.Error("applying valid update did not change unlock hashes")
	}

	// Apply a transaction with two updates; only the first should be applied
	up1 := types.FoundationUnlockHashUpdate{
		NewPrimary:  types.UnlockHash{1, 1, 1},
		NewFailsafe: types.UnlockHash{2, 2, 2},
	}
	up2 := types.FoundationUnlockHashUpdate{
		NewPrimary:  types.UnlockHash{3, 3, 3},
		NewFailsafe: types.UnlockHash{4, 4, 4},
	}
	data1 := encoding.MarshalAll(types.SpecifierFoundation, up1)
	data2 := encoding.MarshalAll(types.SpecifierFoundation, up2)
	apply(types.Transaction{ArbitraryData: [][]byte{data1, data2}}, types.FoundationHardforkHeight+1)
	if newPrimary, newFailsafe := cst.cs.FoundationUnlockHashes(); newPrimary != up1.NewPrimary || newFailsafe != up1.NewFailsafe {
		t.Error("applying two updates did not apply only the first", newPrimary, newFailsafe)
	}
}
