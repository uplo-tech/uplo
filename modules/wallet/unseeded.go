package wallet

import (
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

const (
	// UplogFileExtension is the file extension to be used for uplog files
	UplogFileExtension = ".uplokey"

	// UplogFileHeader is the header for all uplog files. Do not change. Because uplog was created
	// early in development, compatibility with uplog requires manually handling
	// the headers and version instead of using the persist package.
	UplogFileHeader = "uplog"

	// UplogFileVersion is the version number to be used for uplog files
	UplogFileVersion = "1.0"
)

var (
	errAllDuplicates         = errors.New("old wallet has no new seeds")
	errDuplicateSpendableKey = errors.New("key has already been loaded into the wallet")

	// ErrInconsistentKeys is the error when keyfiles provided are for different addresses
	ErrInconsistentKeys = errors.New("keyfiles provided that are for different addresses")
	// ErrInsufficientKeys is the error when there's not enough keys provided to spend the uplofunds
	ErrInsufficientKeys = errors.New("not enough keys provided to spend the uplofunds")
	// ErrNoKeyfile is the error when no keyfile has been presented
	ErrNoKeyfile = errors.New("no keyfile has been presented")
	// ErrUnknownHeader is the error when file contains wrong header
	ErrUnknownHeader = errors.New("file contains the wrong header")
	// ErrUnknownVersion is the error when the file has an unknown version number
	ErrUnknownVersion = errors.New("file has an unknown version number")
)

// A uplogKeyPair is the struct representation of the bytes that get saved to
// disk by uplog when a new keyfile is created.
type uplogKeyPair struct {
	Header           string
	Version          string
	Index            int // should be uint64 - too late now
	SecretKey        crypto.SecretKey
	UnlockConditions types.UnlockConditions
}

// savedKey033x is the persist structure that was used to save and load private
// keys in versions v0.3.3.x for uplod.
type savedKey033x struct {
	SecretKey        crypto.SecretKey
	UnlockConditions types.UnlockConditions
	Visible          bool
}

// decryptSpendableKeyFile decrypts a spendableKeyFile, returning a
// spendableKey.
func decryptSpendableKeyFile(masterKey crypto.CipherKey, uk spendableKeyFile) (sk spendableKey, err error) {
	// Verify that the decryption key is correct.
	decryptionKey := saltedEncryptionKey(masterKey, uk.Salt)
	err = verifyEncryption(decryptionKey, uk.EncryptionVerification)
	if err != nil {
		return
	}

	// Decrypt the spendable key and add it to the wallet.
	encodedKey, err := decryptionKey.DecryptBytes(uk.SpendableKey)
	if err != nil {
		return
	}
	err = encoding.Unmarshal(encodedKey, &sk)
	return
}

// integrateSpendableKey loads a spendableKey into the wallet.
func (w *Wallet) integrateSpendableKey(masterKey crypto.CipherKey, sk spendableKey) {
	w.keys[sk.UnlockConditions.UnlockHash()] = sk
}

// loadSpendableKey loads a spendable key into the wallet database.
func (w *Wallet) loadSpendableKey(masterKey crypto.CipherKey, sk spendableKey) error {
	// Duplication is detected by looking at the set of unlock conditions. If
	// the wallet is locked, correct deduplication is uncertain.
	if !w.unlocked {
		return modules.ErrLockedWallet
	}

	// Check for duplicates.
	_, exists := w.keys[sk.UnlockConditions.UnlockHash()]
	if exists {
		return errDuplicateSpendableKey
	}

	// TODO: Check that the key is actually spendable.

	// Create a UID and encryption verification.
	var skf spendableKeyFile
	fastrand.Read(skf.Salt[:])
	encryptionKey := saltedEncryptionKey(masterKey, skf.Salt)
	skf.EncryptionVerification = encryptionKey.EncryptBytes(verificationPlaintext)

	// Encrypt and save the key.
	skf.SpendableKey = encryptionKey.EncryptBytes(encoding.Marshal(sk))

	err := checkMasterKey(w.dbTx, masterKey)
	if err != nil {
		return err
	}
	var current []spendableKeyFile
	err = encoding.Unmarshal(w.dbTx.Bucket(bucketWallet).Get(keySpendableKeyFiles), &current)
	if err != nil {
		return err
	}
	return w.dbTx.Bucket(bucketWallet).Put(keySpendableKeyFiles, encoding.Marshal(append(current, skf)))

	// w.keys[sk.UnlockConditions.UnlockHash()] = sk -> aids with duplicate
	// detection, but causes db inconsistency. Rescanning is probably the
	// solution.
}

// loadUplogKeys loads a set of uplog keyfiles into the wallet, so that the
// wallet may spend the uplofunds.
func (w *Wallet) loadUplogKeys(masterKey crypto.CipherKey, keyfiles []string) error {
	// Load the keyfiles from disk.
	if len(keyfiles) < 1 {
		return ErrNoKeyfile
	}
	skps := make([]uplogKeyPair, len(keyfiles))
	for i, keyfile := range keyfiles {
		err := encoding.ReadFile(keyfile, &skps[i])
		if err != nil {
			return err
		}

		if skps[i].Header != UplogFileHeader {
			return ErrUnknownHeader
		}
		if skps[i].Version != UplogFileVersion {
			return ErrUnknownVersion
		}
	}

	// Check that all of the loaded files have the same address, and that there
	// are enough to create the transaction.
	baseUnlockHash := skps[0].UnlockConditions.UnlockHash()
	for _, skp := range skps {
		if skp.UnlockConditions.UnlockHash() != baseUnlockHash {
			return ErrInconsistentKeys
		}
	}
	if uint64(len(skps)) < skps[0].UnlockConditions.SignaturesRequired {
		return ErrInsufficientKeys
	}
	// Drop all unneeded keys.
	skps = skps[0:skps[0].UnlockConditions.SignaturesRequired]

	// Merge the keys into a single spendableKey and save it to the wallet.
	var sk spendableKey
	sk.UnlockConditions = skps[0].UnlockConditions
	for _, skp := range skps {
		sk.SecretKeys = append(sk.SecretKeys, skp.SecretKey)
	}
	err := w.loadSpendableKey(masterKey, sk)
	if err != nil {
		return err
	}
	w.integrateSpendableKey(masterKey, sk)
	return nil
}

// LoadUplogKeys loads a set of uplog-generated keys into the wallet.
func (w *Wallet) LoadUplogKeys(masterKey crypto.CipherKey, keyfiles []string) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	// load the keys and reset the consensus change ID and height in preparation for rescan
	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		err := w.loadUplogKeys(masterKey, keyfiles)
		if err != nil {
			return err
		}

		if err = w.dbTx.DeleteBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		if _, err = w.dbTx.CreateBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		w.unconfirmedProcessedTransactions = nil
		err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
		if err != nil {
			return err
		}
		return dbPutConsensusHeight(w.dbTx, 0)
	}()
	if err != nil {
		return err
	}

	// rescan the blockchain
	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	done := make(chan struct{})
	go w.rescanMessage(done)
	defer close(done)

	err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
	if err != nil {
		return err
	}
	w.tpool.TransactionPoolSubscribe(w)
	return nil
}

