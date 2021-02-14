package wallet

import (
	"bytes"
	"fmt"
	"time"

	"github.com/uplo-tech/bolt"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
	"golang.org/x/crypto/pbkdf2"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

var (
	errAlreadyUnlocked   = errors.New("wallet has already been unlocked")
	errReencrypt         = errors.New("wallet is already encrypted, cannot encrypt again")
	errScanInProgress    = errors.New("another wallet rescan is already underway")
	errUnencryptedWallet = errors.New("wallet has not been encrypted yet")

	// verificationPlaintext is the plaintext used to verify encryption keys.
	// By storing the corresponding ciphertext for a given key, we can later
	// verify that a key is correct by using it to decrypt the ciphertext and
	// comparing the result to verificationPlaintext.
	verificationPlaintext = make([]byte, 32)
)

// saltedEncryptionKey creates an encryption key that is used to decrypt a
// specific key file.
func saltedEncryptionKey(masterKey crypto.CipherKey, salt walletSalt) (key crypto.CipherKey) {
	key = crypto.NewWalletKey(crypto.HashAll(masterKey, salt))
	return
}

// walletPasswordEncryptionKey creates an encryption key that is used to
// encrypt/decrypt the master encryption key.
func walletPasswordEncryptionKey(seed modules.Seed, salt walletSalt) (key crypto.CipherKey) {
	var h crypto.Hash
	entropy := pbkdf2.Key(seed[:], salt[:], 10000, crypto.HashSize, crypto.NewHash)
	copy(h[:], entropy)
	key = crypto.NewWalletKey(h)
	return
}

// verifyEncryption verifies that key properly decrypts the ciphertext to a
// preset plaintext.
func verifyEncryption(key crypto.CipherKey, encrypted crypto.Ciphertext) error {
	verification, err := key.DecryptBytes(encrypted)
	if err != nil {
		contextErr := errors.AddContext(modules.ErrBadEncryptionKey, "failed to decrypt key")
		return errors.Compose(err, contextErr)
	}
	if !bytes.Equal(verificationPlaintext, verification) {
		return modules.ErrBadEncryptionKey
	}
	return nil
}

// checkMasterKey verifies that the masterKey is the key used to encrypt the
// wallet.
func checkMasterKey(tx *bolt.Tx, masterKey crypto.CipherKey) error {
	if masterKey == nil {
		return modules.ErrBadEncryptionKey
	}
	uk := saltedEncryptionKey(masterKey, dbGetWalletSalt(tx))
	encryptedVerification := tx.Bucket(bucketWallet).Get(keyEncryptionVerification)
	return verifyEncryption(uk, encryptedVerification)
}

// initEncryption initializes and encrypts the primary SeedFile.
func (w *Wallet) initEncryption(masterKey crypto.CipherKey, seed modules.Seed, progress uint64) (modules.Seed, error) {
	wb := w.dbTx.Bucket(bucketWallet)
	// Check if the wallet encryption key has already been set.
	if wb.Get(keyEncryptionVerification) != nil {
		return modules.Seed{}, errReencrypt
	}

	// create a seedFile for the seed
	sf := createSeedFile(masterKey, seed)

	// set this as the primary seedFile
	err := wb.Put(keyPrimarySeedFile, encoding.Marshal(sf))
	if err != nil {
		return modules.Seed{}, err
	}
	err = wb.Put(keyPrimarySeedProgress, encoding.Marshal(progress))
	if err != nil {
		return modules.Seed{}, err
	}

	// Establish the encryption verification using the masterKey. After this
	// point, the wallet is encrypted.
	uk := saltedEncryptionKey(masterKey, dbGetWalletSalt(w.dbTx))
	err = wb.Put(keyEncryptionVerification, uk.EncryptBytes(verificationPlaintext))
	if err != nil {
		return modules.Seed{}, err
	}

	// Encrypt the masterkey using the seed to allow for a masterkey recovery using
	// the seed.
	wpk := walletPasswordEncryptionKey(seed, dbGetWalletSalt(w.dbTx))
	mkt := masterKey.Type()
	mke := masterKey.Key()
	err = wb.Put(keyWalletPassword, wpk.EncryptBytes(append(mkt[:], mke...)))
	if err != nil {
		return modules.Seed{}, err
	}

	// on future startups, this field will be set by w.initPersist
	w.encrypted = true

	return seed, nil
}

// managedMasterKey retrieves the masterkey that was stored encrypted in the
// wallet's database.
func (w *Wallet) managedMasterKey(seed modules.Seed) (crypto.CipherKey, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Check if wallet is encrypted.
	if !w.encrypted {
		return nil, errUnencryptedWallet
	}
	// Compute password from seed.
	wb := w.dbTx.Bucket(bucketWallet)
	wpk := walletPasswordEncryptionKey(seed, dbGetWalletSalt(w.dbTx))
	// Grab the encrypted masterkey.
	encryptedMK := wb.Get(keyWalletPassword)
	if len(encryptedMK) == 0 {
		return nil, errors.New("wallet is encrypted but masterkey is missing")
	}
	// Decrypt the masterkey.
	masterKey, err := wpk.DecryptBytes(encryptedMK)
	if err != nil {
		return nil, errors.AddContext(err, "failed to decrypt masterkey")
	}
	var ct crypto.CipherType
	copy(ct[:], masterKey)
	return crypto.NewUploKey(ct, masterKey[len(ct):])
}

// managedUnlock loads all of the encrypted file structures into wallet memory. Even
// after loading, the structures are kept encrypted, but some data such as
// addresses are decrypted so that the wallet knows what to track.
func (w *Wallet) managedUnlock(masterKey crypto.CipherKey) <-chan error {
	errChan := make(chan error, 1)

	// Blocking unlock
	lastChange, err := w.managedBlockingUnlock(masterKey)
	if err != nil {
		errChan <- err
		return errChan
	}

	// non-blocking unlock
	go func() {
		defer close(errChan)
		if err := w.tg.Add(); err != nil {
			errChan <- err
			return
		}
		defer w.tg.Done()
		if w.deps.Disrupt("DisableAsyncUnlock") {
			return
		}
		err := w.managedAsyncUnlock(lastChange)
		if err != nil {
			errChan <- err
		}
	}()
	return errChan
}

// managedBlockingUnlock handles the blocking part of hte managedUnlock method.
func (w *Wallet) managedBlockingUnlock(masterKey crypto.CipherKey) (modules.ConsensusChangeID, error) {
	w.mu.RLock()
	unlocked := w.unlocked
	encrypted := w.encrypted
	w.mu.RUnlock()
	if unlocked {
		return modules.ConsensusChangeID{}, errAlreadyUnlocked
	} else if !encrypted {
		return modules.ConsensusChangeID{}, errUnencryptedWallet
	}

	// Load db objects into memory.
	var lastChange modules.ConsensusChangeID
	var primarySeedFile seedFile
	var primarySeedProgress uint64
	var auxiliarySeedFiles []seedFile
	var unseededKeyFiles []spendableKeyFile
	var watchedAddrs []types.UnlockHash
	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		// verify masterKey
		err := checkMasterKey(w.dbTx, masterKey)
		if err != nil {
			return err
		}

		// lastChange
		lastChange = dbGetConsensusChangeID(w.dbTx)

		// primarySeedFile + primarySeedProgress
		wb := w.dbTx.Bucket(bucketWallet)
		err = encoding.Unmarshal(wb.Get(keyPrimarySeedFile), &primarySeedFile)
		if err != nil {
			return err
		}
		err = encoding.Unmarshal(wb.Get(keyPrimarySeedProgress), &primarySeedProgress)
		if err != nil {
			return err
		}

		// auxiliarySeedFiles
		err = encoding.Unmarshal(wb.Get(keyAuxiliarySeedFiles), &auxiliarySeedFiles)
		if err != nil {
			return err
		}

		// unseededKeyFiles
		err = encoding.Unmarshal(wb.Get(keySpendableKeyFiles), &unseededKeyFiles)
		if err != nil {
			return err
		}

		// watchedAddrs
		err = encoding.Unmarshal(wb.Get(keyWatchedAddrs), &watchedAddrs)
		if err != nil {
			return err
		}

		return nil
	}()
	if err != nil {
		return modules.ConsensusChangeID{}, err
	}

	// Decrypt + load keys.
	err = func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		// primarySeedFile
		primarySeed, err := decryptSeedFile(masterKey, primarySeedFile)
		if err != nil {
			return err
		}
		w.integrateSeed(primarySeed, primarySeedProgress)
		w.primarySeed = primarySeed
		w.regenerateLookahead(primarySeedProgress)

		// auxiliarySeedFiles
		for _, sf := range auxiliarySeedFiles {
			auxSeed, err := decryptSeedFile(masterKey, sf)
			if err != nil {
				return err
			}
			w.integrateSeed(auxSeed, modules.PublicKeysPerSeed)
			w.seeds = append(w.seeds, auxSeed)
		}

		// unseededKeyFiles
		for _, uk := range unseededKeyFiles {
			sk, err := decryptSpendableKeyFile(masterKey, uk)
			if err != nil {
				return err
			}
			w.integrateSpendableKey(masterKey, sk)
		}

		// watchedAddrs
		for _, addr := range watchedAddrs {
			w.watchedAddrs[addr] = struct{}{}
		}

		// COMPATv141 if the wallet password hasn't been encrypted yet using the seed,
		// do it.
		wpk := walletPasswordEncryptionKey(primarySeed, dbGetWalletSalt(w.dbTx))
		mkt := masterKey.Type()
		mke := masterKey.Key()
		wb := w.dbTx.Bucket(bucketWallet)
		if len(wb.Get(keyWalletPassword)) == 0 {
			return w.dbTx.Bucket(bucketWallet).Put(keyWalletPassword, wpk.EncryptBytes(append(mkt[:], mke...)))
		}
		return nil
	}()
	if err != nil {
		return modules.ConsensusChangeID{}, err
	}

	w.mu.Lock()
	w.unlocked = true
	w.mu.Unlock()
	return lastChange, nil
}

