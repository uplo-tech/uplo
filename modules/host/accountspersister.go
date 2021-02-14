package host

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

const (
	// accountSize is the fixed account size in bytes
	accountSize = 1 << 7 // 128 bytes

	// accountsFilename is the filename of the file that holds the accounts
	accountsFilename = "accounts.dat"

	// v148AccountsFilename is the filename of the file that holds the accounts
	// used for version <= 1.4.8.
	v148AccountsFilename = "accounts.txt"

	// fingerprintSize is the fixed fingerprint size in bytes
	fingerprintSize = 1 << 5 // 32 bytes

	// bucketBlockRange defines the range of expiry block heights the
	// fingerprints contained in a single bucket can span
	bucketBlockRange = 20

	// sectorSize is (traditionally) the size of a sector on disk in bytes.
	// It is used to perform a sanity check that verifies if this is a multiple
	// of the account size.
	sectorSize = 512
)

var (
	// specifierV1430 is the specifier for version 1.4.3.0
	specifierV1430 = types.NewSpecifier("1.4.3.0")

	// accountMetadata contains the header and version specifiers that identify
	// the accounts persist file.
	accountMetadata = persist.FixedMetadata{
		Header:  types.NewSpecifier("EphemeralAccount"),
		Version: specifierV1430,
	}

	// fingerprintsMetadata contains the header and version specifiers that
	// identify the fingerprints persist file.
	fingerprintsMetadata = persist.FixedMetadata{
		Header:  types.NewSpecifier("Fingerprint"),
		Version: specifierV1430,
	}

	// errRotationDisabled is returned when a disrupt disabled the rotation
	errRotationDisabled = errors.New("RotateFingerprintBuckets is disabled")
)

type (
	// accountsPersister is a subsystem that will persist data for the account
	// manager. This includes all ephemeral account data and the fingerprints of
	// the withdrawal messages.
	accountsPersister struct {
		accounts   modules.File
		indexLocks map[uint32]*indexLock

		staticFingerprintManager *fingerprintManager

		mu sync.Mutex
		h  *Host
	}

	// accountsPersisterData contains all accounts data and fingerprints
	accountsPersisterData struct {
		accounts     map[modules.AccountID]*account
		fingerprints map[crypto.Hash]struct{}
	}

	// accountData contains all data persisted for a single ephemeral account
	accountData struct {
		ID          modules.AccountID
		Balance     types.Currency
		LastTxnTime int64
	}

	// indexLock contains a lock plus a count of the number of threads currently
	// waiting to access the lock.
	indexLock struct {
		waiting int
		mu      sync.Mutex
	}

	// fingerprintManager is used to store fingerprints. It does so by keeping
	// them in two separate buckets, depending on the expiry blockheight of the
	// fingerprint. The underlying files rotate after a certain amount of blocks
	// to ensure the files don't grow too large in size. It has its own mutex to
	// avoid lock contention on the indexLocks.
	fingerprintManager struct {
		current     modules.File
		currentPath string
		next        modules.File
		nextPath    string

		staticSaveFingerprintsQueue saveFingerprintsQueue
		wakeChan                    chan struct{}

		mu sync.Mutex
		h  *Host
	}

	// saveFingerprintsQueue wraps a queue of fingerprints that are scheduled to
	// be persisted to disk. It has its own mutex to be able to enqueue a
	// fingerprint with minimal lock contention.
	saveFingerprintsQueue struct {
		queue []fingerprint
		mu    sync.Mutex
	}

	// fingerprint is a helper struct that contains the fingerprint hash and its
	// expiry block height. These objects are enqueued in the save queue and
	// processed by the threadedSaveFingerprintsLoop.
	fingerprint struct {
		hash   crypto.Hash
		expiry types.BlockHeight
	}
)

