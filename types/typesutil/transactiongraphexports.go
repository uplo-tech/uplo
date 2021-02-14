package typesutil

import (
	"fmt"

	"github.com/uplo-tech/uplo/types"

	"github.com/uplo-tech/errors"
)

var (
	// AnyoneCanSpendUnlockHash is the unlock hash of unlock conditions that are
	// trivially spendable.
	AnyoneCanSpendUnlockHash types.UnlockHash = types.UnlockConditions{}.UnlockHash()
)

var (
	// ErrUplocoinSourceAlreadyAdded is the error returned when a user tries to
	// provide the same source Uplocoin input multiple times.
	ErrUplocoinSourceAlreadyAdded = errors.New("source Uplocoin input has already been used")

	// ErrUplocoinInputAlreadyUsed warns a user that a Uplocoin input has already
	// been used in the transaction graph.
	ErrUplocoinInputAlreadyUsed = errors.New("cannot use the same Uplocoin input twice in a graph")

	// ErrNoSuchUplocoinInput warns a user that they are trying to reference a
	// Uplocoin input which does not yet exist.
	ErrNoSuchUplocoinInput = errors.New("no Uplocoin input exists with that index")

	// ErrUplocoinInputsOutputsMismatch warns a user that they have constructed a
	// transaction which does not spend the same amount of Uplocoins that it
	// consumes.
	ErrUplocoinInputsOutputsMismatch = errors.New("Uplocoin input value to transaction does not match Uplocoin output value of transaction")
)

// UplocoinInput defines a Uplocoin input within the transaction graph, containing
// the input itself, the value of the input, and a flag indicating whether or
// not the input has been used within the graph already.
type UplocoinInput struct {
	input types.UplocoinInput
	used  bool
	value types.Currency
}

// TransactionGraph is a helper tool to allow a user to easily construct
// elaborate transaction graphs. The transaction tool will handle creating valid
// transactions, providing the user with a clean interface for building
// transactions.
type TransactionGraph struct {
	// A map that tracks which source inputs have been consumed, to double check
	// that the user is not supplying the same source inputs multiple times.
	usedUplocoinInputSources map[types.UplocoinOutputID]struct{}

	UplocoinInputs []UplocoinInput

	transactions []types.Transaction
}

// SimpleTransaction specifies what outputs it spends, and what outputs it
// creates, by index. When passed in TransactionGraph, it will be automatically
// transformed into a valid transaction.
//
// Currently, there is only support for UplocoinInputs, UplocoinOutputs, and
// MinerFees, however the code has been structured so that support for Uplofunds
// and FileContracts can be easily added in the future.
type SimpleTransaction struct {
	UplocoinInputs  []int            // Which inputs to use, by index.
	UplocoinOutputs []types.Currency // The values of each output.

	/*
		UplofundInputs  []int            // Which inputs to use, by index.
		UplofundOutputs []types.Currency // The values of each output.

		FileContracts         int   // The number of file contracts to create.
		FileContractRevisions []int // Which file contracts to revise.
		StorageProofs         []int // Which file contracts to create proofs for.
	*/

	MinerFees []types.Currency // The fees used.

	/*
		ArbitraryData [][]byte // Arbitrary data to include in the transaction.
	*/
}

// AddUplocoinSource will add a new source of Uplocoins to the transaction graph,
// returning the index that this source can be referenced by. The provided
// output must have the address AnyoneCanSpendUnlockHash.
//
// The value is used as an input so that the graph can check whether all
// transactions are spending as many Uplocoins as they create.
func (tg *TransactionGraph) AddUplocoinSource(scoid types.UplocoinOutputID, value types.Currency) (int, error) {
	// Check if this scoid has already been used.
	_, exists := tg.usedUplocoinInputSources[scoid]
	if exists {
		return -1, ErrUplocoinSourceAlreadyAdded
	}

	i := len(tg.UplocoinInputs)
	tg.UplocoinInputs = append(tg.UplocoinInputs, UplocoinInput{
		input: types.UplocoinInput{
			ParentID: scoid,
		},
		value: value,
	})
	tg.usedUplocoinInputSources[scoid] = struct{}{}
	return i, nil
}

// AddTransaction will add a new transaction to the transaction graph, following
// the guide of the input. The indexes of all the outputs created will be
// returned.
func (tg *TransactionGraph) AddTransaction(st SimpleTransaction) (newUplocoinInputs []int, err error) {
	var txn types.Transaction
	var totalIn types.Currency
	var totalOut types.Currency

	// Consume all of the inputs.
	for _, sci := range st.UplocoinInputs {
		if sci >= len(tg.UplocoinInputs) {
			return nil, ErrNoSuchUplocoinInput
		}
		if tg.UplocoinInputs[sci].used {
			return nil, ErrUplocoinInputAlreadyUsed
		}
		txn.UplocoinInputs = append(txn.UplocoinInputs, tg.UplocoinInputs[sci].input)
		totalIn = totalIn.Add(tg.UplocoinInputs[sci].value)
	}

	// Create all of the outputs.
	for _, scov := range st.UplocoinOutputs {
		txn.UplocoinOutputs = append(txn.UplocoinOutputs, types.UplocoinOutput{
			UnlockHash: AnyoneCanSpendUnlockHash,
			Value:      scov,
		})
		totalOut = totalOut.Add(scov)
	}

	// Add all of the fees.
	txn.MinerFees = st.MinerFees
	for _, fee := range st.MinerFees {
		totalOut = totalOut.Add(fee)
	}

	// Check that the transaction is consistent.
	if totalIn.Cmp(totalOut) != 0 {
		valuesErr := fmt.Errorf("total input: %s, total output: %s", totalIn, totalOut)
		extendedErr := errors.Extend(ErrUplocoinInputsOutputsMismatch, valuesErr)
		return nil, extendedErr
	}

	// Update the set of Uplocoin inputs that have been used successfully. This
	// must be done after all error checking is complete.
	for _, sci := range st.UplocoinInputs {
		tg.UplocoinInputs[sci].used = true
	}
	tg.transactions = append(tg.transactions, txn)
	for i, sco := range txn.UplocoinOutputs {
		newUplocoinInputs = append(newUplocoinInputs, len(tg.UplocoinInputs))
		tg.UplocoinInputs = append(tg.UplocoinInputs, UplocoinInput{
			input: types.UplocoinInput{
				ParentID: txn.UplocoinOutputID(uint64(i)),
			},
			value: sco.Value,
		})
	}
	return newUplocoinInputs, nil
}

// Transactions will return the transactions that were built up in the graph.
func (tg *TransactionGraph) Transactions() []types.Transaction {
	return tg.transactions
}

// NewTransactionGraph will return a blank transaction graph that is ready for
// use.
func NewTransactionGraph() *TransactionGraph {
	return &TransactionGraph{
		usedUplocoinInputSources: make(map[types.UplocoinOutputID]struct{}),
	}
}
