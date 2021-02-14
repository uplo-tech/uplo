package types

import (
	"bytes"
	"testing"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// TestEd25519PublicKey tests the Ed25519PublicKey function.
func TestEd25519PublicKey(t *testing.T) {
	_, pk := crypto.GenerateKeyPair()
	spk := Ed25519PublicKey(pk)
	if spk.Algorithm != SignatureEd25519 {
		t.Error("Ed25519PublicKey created key with wrong algorithm specifier:", spk.Algorithm)
	}
	if !bytes.Equal(spk.Key, pk[:]) {
		t.Error("Ed25519PublicKey created key with wrong data")
	}
}

// TestUploPublicKeyEquals tests the functionality of the implementation of
// Equals on a UploPublicKey
func TestUploPublicKeyEquals(t *testing.T) {
	var x, y UploPublicKey
	key := func() []byte { return fastrand.Bytes(32) }
	spk := func(algo Specifier, key []byte) UploPublicKey {
		return UploPublicKey{
			Algorithm: algo,
			Key:       key,
		}
	}

	// same algorithm, same key
	x = spk(SignatureEd25519, key())
	y = spk(SignatureEd25519, x.Key)
	if !x.Equals(y) || !y.Equals(x) {
		t.Fatal("Expected keys to be equal")
	}

	// same algorithm, different key
	x = spk(SignatureEd25519, key())
	y = spk(SignatureEd25519, key())
	if x.Equals(y) || y.Equals(x) {
		t.Fatal("Expected keys not to be equal")
	}

	// different algorithm, same key
	x = spk(SignatureEd25519, key())
	y = spk(SignatureEntropy, x.Key)
	if x.Equals(y) || y.Equals(x) {
		t.Fatal("Expected keys not to be equal")
	}

	// different algorithm, different key
	x = spk(SignatureEd25519, key())
	y = spk(SignatureEntropy, key())
	if x.Equals(y) || y.Equals(x) {
		t.Fatal("Expected keys not to be equal")
	}
}

// TestUploPublicKeyToPublicKey tests the functionality of
// 'ToPublicKey' on the UploPublicKey object
func TestUploPublicKeyToPublicKey(t *testing.T) {
	// randomSPK returns a random UploPublicKey with specified key length
	randomSPK := func(keyLength int) UploPublicKey {
		return UploPublicKey{
			Algorithm: SignatureEd25519,
			Key:       fastrand.Bytes(keyLength),
		}
	}

	// verify ToPublicKey for a correct key length
	spk := randomSPK(crypto.PublicKeySize)
	cpk := spk.ToPublicKey()
	if !bytes.Equal(cpk[:], spk.Key[:]) {
		t.Fatal("Expected crytpo.PublicKey to equal the Key in UploPublicKey")
	}

	// verify the build.Critical for an incorrect key length
	spk = randomSPK(crypto.PublicKeySize * 2)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Converting a UploPublicKey, with an incorrect key length, to a crypto.PublicKey did not cause a panic")
		}
	}()
	_ = spk.ToPublicKey()
}

// TestUnlockHash runs the UnlockHash code.
func TestUnlockHash(t *testing.T) {
	uc := UnlockConditions{
		Timelock: 1,
		PublicKeys: []UploPublicKey{
			{
				Algorithm: SignatureEntropy,
				Key:       []byte{'f', 'a', 'k', 'e'},
			},
		},
		SignaturesRequired: 3,
	}

	_ = uc.UnlockHash()
}