// managedAsyncUnlock handles the async part of hte managedUnlock method.
func (w *Wallet) managedAsyncUnlock(lastChange modules.ConsensusChangeID) error {
	// Subscribe to the consensus set if this is the first unlock for the
	// wallet object.
	w.subscribedMu.Lock()
	defer w.subscribedMu.Unlock()
	if !w.subscribed {
		// Subscription can take a while, so spawn a goroutine to print the
		// wallet height every few seconds. (If subscription completes
		// quickly, nothing will be printed.)
		done := make(chan struct{})
		go w.rescanMessage(done)
		defer close(done)

		err := w.cs.ConsensusSetSubscribe(w, lastChange, w.tg.StopChan())
		if errors.Contains(err, modules.ErrInvalidConsensusChangeID) {
			// something went wrong; resubscribe from the beginning
			err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
			if err != nil {
				return fmt.Errorf("failed to reset db during rescan: %v", err)
			}
			err = dbPutConsensusHeight(w.dbTx, 0)
			if err != nil {
				return fmt.Errorf("failed to reset db during rescan: %v", err)
			}
			err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
		}
		if err != nil {
			return fmt.Errorf("wallet subscription failed: %v", err)
		}
		w.tpool.TransactionPoolSubscribe(w)
	}
	w.subscribed = true
	return nil
}

