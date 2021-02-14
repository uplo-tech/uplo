package types

import (
	"testing"

	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
)

// TestTransactionCorrectFileContracts probes the correctFileContracts function
// of the Transaction type.
func TestTransactionCorrectFileContracts(t *testing.T) {
	// Try a transaction with a FileContract that is correct.
	txn := Transaction{
		FileContracts: []FileContract{
			{
				WindowStart: 35,
				WindowEnd:   40,
				Payout:      NewCurrency64(1e6),
				ValidProofOutputs: []UplocoinOutput{
					{Value: NewCurrency64(70e3)},
					{Value: NewCurrency64(900e3)},
				},
				MissedProofOutputs: []UplocoinOutput{
					{Value: NewCurrency64(70e3)},
					{Value: NewCurrency64(900e3)},
				},
			},
		},
	}
	err := txn.correctFileContracts(30)
	if err != nil {
		t.Error(err)
	}

	// Try when the start height was missed.
	err = txn.correctFileContracts(35)
	if !errors.Contains(err, ErrFileContractWindowStartViolation) {
		t.Error(err)
	}
	err = txn.correctFileContracts(135)
	if !errors.Contains(err, ErrFileContractWindowStartViolation) {
		t.Error(err)
	}

	// Try when the expiration equal to and less than the start.
	txn.FileContracts[0].WindowEnd = 35
	err = txn.correctFileContracts(30)
	if !errors.Contains(err, ErrFileContractWindowEndViolation) {
		t.Error(err)
	}
	txn.FileContracts[0].WindowEnd = 35
	err = txn.correctFileContracts(30)
	if !errors.Contains(err, ErrFileContractWindowEndViolation) {
		t.Error(err)
	}
	txn.FileContracts[0].WindowEnd = 40

	// Attempt under and over output sums.
	txn.FileContracts[0].SetValidRenterPayout(NewCurrency64(69e3))
	err = txn.correctFileContracts(30)
	if !errors.Contains(err, ErrFileContractOutputSumViolation) {
		t.Error(err)
	}
	txn.FileContracts[0].SetValidRenterPayout(NewCurrency64(71e3))
	err = txn.correctFileContracts(30)
	if !errors.Contains(err, ErrFileContractOutputSumViolation) {
		t.Error(err)
	}
	txn.FileContracts[0].SetValidRenterPayout(NewCurrency64(70e3))

	txn.FileContracts[0].SetMissedRenterPayout(NewCurrency64(69e3))
	err = txn.correctFileContracts(30)
	if !errors.Contains(err, ErrFileContractOutputSumViolation) {
		t.Error(err)
	}
	txn.FileContracts[0].SetMissedRenterPayout(NewCurrency64(71e3))
	err = txn.correctFileContracts(30)
	if !errors.Contains(err, ErrFileContractOutputSumViolation) {
		t.Error(err)
	}
	txn.FileContracts[0].SetMissedRenterPayout(NewCurrency64(70e3))

	// Try the payouts when the value of the contract is too low to incur a
	// fee.
	txn.FileContracts = append(txn.FileContracts, FileContract{
		WindowStart: 35,
		WindowEnd:   40,
		Payout:      NewCurrency64(1e3),
		ValidProofOutputs: []UplocoinOutput{
			{Value: NewCurrency64(1e3)},
		},
		MissedProofOutputs: []UplocoinOutput{
			{Value: NewCurrency64(1e3)},
		},
	})
	err = txn.correctFileContracts(30)
	if err != nil {
		t.Error(err)
	}
}

// TestCorrectFileContractRevisions probes the correctFileContractRevisions
// method of the Transaction type.
func TestCorrectFileContractRevisions(t *testing.T) {
	// Try a revision that starts in the past.
	txn := Transaction{
		FileContractRevisions: []FileContractRevision{{}},
	}
	err := txn.correctFileContractRevisions(0)
	if !errors.Contains(err, ErrFileContractWindowStartViolation) {
		t.Error(err)
	}

	// Try a revision that has a window which ends before it starts.
	txn = Transaction{
		FileContractRevisions: []FileContractRevision{
			{NewWindowStart: 1},
		},
	}
	err = txn.correctFileContractRevisions(0)
	if !errors.Contains(err, ErrFileContractWindowEndViolation) {
		t.Error(err)
	}

	// Try a revision with misaligned payouts.
	txn.FileContractRevisions = []FileContractRevision{
		{
			NewWindowStart: 1,
			NewWindowEnd:   2,
			NewMissedProofOutputs: []UplocoinOutput{
				{Value: NewCurrency64(10)},
			},
		},
	}
	err = txn.correctFileContractRevisions(0)
	if !errors.Contains(err, ErrFileContractOutputSumViolation) {
		t.Error("Expecting ErrFileContractOutputSumViolation:", err)
	}
}