// newAccountsPersister returns a new account persister
func (h *Host) newAccountsPersister(am *accountManager) (_ *accountsPersister, err error) {
	if sectorSize%accountSize != 0 {
		h.log.Critical(errors.New("Sanity check failure: we expected the sector size to be a multiple of the account size to ensure persisting an account never crosses the sector boundary on disk."))
	}

	ap := &accountsPersister{
		indexLocks: make(map[uint32]*indexLock),
		h:          h,
	}

	// Open the accounts file
	path := filepath.Join(h.persistDir, accountsFilename)
	if ap.accounts, err = ap.openAccountsFile(path); err != nil {
		return nil, errors.AddContext(err, "could not open accounts file")
	}

	// Create the fingerprint manager
	fpm, err := ap.newFingerprintManager()
	if err != nil {
		return nil, err
	}
	ap.staticFingerprintManager = fpm

	// Start the save loop
	go fpm.threadedSaveFingerprintsLoop()

	return ap, nil
}

// newFingerprintManager will create a new fingerprint manager, this manager
// uses two files to store the fingerprints on disk.
func (ap *accountsPersister) newFingerprintManager() (_ *fingerprintManager, err error) {
	currFilename, nextFilename := fingerprintsFilenames(ap.h.blockHeight)

	fm := &fingerprintManager{
		currentPath:                 filepath.Join(ap.h.persistDir, currFilename),
		nextPath:                    filepath.Join(ap.h.persistDir, nextFilename),
		staticSaveFingerprintsQueue: saveFingerprintsQueue{queue: make([]fingerprint, 0)},
		wakeChan:                    make(chan struct{}, 1),
		h:                           ap.h,
	}

	fm.current, err = ap.openFingerprintBucket(fm.currentPath)
	if err != nil {
		return nil, errors.AddContext(err, fmt.Sprintf("could not open fingerprint bucket at path %s", fm.currentPath))
	}

	fm.next, err = ap.openFingerprintBucket(fm.nextPath)
	if err != nil {
		return nil, errors.AddContext(err, fmt.Sprintf("could not open fingerprint bucket at path %s", fm.nextPath))
	}

	return fm, nil
}

// callLoadData loads all accounts data and fingerprints from disk
func (ap *accountsPersister) callLoadData() (*accountsPersisterData, error) {
	accounts := make(map[modules.AccountID]*account)
	fingerprints := make(map[crypto.Hash]struct{})

	// Load accounts
	err := func() error {
		ap.mu.Lock()
		defer ap.mu.Unlock()
		return ap.loadAccounts(ap.accounts, accounts)
	}()
	if err != nil {
		return nil, err
	}

	// Load fingerprints
	fm := ap.staticFingerprintManager
	err = func() error {
		fm.mu.Lock()
		defer fm.mu.Unlock()
		return errors.Compose(
			ap.loadFingerprints(fm.currentPath, fingerprints),
			ap.loadFingerprints(fm.nextPath, fingerprints),
		)
	}()
	if err != nil {
		return nil, err
	}

	return &accountsPersisterData{
		accounts:     accounts,
		fingerprints: fingerprints,
	}, nil
}

// callSaveAccount will persist the given account data at the location
// corresponding to the given index.
func (ap *accountsPersister) callSaveAccount(data *accountData, index uint32) error {
	ap.managedLockIndex(index)
	defer ap.managedUnlockIndex(index)

	// Get the account data bytes
	accBytes, err := data.bytes()
	if err != nil {
		return errors.AddContext(err, "save account failed, account could not be encoded")
	}

	// Write the data to disk
	_, err = ap.accounts.WriteAt(accBytes, location(index))
	if err != nil {
		panic("Unable to write the ephemeral account to disk.")
	}

	return nil
}

// callQueueSaveFingerprint adds the given fingerprint to the save queue.
func (ap *accountsPersister) callQueueSaveFingerprint(hash crypto.Hash, expiry types.BlockHeight) {
	fm := ap.staticFingerprintManager
	fm.staticSaveFingerprintsQueue.mu.Lock()
	fm.staticSaveFingerprintsQueue.queue = append(fm.staticSaveFingerprintsQueue.queue, fingerprint{hash, expiry})
	fm.staticSaveFingerprintsQueue.mu.Unlock()
	fm.staticWake()
}