// rescanMessage prints the blockheight every 3 seconds until done is closed.
func (w *Wallet) rescanMessage(done chan struct{}) {
	if build.Release == "testing" {
		return
	}

	// sleep first because we may not need to print a message at all if
	// done is closed quickly.
	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
	}

	for {
		w.mu.Lock()
		height, _ := dbGetConsensusHeight(w.dbTx)
		w.mu.Unlock()
		fmt.Printf("\rWallet: scanned to height %d...", height)

		select {
		case <-done:
			fmt.Println("\nDone!")
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// wipeSecrets erases all of the seeds and secret keys in the wallet.
func (w *Wallet) wipeSecrets() {
	// 'for i := range' must be used to prevent copies of secret data from
	// being made.
	for i := range w.keys {
		for j := range w.keys[i].SecretKeys {
			crypto.SecureWipe(w.keys[i].SecretKeys[j][:])
		}
	}
	for i := range w.seeds {
		crypto.SecureWipe(w.seeds[i][:])
	}
	crypto.SecureWipe(w.primarySeed[:])
	w.seeds = w.seeds[:0]
}

// Encrypted returns whether or not the wallet has been encrypted.
func (w *Wallet) Encrypted() (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()
	if build.DEBUG && w.unlocked && !w.encrypted {
		panic("wallet is both unlocked and unencrypted")
	}
	return w.encrypted, nil
}

// Encrypt will create a primary seed for the wallet and encrypt it using
// masterKey. If masterKey is blank, then the hash of the primary seed will be
// used instead. The wallet will still be locked after Encrypt is called.
//
// Encrypt can only be called once throughout the life of the wallet, and will
// return an error on subsequent calls (even after restarting the wallet). To
// reset the wallet, the wallet files must be moved to a different directory
// or deleted.
func (w *Wallet) Encrypt(masterKey crypto.CipherKey) (modules.Seed, error) {
	if err := w.tg.Add(); err != nil {
		return modules.Seed{}, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()

	// Create a random seed.
	var seed modules.Seed
	fastrand.Read(seed[:])

	// If masterKey is blank, use the hash of the seed.
	if masterKey == nil {
		masterKey = crypto.NewWalletKey(crypto.HashObject(seed))
	}
	// Initial seed progress is 0.
	return w.initEncryption(masterKey, seed, 0)
}

// Reset will reset the wallet, clearing the database and returning it to
// the unencrypted state.
func (w *Wallet) Reset() error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	// Unsubscribe if we are currently subscribed.
	w.subscribedMu.Lock()
	if w.subscribed {
		w.cs.Unsubscribe(w)
		w.tpool.Unsubscribe(w)
		w.subscribed = false
	}
	w.subscribedMu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	err := dbReset(w.dbTx)
	if err != nil {
		return err
	}
	w.wipeSecrets()
	w.keys = make(map[types.UnlockHash]spendableKey)
	w.lookahead = make(map[types.UnlockHash]uint64)
	w.seeds = []modules.Seed{}
	w.unconfirmedProcessedTransactions = []modules.ProcessedTransaction{}
	w.unlocked = false
	w.encrypted = false

	return nil
}

// InitFromSeed functions like Init, but using a specified seed. Unlike Init,
// the blockchain will be scanned to determine the seed's progress. For this
// reason, InitFromSeed should not be called until the blockchain is fully
// synced.
func (w *Wallet) InitFromSeed(masterKey crypto.CipherKey, seed modules.Seed) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	if !w.cs.Synced() {
		return errors.New("cannot init from seed until blockchain is synced")
	}

	// If masterKey is blank, use the hash of the seed.
	var err error
	if masterKey == nil {
		masterKey = crypto.NewWalletKey(crypto.HashObject(seed))
	}

	if !w.scanLock.TryLock() {
		return errScanInProgress
	}
	defer w.scanLock.Unlock()

	// estimate the primarySeedProgress by scanning the blockchain
	s := newSeedScanner(seed, w.log)
	if err := s.scan(w.cs, w.tg.StopChan()); err != nil {
		return err
	}
	// NOTE: each time the wallet generates a key for index n, it sets its
	// progress to n+1, so the progress should be the largest index seen + 1.
	// We also add 10% as a buffer because the seed may have addresses in the
	// wild that have not appeared in the blockchain yet.
	progress := s.largestIndexSeen + 1
	progress += progress / 10
	w.log.Printf("INFO: found key index %v in blockchain. Setting primary seed progress to %v", s.largestIndexSeen, progress)

	// initialize the wallet with the appropriate seed progress
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.initEncryption(masterKey, seed, progress)
	return err
}

// Unlocked indicates whether the wallet is locked or unlocked.
func (w *Wallet) Unlocked() (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, err
	}
	defer w.tg.Done()
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.unlocked, nil
}