// TestCorrectArbitraryData probes the correctArbitraryData
// method of the Transaction type.
func TestCorrectArbitraryData(t *testing.T) {
	// Try an invalid update prior to the hardfork height.
	txn := Transaction{
		ArbitraryData: [][]byte{encoding.MarshalAll(SpecifierFoundation, [...]byte{1, 2, 3})},
	}
	if err := txn.correctArbitraryData(FoundationHardforkHeight - 1); err != nil {
		t.Error(err)
	}
	// Try after the hardfork height.
	if err := txn.correctArbitraryData(FoundationHardforkHeight); !errors.Contains(err, ErrInvalidFoundationUpdateEncoding) {
		t.Error(err)
	}

	// Try an uninitialized update prior to the hardfork height.
	txn.ArbitraryData[0] = encoding.MarshalAll(SpecifierFoundation, FoundationUnlockHashUpdate{})
	if err := txn.correctArbitraryData(FoundationHardforkHeight - 1); err != nil {
		t.Error(err)
	}
	// Try after the hardfork height.
	if err := txn.correctArbitraryData(FoundationHardforkHeight); !errors.Contains(err, ErrUninitializedFoundationUpdate) {
		t.Error(err)
	}

	// Try an valid update prior to the hardfork height.
	txn.ArbitraryData[0] = encoding.MarshalAll(SpecifierFoundation, FoundationUnlockHashUpdate{
		NewPrimary:  UnlockHash{1, 2, 3},
		NewFailsafe: UnlockHash{4, 5, 6},
	})
	if err := txn.correctArbitraryData(FoundationHardforkHeight - 1); err != nil {
		t.Error(err)
	}
	// Try after the hardfork height.
	if err := txn.correctArbitraryData(FoundationHardforkHeight); err != nil {
		t.Error(err)
	}
}

// TestTransactionFitsInABlock probes the fitsInABlock method of the
// Transaction type.
func TestTransactionFitsInABlock(t *testing.T) {
	// Try a transaction that will fit in a block, followed by one that won't.
	data := make([]byte, BlockSizeLimit/2)
	txn := Transaction{ArbitraryData: [][]byte{data}}
	err := txn.fitsInABlock(0)
	if err != nil {
		t.Error(err)
	}
	data = make([]byte, BlockSizeLimit)
	txn.ArbitraryData[0] = data
	err = txn.fitsInABlock(0)
	if !errors.Contains(err, ErrTransactionTooLarge) {
		t.Error(err)
	}

	// Try a too-large transaction before and after the hardfork height.
	data = make([]byte, OakHardforkTxnSizeLimit+1)
	txn.ArbitraryData[0] = data
	err = txn.fitsInABlock(0)
	if err != nil {
		t.Error(err)
	}
	err = txn.fitsInABlock(OakHardforkBlock)
	if !errors.Contains(err, ErrTransactionTooLarge) {
		t.Error(err)
	}
}

// TestTransactionFollowsMinimumValues probes the followsMinimumValues method
// of the Transaction type.
func TestTransactionFollowsMinimumValues(t *testing.T) {
	// Start with a transaction that follows all of minimum-values rules.
	txn := Transaction{
		UplocoinOutputs: []UplocoinOutput{{Value: NewCurrency64(1)}},
		FileContracts:  []FileContract{{Payout: NewCurrency64(1)}},
		UplofundOutputs: []UplofundOutput{{Value: NewCurrency64(1)}},
		MinerFees:      []Currency{NewCurrency64(1)},
	}
	err := txn.followsMinimumValues()
	if err != nil {
		t.Error(err)
	}

	// Try a zero value for each type.
	txn.UplocoinOutputs[0].Value = ZeroCurrency
	err = txn.followsMinimumValues()
	if !errors.Contains(err, ErrZeroOutput) {
		t.Error(err)
	}
	txn.UplocoinOutputs[0].Value = NewCurrency64(1)
	txn.FileContracts[0].Payout = ZeroCurrency
	err = txn.followsMinimumValues()
	if !errors.Contains(err, ErrZeroOutput) {
		t.Error(err)
	}
	txn.FileContracts[0].Payout = NewCurrency64(1)
	txn.UplofundOutputs[0].Value = ZeroCurrency
	err = txn.followsMinimumValues()
	if !errors.Contains(err, ErrZeroOutput) {
		t.Error(err)
	}
	txn.UplofundOutputs[0].Value = NewCurrency64(1)
	txn.MinerFees[0] = ZeroCurrency
	err = txn.followsMinimumValues()
	if !errors.Contains(err, ErrZeroMinerFee) {
		t.Error(err)
	}
	txn.MinerFees[0] = NewCurrency64(1)

	// Try a non-zero value for the ClaimStart field of a uplofund output.
	txn.UplofundOutputs[0].ClaimStart = NewCurrency64(1)
	err = txn.followsMinimumValues()
	if !errors.Contains(err, ErrNonZeroClaimStart) {
		t.Error(err)
	}
	txn.UplofundOutputs[0].ClaimStart = ZeroCurrency
}