// TestSigHash runs the SigHash function of the transaction type.
func TestSigHash(t *testing.T) {
	txn := Transaction{
		UplocoinInputs:         []UplocoinInput{{}},
		UplocoinOutputs:        []UplocoinOutput{{}},
		FileContracts:         []FileContract{{}},
		FileContractRevisions: []FileContractRevision{{}},
		StorageProofs:         []StorageProof{{}},
		UplofundInputs:         []UplofundInput{{}},
		UplofundOutputs:        []UplofundOutput{{}},
		MinerFees:             []Currency{{}},
		ArbitraryData:         [][]byte{{'o'}, {'t'}},
		TransactionSignatures: []TransactionSignature{
			{
				CoveredFields: CoveredFields{
					WholeTransaction: true,
				},
			},
			{
				CoveredFields: CoveredFields{
					UplocoinInputs:         []uint64{0},
					UplocoinOutputs:        []uint64{0},
					FileContracts:         []uint64{0},
					FileContractRevisions: []uint64{0},
					StorageProofs:         []uint64{0},
					UplofundInputs:         []uint64{0},
					UplofundOutputs:        []uint64{0},
					MinerFees:             []uint64{0},
					ArbitraryData:         []uint64{0},
					TransactionSignatures: []uint64{0},
				},
			},
		},
	}

	_ = txn.SigHash(0, 0)
	_ = txn.SigHash(1, 0)
}

// TestSortedUnique probes the sortedUnique function.
func TestSortedUnique(t *testing.T) {
	su := []uint64{3, 5, 6, 8, 12}
	if !sortedUnique(su, 13) {
		t.Error("sortedUnique rejected a valid array")
	}
	if sortedUnique(su, 12) {
		t.Error("sortedUnique accepted an invalid max")
	}
	if sortedUnique(su, 11) {
		t.Error("sortedUnique accepted an invalid max")
	}

	unsorted := []uint64{3, 5, 3}
	if sortedUnique(unsorted, 6) {
		t.Error("sortedUnique accepted an unsorted array")
	}

	repeats := []uint64{2, 4, 4, 7}
	if sortedUnique(repeats, 8) {
		t.Error("sortedUnique accepted an array with repeats")
	}

	bothFlaws := []uint64{2, 3, 4, 5, 6, 6, 4}
	if sortedUnique(bothFlaws, 7) {
		t.Error("Sorted unique accetped array with multiple flaws")
	}
}

// TestTransactionValidCoveredFields probes the validCoveredFields method of
// the transaction type.
func TestTransactionValidCoveredFields(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Create a transaction with all fields filled in minimally. The first
	// check has a legal CoveredFields object with 'WholeTransaction' set.
	txn := Transaction{
		UplocoinInputs:         []UplocoinInput{{}},
		UplocoinOutputs:        []UplocoinOutput{{}},
		FileContracts:         []FileContract{{}},
		FileContractRevisions: []FileContractRevision{{}},
		StorageProofs:         []StorageProof{{}},
		UplofundInputs:         []UplofundInput{{}},
		UplofundOutputs:        []UplofundOutput{{}},
		MinerFees:             []Currency{{}},
		ArbitraryData:         [][]byte{{'o'}, {'t'}},
		TransactionSignatures: []TransactionSignature{{
			CoveredFields: CoveredFields{WholeTransaction: true},
		}},
	}
	err := txn.validCoveredFields()
	if err != nil {
		t.Error(err)
	}

	// Second check has CoveredFields object where 'WholeTransaction' is not
	// set.
	txn.TransactionSignatures = append(txn.TransactionSignatures, TransactionSignature{
		CoveredFields: CoveredFields{
			UplocoinOutputs:        []uint64{0},
			MinerFees:             []uint64{0},
			ArbitraryData:         []uint64{0},
			FileContractRevisions: []uint64{0},
		},
	})
	err = txn.validCoveredFields()
	if err != nil {
		t.Error(err)
	}

	// Add signature coverage to the first signature. This should not violate
	// any rules.
	txn.TransactionSignatures[0].CoveredFields.TransactionSignatures = []uint64{1}
	err = txn.validCoveredFields()
	if err != nil {
		t.Error(err)
	}

	// Add Uplocoin output coverage to the first signature. This should violate
	// rules, as the fields are not allowed to be set when 'WholeTransaction'
	// is set.
	txn.TransactionSignatures[0].CoveredFields.UplocoinOutputs = []uint64{0}
	err = txn.validCoveredFields()
	if !errors.Contains(err, ErrWholeTransactionViolation) {
		t.Error("Expecting ErrWholeTransactionViolation, got", err)
	}

	// Create a SortedUnique violation instead of a WholeTransactionViolation.
	txn.TransactionSignatures[0].CoveredFields.UplocoinOutputs = nil
	txn.TransactionSignatures[0].CoveredFields.TransactionSignatures = []uint64{1, 2}
	err = txn.validCoveredFields()
	if !errors.Contains(err, ErrSortedUniqueViolation) {
		t.Error("Expecting ErrSortedUniqueViolation, got", err)
	}

	// Clear the CoveredFields completely.
	txn.TransactionSignatures[0].CoveredFields = CoveredFields{}
	err = txn.validCoveredFields()
	if !errors.Contains(err, ErrWholeTransactionViolation) {
		t.Error("Expecting ErrWholeTransactionViolation, got", err)
	}
}

