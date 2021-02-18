package types

// transaction.go defines the transaction type and all of the sub-fields of the
// transaction, as well as providing helper functions for working with
// transactions. The various IDs are designed such that, in a legal blockchain,
// it is cryptographically unlikely that any two objects would share an id.

import (
	"errors"
	"strings"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/encoding"
)

const (
	// UnlockHashChecksumSize is the size of the checksum used to verify
	// human-readable addresses. It is not a crypytographically secure
	// checksum, it's merely intended to prevent typos. 6 is chosen because it
	// brings the total size of the address to 38 bytes, leaving 2 bytes for
	// potential version additions in the future.
	UnlockHashChecksumSize = 6
)

// These Specifiers are used internally when calculating a type's ID. See
// Specifier for more details.
var (
	ErrTransactionIDWrongLen = errors.New("input has wrong length to be an encoded transaction id")

	SpecifierClaimOutput          = NewSpecifier("claim output")
	SpecifierFileContract         = NewSpecifier("file contract")
	SpecifierFileContractRevision = NewSpecifier("file contract re")
	SpecifierMinerFee             = NewSpecifier("miner fee")
	SpecifierMinerPayout          = NewSpecifier("miner payout")
	SpecifierUplocoinInput         = NewSpecifier("Uplocoin input")
	SpecifierUplocoinOutput        = NewSpecifier("Uplocoin output")
	SpecifierUplofundInput         = NewSpecifier("uplofund input")
	SpecifierUplofundOutput        = NewSpecifier("uplofund output")
	SpecifierStorageProofOutput   = NewSpecifier("storage proof")
)

// SpecifierFoundation is used for calculating the OutputID of Foundation
// subsidy outputs. It also serves as the arbitrary data prefix when encoding
// FoundationUnlockHashUpdates.
var SpecifierFoundation = NewSpecifier("foundation")

type (
	// IDs are used to refer to a type without revealing its contents. They
	// are constructed by hashing specific fields of the type, along with a
	// Specifier. While all of these types are hashes, defining type aliases
	// gives us type safety and makes the code more readable.

	// TransactionID uniquely identifies a transaction
	TransactionID crypto.Hash
	// UplocoinOutputID uniquely identifies a Uplocoin output
	UplocoinOutputID crypto.Hash
	// UplofundOutputID uniquely identifies a uplofund output
	UplofundOutputID crypto.Hash
	// FileContractID uniquely identifies a file contract
	FileContractID crypto.Hash
	// OutputID uniquely identifies an output
	OutputID crypto.Hash

	// A Transaction is an atomic component of a block. Transactions can contain
	// inputs and outputs, file contracts, storage proofs, and even arbitrary
	// data. They can also contain signatures to prove that a given party has
	// approved the transaction, or at least a particular subset of it.
	//
	// Transactions can depend on other previous transactions in the same block,
	// but transactions cannot spend outputs that they create or otherwise be
	// self-dependent.
	Transaction struct {
		UplocoinInputs         []UplocoinInput         `json:"Uplocoininputs"`
		UplocoinOutputs        []UplocoinOutput        `json:"Uplocoinoutputs"`
		FileContracts         []FileContract         `json:"filecontracts"`
		FileContractRevisions []FileContractRevision `json:"filecontractrevisions"`
		StorageProofs         []StorageProof         `json:"storageproofs"`
		UplofundInputs         []UplofundInput         `json:"uplofundinputs"`
		UplofundOutputs        []UplofundOutput        `json:"uplofundoutputs"`
		MinerFees             []Currency             `json:"minerfees"`
		ArbitraryData         [][]byte               `json:"arbitrarydata"`
		TransactionSignatures []TransactionSignature `json:"transactionsignatures"`
	}

	// A UplocoinInput consumes a UplocoinOutput and adds the Uplocoins to the set of
	// Uplocoins that can be spent in the transaction. The ParentID points to the
	// output that is getting consumed, and the UnlockConditions contain the rules
	// for spending the output. The UnlockConditions must match the UnlockHash of
	// the output.
	UplocoinInput struct {
		ParentID         UplocoinOutputID  `json:"parentid"`
		UnlockConditions UnlockConditions `json:"unlockconditions"`
	}

	// A UplocoinOutput holds a volume of Uplocoins. Outputs must be spent
	// atomically; that is, they must all be spent in the same transaction. The
	// UnlockHash is the hash of the UnlockConditions that must be fulfilled
	// in order to spend the output.
	UplocoinOutput struct {
		Value      Currency   `json:"value"`
		UnlockHash UnlockHash `json:"unlockhash"`
	}

	// A UplofundInput consumes a UplofundOutput and adds the uplofunds to the set of
	// uplofunds that can be spent in the transaction. The ParentID points to the
	// output that is getting consumed, and the UnlockConditions contain the rules
	// for spending the output. The UnlockConditions must match the UnlockHash of
	// the output.
	UplofundInput struct {
		ParentID         UplofundOutputID  `json:"parentid"`
		UnlockConditions UnlockConditions `json:"unlockconditions"`
		ClaimUnlockHash  UnlockHash       `json:"claimunlockhash"`
	}

	// A UplofundOutput holds a volume of uplofunds. Outputs must be spent
	// atomically; that is, they must all be spent in the same transaction. The
	// UnlockHash is the hash of a set of UnlockConditions that must be fulfilled
	// in order to spend the output.
	//
	// When the UplofundOutput is spent, a UplocoinOutput is created, where:
	//
	//     UplocoinOutput.Value := (UplofundPool - ClaimStart) / 10,000 * Value
	//     UplocoinOutput.UnlockHash := UplofundInput.ClaimUnlockHash
	//
	// When a UplofundOutput is put into a transaction, the ClaimStart must always
	// equal zero. While the transaction is being processed, the ClaimStart is set
	// to the value of the UplofundPool.
	UplofundOutput struct {
		Value      Currency   `json:"value"`
		UnlockHash UnlockHash `json:"unlockhash"`
		ClaimStart Currency   `json:"claimstart"`
	}

	// An UnlockHash is a specially constructed hash of the UnlockConditions type.
	// "Locked" values can be unlocked by providing the UnlockConditions that hash
	// to a given UnlockHash. See UnlockConditions.UnlockHash for details on how the
	// UnlockHash is constructed.
	UnlockHash crypto.Hash
)