// TestTransactionFollowsStorageProofRules probes the followsStorageProofRules
// method of the Transaction type.
func TestTransactionFollowsStorageProofRules(t *testing.T) {
	// Try a transaction with no storage proofs.
	txn := Transaction{}
	err := txn.followsStorageProofRules()
	if err != nil {
		t.Error(err)
	}

	// Try a transaction with a legal storage proof.
	txn.StorageProofs = append(txn.StorageProofs, StorageProof{})
	err = txn.followsStorageProofRules()
	if err != nil {
		t.Error(err)
	}

	// Try a transaction with a storage proof and a UplocoinOutput.
	txn.UplocoinOutputs = append(txn.UplocoinOutputs, UplocoinOutput{})
	err = txn.followsStorageProofRules()
	if !errors.Contains(err, ErrStorageProofWithOutputs) {
		t.Error(err)
	}
	txn.UplocoinOutputs = nil

	// Try a transaction with a storage proof and a FileContract.
	txn.FileContracts = append(txn.FileContracts, FileContract{})
	err = txn.followsStorageProofRules()
	if !errors.Contains(err, ErrStorageProofWithOutputs) {
		t.Error(err)
	}
	txn.FileContracts = nil

	// Try a transaction with a storage proof and a FileContractRevision.
	txn.FileContractRevisions = append(txn.FileContractRevisions, FileContractRevision{})
	err = txn.followsStorageProofRules()
	if !errors.Contains(err, ErrStorageProofWithOutputs) {
		t.Error(err)
	}
	txn.FileContractRevisions = nil

	// Try a transaction with a storage proof and a FileContractRevision.
	txn.UplofundOutputs = append(txn.UplofundOutputs, UplofundOutput{})
	err = txn.followsStorageProofRules()
	if !errors.Contains(err, ErrStorageProofWithOutputs) {
		t.Error(err)
	}
	txn.UplofundOutputs = nil
}

// TestTransactionNoRepeats probes the noRepeats method of the Transaction
// type.
func TestTransactionNoRepeats(t *testing.T) {
	// Try a transaction all the repeatable types but no conflicts.
	txn := Transaction{
		UplocoinInputs:         []UplocoinInput{{}},
		StorageProofs:         []StorageProof{{}},
		FileContractRevisions: []FileContractRevision{{}},
		UplofundInputs:         []UplofundInput{{}},
	}
	txn.FileContractRevisions[0].ParentID[0] = 1 // Otherwise it will conflict with the storage proof.
	err := txn.noRepeats()
	if err != nil {
		t.Error(err)
	}

	// Try a transaction double spending a Uplocoin output.
	txn.UplocoinInputs = append(txn.UplocoinInputs, UplocoinInput{})
	err = txn.noRepeats()
	if !errors.Contains(err, ErrDoubleSpend) {
		t.Error(err)
	}
	txn.UplocoinInputs = txn.UplocoinInputs[:1]

	// Try double spending a file contract, checking that both storage proofs
	// and terminations can conflict with each other.
	txn.StorageProofs = append(txn.StorageProofs, StorageProof{})
	err = txn.noRepeats()
	if !errors.Contains(err, ErrDoubleSpend) {
		t.Error(err)
	}
	txn.StorageProofs = txn.StorageProofs[:1]

	// Have the storage proof conflict with the file contract termination.
	txn.StorageProofs[0].ParentID[0] = 1
	err = txn.noRepeats()
	if !errors.Contains(err, ErrDoubleSpend) {
		t.Error(err)
	}
	txn.StorageProofs[0].ParentID[0] = 0

	// Have the file contract termination conflict with itself.
	txn.FileContractRevisions = append(txn.FileContractRevisions, FileContractRevision{})
	txn.FileContractRevisions[1].ParentID[0] = 1
	err = txn.noRepeats()
	if !errors.Contains(err, ErrDoubleSpend) {
		t.Error(err)
	}
	txn.FileContractRevisions = txn.FileContractRevisions[:1]

	// Try a transaction double spending a uplofund output.
	txn.UplofundInputs = append(txn.UplofundInputs, UplofundInput{})
	err = txn.noRepeats()
	if !errors.Contains(err, ErrDoubleSpend) {
		t.Error(err)
	}
	txn.UplofundInputs = txn.UplofundInputs[:1]
}