// callBatchDeleteAccount will overwrite the accounts at given indexes with
// zero-bytes. Effectively deleting it.
func (ap *accountsPersister) callBatchDeleteAccount(indexes []uint32) (deleted []uint32, err error) {
	results := make([]error, len(indexes))
	zeroBytes := make([]byte, accountSize)

	// Overwrite the accounts with 0 bytes in parallel
	var wg sync.WaitGroup
	for n, index := range indexes {
		wg.Add(1)
		go func(n int, index uint32) {
			defer wg.Done()
			ap.managedLockIndex(index)
			defer ap.managedUnlockIndex(index)
			_, results[n] = ap.accounts.WriteAt(zeroBytes, location(index))
		}(n, index)
	}
	wg.Wait()

	// Collect the indexes of all accounts that were successfully deleted,
	// compose the errors of the failures.
	for n, rErr := range results {
		if rErr != nil {
			err = errors.Compose(err, rErr)
			continue
		}
		deleted = append(deleted, indexes[n])
	}
	err = errors.AddContext(err, "batch delete account failed")
	return
}

// callRotateFingerprintBuckets will rotate the fingerprint buckets
func (ap *accountsPersister) callRotateFingerprintBuckets() (err error) {
	if ap.h.dependencies.Disrupt("DisableRotateFingerprintBuckets") {
		return errRotationDisabled
	}

	// Get blockheight before acquiring fingerprint manager lock.
	bh := ap.h.BlockHeight()

	fm := ap.staticFingerprintManager
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Close the current fingerprint files, this syncs the files before closing
	err = fm.syncAndClose()
	if err != nil {
		// note that we do not prevent this error from reopening the fingerprint
		// buckets, if we were to return here chances are the host is in a
		// deadlock situation where his withdrawals would be permanently
		// deactivated, which would be a devastating event for a host, instead
		// we log the critical
		ap.h.log.Critical(fmt.Sprintf("could not close fingerprint files, err: %v", err))
	}

	// Calculate new filenames for the fingerprint buckets
	currFilename, nextFilename := fingerprintsFilenames(bh)

	// Reopen files
	fm.currentPath = filepath.Join(ap.h.persistDir, currFilename)
	fm.current, err = ap.openFingerprintBucket(fm.currentPath)
	if err != nil {
		return errors.AddContext(err, fmt.Sprintf("could not open fingerprint bucket, path %s", fm.currentPath))
	}

	fm.nextPath = filepath.Join(ap.h.persistDir, nextFilename)
	fm.next, err = ap.openFingerprintBucket(fm.nextPath)
	if err != nil {
		return errors.AddContext(err, fmt.Sprintf("could not open fingerprint bucket, path %s", fm.nextPath))
	}

	// Remove old fingerprint buckets in a separate thread
	go fm.threadedRemoveOldFingerprintBuckets()

	return nil
}

// callClose will cleanly shutdown the account persister's open file handles
func (ap *accountsPersister) callClose() error {
	ap.staticFingerprintManager.mu.Lock()
	err1 := ap.staticFingerprintManager.syncAndClose()
	ap.staticFingerprintManager.mu.Unlock()
	ap.mu.Lock()
	err2 := syncAndClose(ap.accounts)
	ap.mu.Unlock()
	return errors.Compose(err1, err2)
}

// managedLockIndex grabs a lock on an (account) index.
func (ap *accountsPersister) managedLockIndex(index uint32) {
	ap.mu.Lock()
	il, exists := ap.indexLocks[index]
	if exists {
		il.waiting++
	} else {
		il = &indexLock{
			waiting: 1,
		}
		ap.indexLocks[index] = il
	}
	ap.mu.Unlock()

	// Block until the index is available.
	il.mu.Lock()
}

