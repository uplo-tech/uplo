package typesutil

import (
	"testing"

	"github.com/uplo-tech/uplo/types"

	"github.com/uplo-tech/errors"
)

// TestTransactionGraph will check that the basic construction of a transaction
// graph works as expected.
func TestTransactionGraph(t *testing.T) {
	// Make a basic transaction.
	var source types.UplocoinOutputID
	tg := NewTransactionGraph()
	index, err := tg.AddUplocoinSource(source, types.UplocoinPrecision.Mul64(3))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tg.AddUplocoinSource(source, types.UplocoinPrecision.Mul64(3))
	if !errors.Contains(err, ErrUplocoinSourceAlreadyAdded) {
		t.Fatal("should not be able to add the same Uplocoin input source multiple times")
	}
	newIndexes, err := tg.AddTransaction(SimpleTransaction{
		UplocoinInputs:  []int{index},
		UplocoinOutputs: []types.Currency{types.UplocoinPrecision.Mul64(2)},
		MinerFees:      []types.Currency{types.UplocoinPrecision},
	})
	if err != nil {
		t.Fatal(err)
	}
	txns := tg.Transactions()
	if len(txns) != 1 {
		t.Fatal("expected to get one transaction")
	}
	// Check that the transaction is standalone valid.
	err = txns[0].StandaloneValid(0)
	if err != nil {
		t.Fatal("transactions produced by graph should be valid")
	}

	// Try to build a transaction that has a value mismatch, ensure there is an
	// error.
	_, err = tg.AddTransaction(SimpleTransaction{
		UplocoinInputs:  []int{newIndexes[0]},
		UplocoinOutputs: []types.Currency{types.UplocoinPrecision.Mul64(2)},
		MinerFees:      []types.Currency{types.UplocoinPrecision},
	})
	if !errors.Contains(err, ErrUplocoinInputsOutputsMismatch) {
		t.Fatal("An error should be returned when a transaction's outputs and inputs mismatch")
	}
	_, err = tg.AddTransaction(SimpleTransaction{
		UplocoinInputs:  []int{2},
		UplocoinOutputs: []types.Currency{types.UplocoinPrecision},
		MinerFees:      []types.Currency{types.UplocoinPrecision},
	})
	if !errors.Contains(err, ErrNoSuchUplocoinInput) {
		t.Fatal("An error should be returned when a transaction spends a missing input")
	}
	_, err = tg.AddTransaction(SimpleTransaction{
		UplocoinInputs:  []int{0},
		UplocoinOutputs: []types.Currency{types.UplocoinPrecision},
		MinerFees:      []types.Currency{types.UplocoinPrecision},
	})
	if !errors.Contains(err, ErrUplocoinInputAlreadyUsed) {
		t.Fatal("Error should be returned when a transaction spends an input that has been spent before")
	}

	// Build a correct second transaction, see that it validates.
	_, err = tg.AddTransaction(SimpleTransaction{
		UplocoinInputs:  []int{newIndexes[0]},
		UplocoinOutputs: []types.Currency{types.UplocoinPrecision},
		MinerFees:      []types.Currency{types.UplocoinPrecision},
	})
	if err != nil {
		t.Fatal("Transaction was built incorrectly", err)
	}
}