// Lock will erase all keys from memory and prevent the wallet from spending
// coins until it is unlocked.
func (w *Wallet) Lock() error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()
	return w.managedLock()
}

// ChangeKey changes the wallet's encryption key from masterKey to newKey.
func (w *Wallet) ChangeKey(masterKey crypto.CipherKey, newKey crypto.CipherKey) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	return w.managedChangeKey(masterKey, newKey)
}

// ChangeKeyWithSeed is the same as ChangeKey but uses the primary seed
// instead of the current masterKey.
func (w *Wallet) ChangeKeyWithSeed(seed modules.Seed, newKey crypto.CipherKey) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()
	mk, err := w.managedMasterKey(seed)
	if err != nil {
		return errors.AddContext(err, "failed to retrieve masterkey by seed")
	}
	return w.managedChangeKey(mk, newKey)
}

// IsMasterKey verifies that the masterKey is the key used to encrypt the
// wallet.
func (w *Wallet) IsMasterKey(masterKey crypto.CipherKey) (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check provided key
	err := checkMasterKey(w.dbTx, masterKey)
	if errors.Contains(err, modules.ErrBadEncryptionKey) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// UnlockAsync will decrypt the wallet seed and load all of the addresses into
// memory.
func (w *Wallet) UnlockAsync(masterKey crypto.CipherKey) <-chan error {
	errChan := make(chan error, 1)
	defer close(errChan)
	// By having the wallet's ThreadGroup track the Unlock method, we ensure
	// that Unlock will never unlock the wallet once the ThreadGroup has been
	// stopped. Without this precaution, the wallet's Close method would be
	// unsafe because it would theoretically be possible for another function
	// to Unlock the wallet in the short interval after Close calls w.Lock
	// and before Close calls w.mu.Lock.
	if err := w.tg.Add(); err != nil {
		errChan <- err
		return errChan
	}
	defer w.tg.Done()

	if !w.scanLock.TryLock() {
		errChan <- errScanInProgress
		return errChan
	}
	defer w.scanLock.Unlock()

	w.log.Println("INFO: Unlocking wallet.")

	// Initialize all of the keys in the wallet under a lock. While holding the
	// lock, also grab the subscriber status.
	return w.managedUnlock(masterKey)
}

// Unlock will decrypt the wallet seed and load all of the addresses into
// memory.
func (w *Wallet) Unlock(masterKey crypto.CipherKey) error {
	return <-w.UnlockAsync(masterKey)
}

// managedChangeKey safely performs the database operations required to change
// the wallet's encryption key.
func (w *Wallet) managedChangeKey(masterKey crypto.CipherKey, newKey crypto.CipherKey) error {
	w.mu.Lock()
	encrypted := w.encrypted
	w.mu.Unlock()
	if !encrypted {
		return errUnencryptedWallet
	}

	// grab the current seed files
	var primarySeedFile seedFile
	var auxiliarySeedFiles []seedFile
	var unseededKeyFiles []spendableKeyFile

	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		// verify masterKey
		err := checkMasterKey(w.dbTx, masterKey)
		if err != nil {
			return errors.AddContext(err, "unable to verify master key")
		}

		wb := w.dbTx.Bucket(bucketWallet)

		// primarySeedFile
		err = encoding.Unmarshal(wb.Get(keyPrimarySeedFile), &primarySeedFile)
		if err != nil {
			return errors.AddContext(err, "unable to decode primary seed file")
		}

		// auxiliarySeedFiles
		err = encoding.Unmarshal(wb.Get(keyAuxiliarySeedFiles), &auxiliarySeedFiles)
		if err != nil {
			return errors.AddContext(err, "unable to decode auxiliary seed file")
		}

		// unseededKeyFiles
		err = encoding.Unmarshal(wb.Get(keySpendableKeyFiles), &unseededKeyFiles)
		if err != nil {
			return errors.AddContext(err, "unable to decode unseeded key file")
		}

		return nil
	}()
	if err != nil {
		return err
	}

	// decrypt key files
	var primarySeed modules.Seed
	var auxiliarySeeds []modules.Seed
	var spendableKeys []spendableKey

	primarySeed, err = decryptSeedFile(masterKey, primarySeedFile)
	if err != nil {
		return errors.AddContext(err, "unable to decrypt primary seed file")
	}
	for _, sf := range auxiliarySeedFiles {
		auxSeed, err := decryptSeedFile(masterKey, sf)
		if err != nil {
			return errors.AddContext(err, "unable to decrypt auxiliary seed file")
		}
		auxiliarySeeds = append(auxiliarySeeds, auxSeed)
	}
	for _, uk := range unseededKeyFiles {
		sk, err := decryptSpendableKeyFile(masterKey, uk)
		if err != nil {
			return errors.AddContext(err, "unable to decrypt unseed key file")
		}
		spendableKeys = append(spendableKeys, sk)
	}

	// encrypt new keyfiles using newKey
	var newPrimarySeedFile seedFile
	var newAuxiliarySeedFiles []seedFile
	var newUnseededKeyFiles []spendableKeyFile

	newPrimarySeedFile = createSeedFile(newKey, primarySeed)
	for _, seed := range auxiliarySeeds {
		sf := createSeedFile(newKey, seed)
		newAuxiliarySeedFiles = append(newAuxiliarySeedFiles, sf)
	}
	for _, sk := range spendableKeys {
		var skf spendableKeyFile
		fastrand.Read(skf.Salt[:])
		encryptionKey := saltedEncryptionKey(newKey, skf.Salt)
		skf.EncryptionVerification = encryptionKey.EncryptBytes(verificationPlaintext)

		// Encrypt and save the key.
		skf.SpendableKey = encryptionKey.EncryptBytes(encoding.Marshal(sk))
		newUnseededKeyFiles = append(newUnseededKeyFiles, skf)
	}

	// put the newly encrypted keys in the database
	err = func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		wb := w.dbTx.Bucket(bucketWallet)

		err = wb.Put(keyPrimarySeedFile, encoding.Marshal(newPrimarySeedFile))
		if err != nil {
			return errors.AddContext(err, "unable to put primary key into db")
		}
		err = wb.Put(keyAuxiliarySeedFiles, encoding.Marshal(newAuxiliarySeedFiles))
		if err != nil {
			return errors.AddContext(err, "unable to put auxiliary key into db")
		}
		err = wb.Put(keySpendableKeyFiles, encoding.Marshal(newUnseededKeyFiles))
		if err != nil {
			return errors.AddContext(err, "unable to put unseeded key into db")
		}

		uk := saltedEncryptionKey(newKey, dbGetWalletSalt(w.dbTx))
		err = wb.Put(keyEncryptionVerification, uk.EncryptBytes(verificationPlaintext))
		if err != nil {
			return errors.AddContext(err, "unable to put key encryption verification into db")
		}

		wpk := walletPasswordEncryptionKey(primarySeed, dbGetWalletSalt(w.dbTx))
		mkt := newKey.Type()
		mke := newKey.Key()
		err = wb.Put(keyWalletPassword, wpk.EncryptBytes(append(mkt[:], mke...)))
		if err != nil {
			return errors.AddContext(err, "unable to put wallet password into db")
		}

		return nil
	}()
	if err != nil {
		return err
	}

	return nil
}

// managedLock will erase all keys from memory and prevent the wallet from
// spending coins until it is unlocked.
func (w *Wallet) managedLock() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return modules.ErrLockedWallet
	}
	w.log.Println("INFO: Locking wallet.")

	// Wipe all of the seeds and secret keys. They will be replaced upon
	// calling 'Unlock' again. Note that since the public keys are not wiped,
	// we can continue processing blocks.
	w.wipeSecrets()
	w.unlocked = false
	return nil
}

// managedUnlocked indicates whether the wallet is locked or unlocked.
func (w *Wallet) managedUnlocked() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.unlocked
}