// managedUnlockIndex releases a lock on an (account) index.
func (ap *accountsPersister) managedUnlockIndex(index uint32) {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	// Release the lock on the index.
	il, exists := ap.indexLocks[index]
	if !exists {
		ap.h.log.Critical("Unlock of an account index that is not locked.")
		return
	}
	il.waiting--
	il.mu.Unlock()

	// If nobody else is trying to lock the index, perform garbage collection.
	if il.waiting == 0 {
		delete(ap.indexLocks, index)
	}
}

// openAccountsFile is a helper method to open the accounts file with
// appropriate metadata header and flags
func (ap *accountsPersister) openAccountsFile(path string) (modules.File, error) {
	// open file in read-write mode and create if it does not exist yet
	return ap.openFileWithMetadata(path, os.O_RDWR|os.O_CREATE, accountMetadata)
}

// openFingerprintBucket is a helper method to open a fingerprint bucket with
// appropriate metadata header and flags
func (ap *accountsPersister) openFingerprintBucket(path string) (modules.File, error) {
	// open file in append-only mode and create if it does not exist yet
	return ap.openFileWithMetadata(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, fingerprintsMetadata)
}

// openFileWithMetadata will open the file at given path. If the file did not
// exist prior to calling this method, it will write the metadata header to it.
func (ap *accountsPersister) openFileWithMetadata(path string, flags int, metadata persist.FixedMetadata) (modules.File, error) {
	_, statErr := os.Stat(path)

	// Open the file, create it if it does not exist yet
	file, err := ap.h.dependencies.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}

	// If it did not exist prior to calling this method, write header metadata.
	// Otherwise verify the metadata header.
	if os.IsNotExist(statErr) {
		_, err = file.Write(encoding.Marshal(metadata))
	} else {
		_, err = persist.VerifyMetadataHeader(file, metadata)
	}
	if err != nil {
		return nil, err
	}

	return file, nil
}

// loadAccounts will read the given file and load the accounts into the map
func (ap *accountsPersister) loadAccounts(file modules.File, m map[modules.AccountID]*account) error {
	bytes, err := ioutil.ReadFile(file.Name())
	if err != nil {
		return errors.AddContext(err, "could not read accounts file")
	}
	nBytes := int64(len(bytes))

	for index := uint32(0); ; index++ {
		accLocation := location(index)
		if accLocation >= nBytes {
			break
		}
		accBytes := bytes[accLocation : accLocation+accountSize]

		var data accountData
		if err := encoding.Unmarshal(accBytes, &data); err != nil {
			// not much we can do here besides log this critical event
			ap.h.log.Critical(errors.New("could not decode account data"))
			continue // TODO follow-up host alert (?)
		}

		// deleted accounts will decode into an account with 0 as LastTxnTime,
		// we want to skip those so that index becomes free and eventually gets
		// overwritten.
		if data.LastTxnTime > 0 {
			account := data.account(index)
			m[account.id] = account
		}
	}

	return nil
}

// loadFingerprints will read the file at given path and load the fingerprints
// into the map
func (ap *accountsPersister) loadFingerprints(path string, m map[crypto.Hash]struct{}) error {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return errors.AddContext(err, "could not read fingerprints file")
	}

	for i := persist.FixedMetadataSize; i < len(bytes); i += fingerprintSize {
		var fp crypto.Hash
		if err := encoding.Unmarshal(bytes[i:i+fingerprintSize], &fp); err != nil {
			// not much we can do here besides log this critical event
			ap.h.log.Critical(errors.New("could not decode fingerprint data"))
			continue // TODO follow-up host alert (?)
		}
		m[fp] = struct{}{}
	}

	return nil
}

