package types

import (
	"bytes"
	"testing"

	"github.com/uplo-tech/uplo/crypto"
)

// TestTransactionIDs probes all of the ID functions of the Transaction type.
func TestIDs(t *testing.T) {
	// Create every type of ID using empty fields.
	txn := Transaction{
		UplocoinOutputs: []UplocoinOutput{{}},
		FileContracts:  []FileContract{{}},
		UplofundOutputs: []UplofundOutput{{}},
	}
	tid := txn.ID()
	scoid := txn.UplocoinOutputID(0)
	fcid := txn.FileContractID(0)
	spidT := fcid.StorageProofOutputID(ProofValid, 0)
	spidF := fcid.StorageProofOutputID(ProofMissed, 0)
	sfoid := txn.UplofundOutputID(0)
	scloid := sfoid.uploclaimOutputID()

	// Put all of the ids into a slice.
	var ids []crypto.Hash
	ids = append(ids,
		crypto.Hash(tid),
		crypto.Hash(scoid),
		crypto.Hash(fcid),
		crypto.Hash(spidT),
		crypto.Hash(spidF),
		crypto.Hash(sfoid),
		crypto.Hash(scloid),
	)

	// Check that each id is unique.
	knownIDs := make(map[crypto.Hash]struct{})
	for i, id := range ids {
		_, exists := knownIDs[id]
		if exists {
			t.Error("id repeat for index", i)
		}
		knownIDs[id] = struct{}{}
	}
}

// TestTransactionUplocoinOutputSum probes the UplocoinOutputSum method of the
// Transaction type.
func TestTransactionUplocoinOutputSum(t *testing.T) {
	// Create a transaction with all types of Uplocoin outputs.
	txn := Transaction{
		UplocoinOutputs: []UplocoinOutput{
			{Value: NewCurrency64(1)},
			{Value: NewCurrency64(20)},
		},
		FileContracts: []FileContract{
			{Payout: NewCurrency64(300)},
			{Payout: NewCurrency64(4000)},
		},
		MinerFees: []Currency{
			NewCurrency64(50000),
			NewCurrency64(600000),
		},
	}
	if txn.UplocoinOutputSum().Cmp(NewCurrency64(654321)) != 0 {
		t.Error("wrong Uplocoin output sum was calculated, got:", txn.UplocoinOutputSum())
	}
}

// TestRuneToString makes sure a correct specifier is created appending the
// result of RuneToString to a string.
func TestRuneToString(t *testing.T) {
	t.Parallel()

	specifier := NewSpecifier("Download" + RuneToString(2))
	expectedSpecifier := Specifier{68, 111, 119, 110, 108, 111, 97, 100, 2, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(specifier[:], expectedSpecifier[:]) {
		t.Fatal("failure")
	}
}