// TestValudUnlockConditions probes the validUnlockConditions function.
func TestValidUnlockConditions(t *testing.T) {
	// The only thing to check is the timelock.
	uc := UnlockConditions{Timelock: 3}
	err := validUnlockConditions(uc, 2)
	if !errors.Contains(err, ErrTimelockNotSatisfied) {
		t.Error(err)
	}
	err = validUnlockConditions(uc, 3)
	if err != nil {
		t.Error(err)
	}
	err = validUnlockConditions(uc, 4)
	if err != nil {
		t.Error(err)
	}
}

// TestTransactionValidUnlockConditions probes the validUnlockConditions method
// of the transaction type.
func TestTransactionValidUnlockConditions(t *testing.T) {
	// Create a transaction with each type of valid unlock condition.
	txn := Transaction{
		UplocoinInputs: []UplocoinInput{
			{UnlockConditions: UnlockConditions{Timelock: 3}},
		},
		FileContractRevisions: []FileContractRevision{
			{UnlockConditions: UnlockConditions{Timelock: 3}},
		},
		UplofundInputs: []UplofundInput{
			{UnlockConditions: UnlockConditions{Timelock: 3}},
		},
	}
	err := txn.validUnlockConditions(4)
	if err != nil {
		t.Error(err)
	}

	// Try with illegal conditions in the Uplocoin inputs.
	txn.UplocoinInputs[0].UnlockConditions.Timelock = 5
	err = txn.validUnlockConditions(4)
	if err == nil {
		t.Error(err)
	}
	txn.UplocoinInputs[0].UnlockConditions.Timelock = 3

	// Try with illegal conditions in the uplofund inputs.
	txn.FileContractRevisions[0].UnlockConditions.Timelock = 5
	err = txn.validUnlockConditions(4)
	if err == nil {
		t.Error(err)
	}
	txn.FileContractRevisions[0].UnlockConditions.Timelock = 3

	// Try with illegal conditions in the uplofund inputs.
	txn.UplofundInputs[0].UnlockConditions.Timelock = 5
	err = txn.validUnlockConditions(4)
	if err == nil {
		t.Error(err)
	}
	txn.UplofundInputs[0].UnlockConditions.Timelock = 3
}

// TestTransactionStandaloneValid probes the StandaloneValid method of the
// Transaction type.
func TestTransactionStandaloneValid(t *testing.T) {
	// Build a working transaction.
	var txn Transaction
	err := txn.StandaloneValid(0)
	if err != nil {
		t.Error(err)
	}

	// Violate fitsInABlock.
	data := make([]byte, BlockSizeLimit)
	txn.ArbitraryData = [][]byte{data}
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger fitsInABlock error")
	}
	txn.ArbitraryData = nil

	// Violate followsStorageProofRules
	txn.StorageProofs = []StorageProof{{}}
	txn.UplocoinOutputs = []UplocoinOutput{{}}
	txn.UplocoinOutputs[0].Value = NewCurrency64(1)
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger followsStorageProofRules error")
	}
	txn.StorageProofs = nil
	txn.UplocoinOutputs = nil

	// Violate noRepeats
	txn.UplocoinInputs = []UplocoinInput{{}, {}}
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger noRepeats error")
	}
	txn.UplocoinInputs = nil

	// Violate followsMinimumValues
	txn.UplocoinOutputs = []UplocoinOutput{{}}
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger followsMinimumValues error")
	}
	txn.UplocoinOutputs = nil

	// Violate correctFileContracts
	txn.FileContracts = []FileContract{
		{
			Payout:      NewCurrency64(1),
			WindowStart: 5,
			WindowEnd:   5,
		},
	}
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger correctFileContracts error")
	}
	txn.FileContracts = nil

	// Violate correctFileContractRevisions
	txn.FileContractRevisions = []FileContractRevision{{}}
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger correctFileContractRevisions error")
	}
	txn.FileContractRevisions = nil

	// Violate validUnlockConditions
	txn.UplocoinInputs = []UplocoinInput{{}}
	txn.UplocoinInputs[0].UnlockConditions.Timelock = 1
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger validUnlockConditions error")
	}
	txn.UplocoinInputs = nil

	// Violate validSignatures
	txn.TransactionSignatures = []TransactionSignature{{}}
	err = txn.StandaloneValid(0)
	if err == nil {
		t.Error("failed to trigger validSignatures error")
	}
	txn.TransactionSignatures = nil
}