// ID returns the id of a transaction, which is taken by marshalling all of the
// fields except for the signatures and taking the hash of the result.
func (t Transaction) ID() TransactionID {
	// Get the transaction id by hashing all data minus the signatures.
	var txid TransactionID
	h := crypto.NewHash()
	t.marshalUploNoSignatures(h)
	h.Sum(txid[:0])
	return txid
}

// RuneToString converts a rune type to a string.
func RuneToString(r rune) string {
	var sb strings.Builder
	sb.WriteRune(r)
	return sb.String()
}

// UplocoinOutputID returns the ID of a Uplocoin output at the given index,
// which is calculated by hashing the concatenation of the UplocoinOutput
// Specifier, all of the fields in the transaction (except the signatures),
// and output index.
func (t Transaction) UplocoinOutputID(i uint64) UplocoinOutputID {
	// Create the id.
	var id UplocoinOutputID
	h := crypto.NewHash()
	h.Write(SpecifierUplocoinOutput[:])
	t.marshalUploNoSignatures(h) // Encode non-signature fields into hash.
	encoding.WriteUint64(h, i)  // Writes index of this output.
	h.Sum(id[:0])
	return id
}

// FileContractID returns the ID of a file contract at the given index, which
// is calculated by hashing the concatenation of the FileContract Specifier,
// all of the fields in the transaction (except the signatures), and the
// contract index.
func (t Transaction) FileContractID(i uint64) FileContractID {
	var id FileContractID
	h := crypto.NewHash()
	h.Write(SpecifierFileContract[:])
	t.marshalUploNoSignatures(h) // Encode non-signature fields into hash.
	encoding.WriteUint64(h, i)  // Writes index of this output.
	h.Sum(id[:0])
	return id
}

// UplofundOutputID returns the ID of a UplofundOutput at the given index, which
// is calculated by hashing the concatenation of the UplofundOutput Specifier,
// all of the fields in the transaction (except the signatures), and output
// index.
func (t Transaction) UplofundOutputID(i uint64) UplofundOutputID {
	var id UplofundOutputID
	h := crypto.NewHash()
	h.Write(SpecifierUplofundOutput[:])
	t.marshalUploNoSignatures(h) // Encode non-signature fields into hash.
	encoding.WriteUint64(h, i)  // Writes index of this output.
	h.Sum(id[:0])
	return id
}

// UplocoinOutputSum returns the sum of all the Uplocoin outputs in the
// transaction, which must match the sum of all the Uplocoin inputs. Uplocoin
// outputs created by storage proofs and uplofund outputs are not considered, as
// they were considered when the contract responsible for funding them was
// created.
func (t Transaction) UplocoinOutputSum() (sum Currency) {
	// Add the Uplocoin outputs.
	for _, sco := range t.UplocoinOutputs {
		sum = sum.Add(sco.Value)
	}

	// Add the file contract payouts.
	for _, fc := range t.FileContracts {
		sum = sum.Add(fc.Payout)
	}

	// Add the miner fees.
	for _, fee := range t.MinerFees {
		sum = sum.Add(fee)
	}

	return
}

// HostSignature returns the host's transaction signature
func (t Transaction) HostSignature() TransactionSignature {
	return t.TransactionSignatures[1]
}

// RenterSignature returns the host's transaction signature
func (t Transaction) RenterSignature() TransactionSignature {
	return t.TransactionSignatures[0]
}

// uploclaimOutputID returns the ID of the UplocoinOutput that is created when
// the uplofund output is spent. The ID is the hash the UplofundOutputID.
func (id UplofundOutputID) UploclaimOutputID() UplocoinOutputID {
	return UplocoinOutputID(crypto.HashObject(id))
}

// A FoundationUnlockHashUpdate directs the consensus set to update its
// Foundation-related UnlockHashes. Updates are submitted to the chain via the
// ArbitraryData field of a transaction.
type FoundationUnlockHashUpdate struct {
	NewPrimary  UnlockHash
	NewFailsafe UnlockHash
}
