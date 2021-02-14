package crypto

import (
	"bytes"
	"testing"

	"github.com/uplo-tech/fastrand"
)

// TestXChaCha20Encryption checks that encryption and decryption works correctly.
func TestXChaCha20Encryption(t *testing.T) {
	key := generateXChaCha20CipherKey()

	// Encrypt and decrypt a zero plaintext, and compare the decrypted to the
	// original.
	plaintext := make([]byte, 600)
	ciphertext := key.EncryptBytes(plaintext)
	decryptedPlaintext, err := key.DecryptBytes(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, decryptedPlaintext) {
		t.Fatal("Encrypted and decrypted zero plaintext do not match")
	}

	// Try again with a nonzero plaintext.
	plaintext = fastrand.Bytes(600)
	ciphertext = key.EncryptBytes(plaintext)

	// Multiple encryptions should return the same ciphertext.
	for i := 0; i < 3; i++ {
		newCipherText := key.EncryptBytes(plaintext)
		if !bytes.Equal(ciphertext, newCipherText) {
			t.Fatal("Multiple encryptions of non-zero values doesn't match")
		}
	}

	// Multiple decryptions should return the same plaintext.
	for i := 0; i < 3; i++ {
		decryptedPlaintext, err = key.DecryptBytes(ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plaintext, decryptedPlaintext) {
			t.Fatal("Encrypted and decrypted non-zero plaintext do not match")
		}
	}

	// Try to trigger a panic or error with nil values.
	key.EncryptBytes(nil)
	_, err = key.DecryptBytes(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Check that derive does not fail
	key.Derive(fastrand.Uint64n(1<<63), fastrand.Uint64n(1<<63))
}

// TestXChaCha20DecryptInPlace checks that decrypt in place works as expected.
func TestXChaCha20DecryptInPlace(t *testing.T) {
	key := generateXChaCha20CipherKey()

	plaintext := fastrand.Bytes(4096)
	ciphertext := key.EncryptBytes(plaintext)
	ciphertextCopy := make([]byte, 4096)
	copy(ciphertextCopy, ciphertext)

	decryptedCiphertext, err := key.DecryptBytes(ciphertextCopy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decryptedCiphertext, plaintext) {
		t.Fatal("decryptedCiphertext should equal plaintext")
	}

	// Partially decrypt the ciphertext in place.
	// Choose a random, non-zero block index in the first half of the plaintext.
	var blockIndex uint64
	for blockIndex == 0 {
		blockIndex = fastrand.Uint64n(2048 / 64)
	}
	partiallyDecryptedCiphertext, err := key.DecryptBytesInPlace(ciphertext, blockIndex)
	if err != nil {
		t.Fatal(err)
	}

	// Check that what should be partially decrypted is actually only partially
	// decrypted.
	sliceIndex := blockIndex * 64
	if bytes.Equal(decryptedCiphertext[:sliceIndex], partiallyDecryptedCiphertext[:sliceIndex]) {
		t.Fatal("Partially decrypted plaintext prefix should not match")
	}
	if bytes.Equal(decryptedCiphertext[sliceIndex:], partiallyDecryptedCiphertext[sliceIndex:]) {
		t.Fatal("Partially decrypted plaintext suffix should match")
	}
}