// threadedSaveFingerprintsLoop continuously checks if fingerprints got added to
// the queue and will save them. The loop blocks until it receives a message on
// the wakeChan, or until it receives a stop signal.
//
// Note: threadgroup counter must be inside for loop. If not, calling 'Flush'
// on the threadgroup would deadlock.
func (fm *fingerprintManager) threadedSaveFingerprintsLoop() {
	for {
		var workPerformed bool
		func() {
			if err := fm.h.tg.Add(); err != nil {
				return
			}
			defer fm.h.tg.Done()

			fm.staticSaveFingerprintsQueue.mu.Lock()
			if len(fm.staticSaveFingerprintsQueue.queue) == 0 {
				fm.staticSaveFingerprintsQueue.mu.Unlock()
				return
			}

			fp := fm.staticSaveFingerprintsQueue.queue[0]
			fm.staticSaveFingerprintsQueue.queue = fm.staticSaveFingerprintsQueue.queue[1:]
			fm.staticSaveFingerprintsQueue.mu.Unlock()

			err := fm.managedSave(fp)
			if err != nil {
				fm.h.log.Fatal("Could not save fingerprint", err)
			}
			workPerformed = true
		}()

		if workPerformed {
			continue
		}

		select {
		case <-fm.wakeChan:
			continue
		case <-fm.h.tg.StopChan():
			return
		}
	}
}

// managedSave will persist the given fingerprint into the appropriate bucket
func (fm *fingerprintManager) managedSave(fp fingerprint) error {
	// Encode the fingerprint, verify it has the correct size
	fpBytes, err := safeEncode(fp.hash, fingerprintSize)
	if err != nil {
		build.Critical(errors.New("fingerprint size is larger than the expected size"))
		return ErrAccountPersist
	}
	bh := fm.h.BlockHeight()

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Write into bucket depending on its expiry
	_, max := currentBucketRange(bh)
	if fp.expiry <= max {
		_, err := fm.current.Write(fpBytes)
		return err
	}
	_, err = fm.next.Write(fpBytes)
	return err
}

// staticWake is called every time a fingerprint is added to the save queue.
func (fm *fingerprintManager) staticWake() {
	select {
	case fm.wakeChan <- struct{}{}:
	default:
	}
}

// threadedRemoveOldFingerprintBuckets will remove the fingerprint buckets that
// are not active and can be safely removed.
func (fm *fingerprintManager) threadedRemoveOldFingerprintBuckets() {
	if err := fm.h.tg.Add(); err != nil {
		return
	}
	defer fm.h.tg.Done()

	// Grab the current path
	fm.mu.Lock()
	current := fm.currentPath
	fm.mu.Unlock()

	// Get the min blockheight of the bucket range, although it should never be
	// the case we sanity check the current path is a valid bucket path.
	min, _, bucket := isFingerprintBucket(filepath.Base(current))
	if !bucket {
		build.Critical("The current fingerprint bucket path is considered invalid")
	}

	// Create a function that decides whether or not to remove a fingerprint
	// bucket, we can safely remove it if it's max is below the current min
	// bucket range. This way we are sure to remove only old bucket files. This
	// is important because there might be new files opened on disk after
	// releasing the lock, we would not want to remove the current buckets.
	isOldBucket := func(name string) bool {
		_, max, bucket := isFingerprintBucket(name)
		return bucket && max < min
	}

	// Read directory
	fileinfos, err := ioutil.ReadDir(fm.h.persistDir)
	if err != nil {
		fm.h.log.Fatal("Failed to remove old fingerprint buckets, could not read directory", err)
		return
	}

	// Iterate over directory
	for _, fi := range fileinfos {
		if isOldBucket(fi.Name()) {
			err := os.Remove(filepath.Join(fm.h.persistDir, fi.Name()))
			if err != nil {
				fm.h.log.Fatal("Failed to remove old fingerprint buckets", err)
			}
		}
	}
}

// syncAndClose will safely close the current and next bucket file
func (fm *fingerprintManager) syncAndClose() error {
	return errors.Compose(
		syncAndClose(fm.current),
		syncAndClose(fm.next),
	)
}