// TestTransactionValidSignatures probes the validSignatures method of the
// Transaction type.
func TestTransactionValidSignatures(t *testing.T) {
	// Create keys for use in signing and verifying.
	sk, pk := crypto.GenerateKeyPair()

	// Create UnlockConditions with 3 keys, 2 of which are required. The first
	// possible key is a standard signature. The second key is an unknown
	// signature type, which should always be accepted. The final type is an
	// entropy type, which should never be accepted.
	uc := UnlockConditions{
		PublicKeys: []UploPublicKey{
			{Algorithm: SignatureEd25519, Key: pk[:]},
			{},
			{Algorithm: SignatureEntropy},
		},
		SignaturesRequired: 2,
	}

	// Create a transaction with each type of unlock condition.
	txn := Transaction{
		UplocoinInputs: []UplocoinInput{
			{UnlockConditions: uc},
		},
		FileContractRevisions: []FileContractRevision{
			{UnlockConditions: uc},
		},
		UplofundInputs: []UplofundInput{
			{UnlockConditions: uc},
		},
	}
	txn.FileContractRevisions[0].ParentID[0] = 1 // can't overlap with other objects
	txn.UplofundInputs[0].ParentID[0] = 2         // can't overlap with other objects

	// Create the signatures that spend the output.
	txn.TransactionSignatures = []TransactionSignature{
		// First signatures use cryptography.
		{
			Timelock:      5,
			CoveredFields: CoveredFields{WholeTransaction: true},
		},
		{
			CoveredFields: CoveredFields{WholeTransaction: true},
		},
		{
			CoveredFields: CoveredFields{WholeTransaction: true},
		},

		// The second signatures should always work for being unrecognized
		// types.
		{PublicKeyIndex: 1, CoveredFields: CoveredFields{WholeTransaction: true}},
		{PublicKeyIndex: 1, CoveredFields: CoveredFields{WholeTransaction: true}},
		{PublicKeyIndex: 1, CoveredFields: CoveredFields{WholeTransaction: true}},
	}
	txn.TransactionSignatures[1].ParentID[0] = 1
	txn.TransactionSignatures[2].ParentID[0] = 2
	txn.TransactionSignatures[4].ParentID[0] = 1
	txn.TransactionSignatures[5].ParentID[0] = 2
	sigHash0 := txn.SigHash(0, 10)
	sigHash1 := txn.SigHash(1, 10)
	sigHash2 := txn.SigHash(2, 10)
	sig0 := crypto.SignHash(sigHash0, sk)
	sig1 := crypto.SignHash(sigHash1, sk)
	sig2 := crypto.SignHash(sigHash2, sk)
	txn.TransactionSignatures[0].Signature = sig0[:]
	txn.TransactionSignatures[1].Signature = sig1[:]
	txn.TransactionSignatures[2].Signature = sig2[:]

	// Check that the signing was successful.
	err := txn.validSignatures(10)
	if err != nil {
		t.Error(err)
	}

	// Corrupt one of the signatures.
	sig0[0]++
	txn.TransactionSignatures[0].Signature = sig0[:]
	err = txn.validSignatures(10)
	if err == nil {
		t.Error("Corrupted a signature but the txn was still accepted as valid!")
	}
	sig0[0]--
	txn.TransactionSignatures[0].Signature = sig0[:]

	// Fail the validCoveredFields check.
	txn.TransactionSignatures[0].CoveredFields.UplocoinInputs = []uint64{33}
	err = txn.validSignatures(10)
	if err == nil {
		t.Error("failed to flunk the validCoveredFields check")
	}
	txn.TransactionSignatures[0].CoveredFields.UplocoinInputs = nil

	// Double spend a UplocoinInput, FileContractTermination, and UplofundInput.
	txn.UplocoinInputs = append(txn.UplocoinInputs, UplocoinInput{UnlockConditions: UnlockConditions{}})
	err = txn.validSignatures(10)
	if err == nil {
		t.Error("failed to double spend a Uplocoin input")
	}
	txn.UplocoinInputs = txn.UplocoinInputs[:len(txn.UplocoinInputs)-1]
	txn.FileContractRevisions = append(txn.FileContractRevisions, FileContractRevision{UnlockConditions: UnlockConditions{}})
	err = txn.validSignatures(10)
	if err == nil {
		t.Error("failed to double spend a file contract termination")
	}
	txn.FileContractRevisions = txn.FileContractRevisions[:len(txn.FileContractRevisions)-1]
	txn.UplofundInputs = append(txn.UplofundInputs, UplofundInput{UnlockConditions: UnlockConditions{}})
	err = txn.validSignatures(10)
	if err == nil {
		t.Error("failed to double spend a uplofund input")
	}
	txn.UplofundInputs = txn.UplofundInputs[:len(txn.UplofundInputs)-1]

	// Add a frivolous signature
	txn.TransactionSignatures = append(txn.TransactionSignatures, TransactionSignature{CoveredFields: CoveredFields{WholeTransaction: true}})
	err = txn.validSignatures(10)
	if !errors.Contains(err, ErrFrivolousSignature) {
		t.Error(err)
	}
	txn.TransactionSignatures = txn.TransactionSignatures[:len(txn.TransactionSignatures)-1]

	// Replace one of the cryptography signatures with an always-accepted
	// signature. This should get rejected because the always-accepted
	// signature has already been used.
	tmpTxn0 := txn.TransactionSignatures[0]
	txn.TransactionSignatures[0] = TransactionSignature{PublicKeyIndex: 1, CoveredFields: CoveredFields{WholeTransaction: true}}
	err = txn.validSignatures(10)
	if !errors.Contains(err, ErrPublicKeyOveruse) {
		t.Error(err)
	}
	txn.TransactionSignatures[0] = tmpTxn0

	// Fail the timelock check for signatures.
	err = txn.validSignatures(4)
	if !errors.Contains(err, ErrPrematureSignature) {
		t.Error(err)
	}

	// Try to spend an entropy signature.
	txn.TransactionSignatures[0] = TransactionSignature{PublicKeyIndex: 2, CoveredFields: CoveredFields{WholeTransaction: true}}
	err = txn.validSignatures(10)
	if !errors.Contains(err, ErrEntropyKey) {
		t.Error(err)
	}
	txn.TransactionSignatures[0] = tmpTxn0

	// Try to point to a nonexistent public key.
	txn.TransactionSignatures[0] = TransactionSignature{PublicKeyIndex: 5, CoveredFields: CoveredFields{WholeTransaction: true}}
	err = txn.validSignatures(10)
	if !errors.Contains(err, ErrInvalidPubKeyIndex) {
		t.Error(err)
	}
	txn.TransactionSignatures[0] = tmpTxn0

	// Insert a malformed public key into the transaction.
	txn.UplocoinInputs[0].UnlockConditions.PublicKeys[0].Key = []byte{'b', 'a', 'd'}
	err = txn.validSignatures(10)
	if err == nil {
		t.Error(err)
	}
	txn.UplocoinInputs[0].UnlockConditions.PublicKeys[0].Key = pk[:]

	// Insert a malformed signature into the transaction.
	txn.TransactionSignatures[0].Signature = []byte{'m', 'a', 'l'}
	err = txn.validSignatures(10)
	if err == nil {
		t.Error(err)
	}
	txn.TransactionSignatures[0] = tmpTxn0

	// Try to spend a transaction when not every required signature is
	// available.
	txn.TransactionSignatures = txn.TransactionSignatures[1:]
	err = txn.validSignatures(10)
	if !errors.Contains(err, ErrMissingSignatures) {
		t.Error(err)
	}
}
