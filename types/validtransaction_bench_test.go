package types

import (
	"testing"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/encoding"
)

// BenchmarkStandaloneValid times how long it takes to verify a single
// large transaction, with a certain number of signatures
func BenchmarkStandaloneValid(b *testing.B) {
	numSigs := 7
	// make a transaction numSigs with valid inputs with valid signatures
	b.ReportAllocs()
	txn := Transaction{}
	sk := make([]crypto.SecretKey, numSigs)
	pk := make([]crypto.PublicKey, numSigs)
	for i := 0; i < numSigs; i++ {
		s, p := crypto.GenerateKeyPair()
		sk[i] = s
		pk[i] = p

		uc := UnlockConditions{
			PublicKeys: []UploPublicKey{
				{Algorithm: SignatureEd25519, Key: pk[i][:]},
			},
			SignaturesRequired: 1,
		}
		txn.UplocoinInputs = append(txn.UplocoinInputs, UplocoinInput{
			UnlockConditions: uc,
		})
		copy(txn.UplocoinInputs[i].ParentID[:], encoding.Marshal(i))
		txn.TransactionSignatures = append(txn.TransactionSignatures, TransactionSignature{
			CoveredFields: CoveredFields{WholeTransaction: true},
		})
		copy(txn.TransactionSignatures[i].ParentID[:], encoding.Marshal(i))
	}
	// Transaction must be constructed before signing
	for i := 0; i < numSigs; i++ {
		sigHash := txn.SigHash(i, 0)
		sig0 := crypto.SignHash(sigHash, sk[i])
		txn.TransactionSignatures[i].Signature = sig0[:]
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := txn.StandaloneValid(10)
		if err != nil {
			b.Fatal(err)
		}
	}
}