// accountData transforms the account into an accountData struct which will
// contain all data we persist to disk
func (a *account) accountData() *accountData {
	return &accountData{
		ID:          a.id,
		Balance:     a.balance,
		LastTxnTime: a.lastTxnTime,
	}
}

// account transforms the accountData we loaded from disk into an account we
// keep in memory
func (a *accountData) account(index uint32) *account {
	return &account{
		id:                 a.ID,
		balance:            a.Balance,
		lastTxnTime:        a.LastTxnTime,
		index:              index,
		blockedWithdrawals: make(blockedWithdrawalHeap, 0),
	}
}

// bytes returns the account data as bytes.
func (a *accountData) bytes() ([]byte, error) {
	// Encode the account, verify it has the correct size
	accBytes, err := safeEncode(*a, accountSize)
	if err != nil {
		build.Critical(errors.AddContext(err, "unexpected ephemeral account size"))
		return nil, err
	}
	return accBytes, nil
}

// currentBucketRange will calculate the range (in blockheight) that defines the
// boundaries of the current bucket.
func currentBucketRange(currentBlockHeight types.BlockHeight) (min, max types.BlockHeight) {
	cbh := uint64(currentBlockHeight)
	bbr := uint64(bucketBlockRange)
	threshold := cbh + (bbr - (cbh % bbr))
	min = types.BlockHeight(threshold - bucketBlockRange)
	max = types.BlockHeight(threshold) - 1
	return
}

// fingerprintsFilenames will calculate the filenames for the current and next
// fingerprints bucket.
func fingerprintsFilenames(currentBlockHeight types.BlockHeight) (current, next string) {
	min, max := currentBucketRange(currentBlockHeight)
	current = fmt.Sprintf("fingerprintsbucket_%v-%v.db", min, max)
	min += bucketBlockRange
	max += bucketBlockRange
	next = fmt.Sprintf("fingerprintsbucket_%v-%v.db", min, max)
	return
}

// isFingerprintBucket is a helper function that takes a filename and returns
// whether or not this is a fingerprint bucket. If it is, it also returns the
// bucket's range as a min and max blockheight.
func isFingerprintBucket(filename string) (types.BlockHeight, types.BlockHeight, bool) {
	// match the filename
	re := regexp.MustCompile(`^fingerprintsbucket_(\d+)-(\d+).db$`)
	match := re.FindStringSubmatch(filename)
	if len(match) != 3 {
		return 0, 0, false
	}

	// parse range - note we can safely ignore the error here due to our regex
	min, _ := strconv.Atoi(match[1])
	max, _ := strconv.Atoi(match[2])

	// sanity check the range makes sense
	if min >= max {
		build.Critical(fmt.Sprintf("Bucket file found with range where min is not smaller than max height, %s", filename))
		return 0, 0, false
	}

	return types.BlockHeight(min), types.BlockHeight(max), true
}

// syncAndClose will sync and close the given file
func syncAndClose(file modules.File) error {
	return errors.Compose(
		file.Sync(),
		file.Close(),
	)
}

// safeEncode will encode the given object while performing a sanity check on
// the size
func safeEncode(obj interface{}, expectedSize int) ([]byte, error) {
	objMar := encoding.Marshal(obj)
	if len(objMar) > expectedSize {
		return nil, errors.New("encoded object is larger than the expected size")
	}

	// copy bytes into an array of appropriate size
	bytes := make([]byte, expectedSize)
	copy(bytes, objMar)
	return bytes, nil
}

// location is a helper method that returns the location of the account with
// given index.
func location(index uint32) int64 {
	// metadataPadding is the amount of bytes we pad the metadata with. We pad
	// metadata to ensure writing an account to disk never crosses the 512 bytes
	// boundary, traditionally the size of a sector on disk. Seeing as the
	// sector size is a multiple of the account size we pad the metadata until
	// it's as large as a single account
	metadataPadding := int64(accountSize - persist.FixedMetadataSize)
	return persist.FixedMetadataSize + metadataPadding + int64(uint64(index)*accountSize)
}