// Load033xWallet loads a v0.3.3.x wallet as an unseeded key, such that the
// funds become spendable to the current wallet.
func (w *Wallet) Load033xWallet(masterKey crypto.CipherKey, filepath033x string) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	// load the keys and reset the consensus change ID and height in preparation for rescan
	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		var savedKeys []savedKey033x
		err := encoding.ReadFile(filepath033x, &savedKeys)
		if err != nil {
			return err
		}
		var seedsLoaded int
		for _, savedKey := range savedKeys {
			spendKey := spendableKey{
				UnlockConditions: savedKey.UnlockConditions,
				SecretKeys:       []crypto.SecretKey{savedKey.SecretKey},
			}
			err = w.loadSpendableKey(masterKey, spendKey)
			if err != nil && !errors.Contains(err, errDuplicateSpendableKey) {
				return err
			}
			if err == nil {
				seedsLoaded++
			}
			w.integrateSpendableKey(masterKey, spendKey)
		}
		if seedsLoaded == 0 {
			return errAllDuplicates
		}

		if err = w.dbTx.DeleteBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		if _, err = w.dbTx.CreateBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		w.unconfirmedProcessedTransactions = nil
		err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
		if err != nil {
			return err
		}
		return dbPutConsensusHeight(w.dbTx, 0)
	}()
	if err != nil {
		return err
	}

	// rescan the blockchain
	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	done := make(chan struct{})
	go w.rescanMessage(done)
	defer close(done)

	err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
	if err != nil {
		return err
	}
	w.tpool.TransactionPoolSubscribe(w)

	return nil
}
