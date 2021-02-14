package host

// storageobligations.go is responsible for managing the storage obligations
// within the host - making sure that any file contracts, transaction
// dependencies, file contract revisions, and storage proofs are making it into
// the blockchain in a reasonable time.
//
// NOTE: Currently, the code partially supports changing the storage proof
// window in file contract revisions, however the action item code will not
// handle it correctly. Until the action item code is improved (to also handle
// byzantine situations where the renter submits prior revisions), the host
// should not support changing the storage proof window, especially to further
// in the future.

// TODO: Need to queue the action item for checking on the submission status of
// the file contract revision. Also need to make sure that multiple actions are
// being taken if needed.

// TODO: Make sure that the origin tranasction set is not submitted to the
// transaction pool before addSO is called - if it is, there will be a
// duplicate transaction error, and then the storage obligation will return an
// error, which is bad. Well, or perhas we just need to have better logic
// handling.

// TODO: Need to make sure that 'revision confirmed' is actually looking only
// at the most recent revision (I think it is...)

// TODO: Make sure that not too many action items are being created.

// TODO: The ProofConstructed field of storageObligation
// is not set or used.

import (
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"github.com/uplo-tech/bolt"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/wallet"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
)

const (
	obligationUnresolved storageObligationStatus = iota // Indicatees that an unitialized value was used.
	obligationRejected                                  // Indicates that the obligation never got started, no revenue gained or lost.
	obligationSucceeded                                 // Indicates that the obligation was completed, revenues were gained.
	obligationFailed                                    // Indicates that the obligation failed, revenues and collateral were lost.
)

const (
	// largeContractSize is the threshold at which the largeContractUpdateDelay
	// kicks in whenever modifyStorageObligation is called.
	largeContractSize = 2 * 1 << 40 // 2 TiB
	// largeContractUpdateDelay is the delay applied when calling
	// modifyStorageObligation on an obligation for a contract with a size
	// greater than or equal to largeContractSize.
	largeContractUpdateDelay = 2 * time.Second

	// txnFeeSizeBuffer is a buffer added to the approximate size of a
	// transaction before estimating its fee. This makes it more likely that the
	// txn will be mined in the next block.
	txnFeeSizeBuffer = 300
)

var (
	// errInsaneFileContractOutputCounts is returned when a file contract has
	// the wrong number of outputs for either the valid or missed payouts.
	//
	//lint:ignore U1000 used in isSane() which is currently unused but we want to keep it around
	errInsaneFileContractOutputCounts = errors.New("file contract has incorrect number of outputs for the valid or missed payouts")

	// errInsaneFileContractRevisionOutputCounts is returned when a file
	// contract has the wrong number of outputs for either the valid or missed
	// payouts.
	//
	//lint:ignore U1000 used in isSane() which is currently unused but we want to keep it around
	errInsaneFileContractRevisionOutputCounts = errors.New("file contract revision has incorrect number of outputs for the valid or missed payouts")

	// errInsaneOriginSetFileContract is returned is the final transaction of
	// the origin transaction set of a storage obligation does not have a file
	// contract in the final transaction - there should be a file contract
	// associated with every storage obligation.
	//
	//lint:ignore U1000 used in isSane() which is currently unused but we want to keep it around
	errInsaneOriginSetFileContract = errors.New("origin transaction set of storage obligation should have one file contract in the final transaction")

	// errInsaneOriginSetSize is returned if the origin transaction set of a
	// storage obligation is empty - there should be a file contract associated
	// with every storage obligation.
	//
	//lint:ignore U1000 used in isSane() which is currently unused but we want to keep it around
	errInsaneOriginSetSize = errors.New("origin transaction set of storage obligation is size zero")

	// errInsaneRevisionSetRevisionCount is returned if the final transaction
	// in the revision transaction set of a storage obligation has more or less
	// than one file contract revision.
	//
	//lint:ignore U1000 used in isSane() which is currently unused but we want to keep it around
	errInsaneRevisionSetRevisionCount = errors.New("revision transaction set of storage obligation should have one file contract revision in the final transaction")

	// errInsaneStorageObligationRevision is returned if there is an attempted
	// storage obligation revision which does not have sensical inputs.
	errInsaneStorageObligationRevision = errors.New("revision to storage obligation does not make sense")

	// errInsaneStorageObligationRevisionData is returned if there is an
	// attempted storage obligation revision which does not have sensical
	// inputs.
	//
	//lint:ignore U1000 used in isSane() which is currently unused but we want to keep it around
	errInsaneStorageObligationRevisionData = errors.New("revision to storage obligation has insane data")

	// errNoBuffer is returned if there is an attempted storage obligation that
	// needs to have the storage proof submitted in less than
	// revisionSubmissionBuffer blocks.
	errNoBuffer = errors.New("file contract rejected because storage proof window is too close")

	// errNoStorageObligation is returned if the requested storage obligation
	// is not found in the database.
	errNoStorageObligation = errors.New("storage obligation not found in database")

	// errObligationUnlocked is returned when a storage obligation is being
	// removed from lock, but is already unlocked.
	errObligationUnlocked = errors.New("storage obligation is unlocked, and should not be getting unlocked")
)

// storageObligation contains all of the metadata related to a file contract
// and the storage contained by the file contract.
type storageObligation struct {
	// Storage obligations are broken up into ordered atomic sectors that are
	// exactly 4MiB each. By saving the roots of each sector, storage proofs
	// and modifications to the data can be made inexpensively by making use of
	// the merkletree.CachedTree. Sectors can be appended, modified, or deleted
	// and the host can recompute the Merkle root of the whole file without
	// much computational or I/O expense.
	SectorRoots []crypto.Hash

	// Variables about the file contract that enforces the storage obligation.
	// The origin an revision transaction are stored as a set, where the set
	// contains potentially unconfirmed transactions.
	ContractCost             types.Currency
	LockedCollateral         types.Currency
	PotentialAccountFunding  types.Currency
	PotentialDownloadRevenue types.Currency
	PotentialStorageRevenue  types.Currency
	PotentialUploadRevenue   types.Currency
	RiskedCollateral         types.Currency
	TransactionFeesAdded     types.Currency

	// The negotiation height specifies the block height at which the file
	// contract was negotiated. If the origin transaction set is not accepted
	// onto the blockchain quickly enough, the contract is pruned from the
	// host. The origin and revision transaction set contain the contracts +
	// revisions as well as all parent transactions. The parents are necessary
	// because after a restart the transaction pool may be emptied out.
	NegotiationHeight      types.BlockHeight
	OriginTransactionSet   []types.Transaction
	RevisionTransactionSet []types.Transaction

	// Variables indicating whether the critical transactions in a storage
	// obligation have been confirmed on the blockchain.
	ObligationStatus    storageObligationStatus
	OriginConfirmed     bool
	ProofConfirmed      bool
	ProofConstructed    bool
	RevisionConfirmed   bool
	RevisionConstructed bool

	h *Host
}

// storageObligationStatus indicates the current status of a storage obligation
type storageObligationStatus uint64

// String converts a storageObligationStatus to a string.
func (i storageObligationStatus) String() string {
	if i == 0 {
		return "obligationUnresolved"
	}
	if i == 1 {
		return "obligationRejected"
	}
	if i == 2 {
		return "obligationSucceeded"
	}
	if i == 3 {
		return "obligationFailed"
	}
	return "storageObligationStatus(" + strconv.FormatInt(int64(i), 10) + ")"
}

// managedGetStorageObligationSnapshot fetches a storage obligation from the
// database and returns a snapshot.
func (h *Host) managedGetStorageObligationSnapshot(id types.FileContractID) (StorageObligationSnapshot, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var err error
	var so storageObligation
	if err = h.db.View(func(tx *bolt.Tx) error {
		so, err = h.getStorageObligation(tx, id)
		return err
	}); err != nil {
		return StorageObligationSnapshot{}, err
	}

	if len(so.OriginTransactionSet) == 0 {
		return StorageObligationSnapshot{}, errors.New("origin txnset is empty")
	}
	if len(so.RevisionTransactionSet) == 0 {
		return StorageObligationSnapshot{}, errors.New("revision txnset is empty")
	}

	revTxn := so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1]

	return StorageObligationSnapshot{
		staticContractSize:  so.fileSize(),
		staticMerkleRoot:    so.merkleRoot(),
		staticProofDeadline: so.proofDeadline(),
		staticRevisionTxn:   revTxn,
		staticSectorRoots:   so.SectorRoots,
	}, nil
}

// getStorageObligation fetches a storage obligation from the database tx.
func (h *Host) getStorageObligation(tx *bolt.Tx, soid types.FileContractID) (so storageObligation, err error) {
	soBytes := tx.Bucket(bucketStorageObligations).Get(soid[:])
	if soBytes == nil {
		return storageObligation{}, errNoStorageObligation
	}
	err = json.Unmarshal(soBytes, &so)
	if err != nil {
		return storageObligation{}, err
	}
	so.h = h
	return so, nil
}

// putStorageObligation places a storage obligation into the database,
// overwriting the existing storage obligation if there is one.
func putStorageObligation(tx *bolt.Tx, so storageObligation) error {
	soBytes, err := json.Marshal(so)
	if err != nil {
		return err
	}
	soid := so.id()
	return tx.Bucket(bucketStorageObligations).Put(soid[:], soBytes)
}

// StorageObligationSnapshot is a snapshot of a StorageObligation. A snapshot is
// a deep-copy and can be accessed without locking at the cost of being a frozen
// readonly representation of an SO which only exists in memory. Note that this
// snapshot only contains the properties required by the MDM to execute a
// program. This can be extended in the future to support other use cases.
type StorageObligationSnapshot struct {
	staticContractSize  uint64
	staticMerkleRoot    crypto.Hash
	staticProofDeadline types.BlockHeight
	staticRevisionTxn   types.Transaction
	staticSectorRoots   []crypto.Hash
}

// ZeroStorageObligationSnapshot returns the storage obligation snapshot of an
// empty contract. All fields are set to the defaults.
func ZeroStorageObligationSnapshot() StorageObligationSnapshot {
	return StorageObligationSnapshot{
		staticContractSize:  0,
		staticMerkleRoot:    crypto.Hash{},
		staticProofDeadline: types.BlockHeight(0),
		staticSectorRoots:   []crypto.Hash{},
		staticRevisionTxn: types.Transaction{
			FileContractRevisions: []types.FileContractRevision{
				{
					NewValidProofOutputs:  make([]types.UplocoinOutput, 2),
					NewMissedProofOutputs: make([]types.UplocoinOutput, 3),
				},
			},
		},
	}
}

// ContractSize returns the size of the underlying contract, which is static and
// is the value of the contract size at the time the snapshot was taken.
func (sos StorageObligationSnapshot) ContractSize() uint64 {
	return sos.staticContractSize
}

// ProofDeadline returns the proof deadline of the underlying contract.
func (sos StorageObligationSnapshot) ProofDeadline() types.BlockHeight {
	return sos.staticProofDeadline
}

// MerkleRoot returns the merkle root, which is static and is the value of the
// merkle root at the time the snapshot was taken.
func (sos StorageObligationSnapshot) MerkleRoot() crypto.Hash {
	return sos.staticMerkleRoot
}

// RecentRevision returns the recent revision at the time the snapshot was
// taken.
func (sos StorageObligationSnapshot) RecentRevision() types.FileContractRevision {
	return sos.staticRevisionTxn.FileContractRevisions[0]
}

// RevisionTxn returns the txn containing the filecontract revision.
func (sos StorageObligationSnapshot) RevisionTxn() types.Transaction {
	return sos.staticRevisionTxn
}

// SectorRoots returns a static list of the sector roots present at the time the
// snapshot was taken.
func (sos StorageObligationSnapshot) SectorRoots() []crypto.Hash {
	return sos.staticSectorRoots
}

// UnallocatedCollateral returns the remaining collateral within the contract
// that hasn't been allocated yet. This means it is not yet moved to the void in
// case of a missed storage proof.
func (sos StorageObligationSnapshot) UnallocatedCollateral() types.Currency {
	return sos.RecentRevision().MissedHostPayout()
}

// Update will take a list of sector changes and update the database to account
// for all of it.
func (so storageObligation) Update(sectorRoots []crypto.Hash, sectorsRemoved map[crypto.Hash]struct{}, sectorsGained map[crypto.Hash][]byte) error {
	so.SectorRoots = sectorRoots
	sr := make([]crypto.Hash, 0, len(sectorsRemoved))
	for sector := range sectorsRemoved {
		sr = append(sr, sector)
	}
	return so.h.managedModifyStorageObligation(so, sr, sectorsGained)
}

// expiration returns the height at which the storage obligation expires.
func (so storageObligation) expiration() types.BlockHeight {
	if len(so.RevisionTransactionSet) > 0 {
		return so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewWindowStart
	}
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].WindowStart
}

// fileSize returns the size of the data protected by the obligation.
func (so storageObligation) fileSize() uint64 {
	if len(so.RevisionTransactionSet) > 0 {
		return so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewFileSize
	}
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].FileSize
}

// id returns the id of the storage obligation, which is defined by the file
// contract id of the file contract that governs the storage contract.
func (so storageObligation) id() types.FileContractID {
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContractID(0)
}

// isSane checks that required assumptions about the storage obligation are
// correct.
//
//lint:ignore U1000 isSane() is currently unused but we want to keep it around
func (so storageObligation) isSane() error {
	// There should be an origin transaction set.
	if len(so.OriginTransactionSet) == 0 {
		build.Critical("origin transaction set is empty")
		return errInsaneOriginSetSize
	}

	// The final transaction of the origin transaction set should have one file
	// contract.
	final := len(so.OriginTransactionSet) - 1
	fcCount := len(so.OriginTransactionSet[final].FileContracts)
	if fcCount != 1 {
		build.Critical("wrong number of file contracts associated with storage obligation:", fcCount)
		return errInsaneOriginSetFileContract
	}

	// The file contract in the final transaction of the origin transaction set
	// should have two valid proof outputs and two missed proof outputs.
	lenVPOs := len(so.OriginTransactionSet[final].FileContracts[0].ValidProofOutputs)
	lenMPOs := len(so.OriginTransactionSet[final].FileContracts[0].MissedProofOutputs)
	if lenVPOs != 2 || lenMPOs != 2 {
		build.Critical("file contract has wrong number of VPOs and MPOs, expecting 2 each:", lenVPOs, lenMPOs)
		return errInsaneFileContractOutputCounts
	}

	// If there is a revision transaction set, there should be one file
	// contract revision in the final transaction.
	if len(so.RevisionTransactionSet) > 0 {
		final = len(so.OriginTransactionSet) - 1
		fcrCount := len(so.OriginTransactionSet[final].FileContractRevisions)
		if fcrCount != 1 {
			build.Critical("wrong number of file contract revisions in final transaction of revision transaction set:", fcrCount)
			return errInsaneRevisionSetRevisionCount
		}

		// The file contract revision in the final transaction of the revision
		// transaction set should have two valid proof outputs and two missed
		// proof outputs.
		lenVPOs = len(so.RevisionTransactionSet[final].FileContractRevisions[0].NewValidProofOutputs)
		lenMPOs = len(so.RevisionTransactionSet[final].FileContractRevisions[0].NewMissedProofOutputs)
		if lenVPOs != 2 || lenMPOs != 2 {
			build.Critical("file contract has wrong number of VPOs and MPOs, expecting 2 each:", lenVPOs, lenMPOs)
			return errInsaneFileContractRevisionOutputCounts
		}
	}
	return nil
}

// merkleRoot returns the file merkle root of a storage obligation.
func (so storageObligation) merkleRoot() crypto.Hash {
	if len(so.RevisionTransactionSet) > 0 {
		return so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewFileMerkleRoot
	}
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].FileMerkleRoot
}

// payouts returns the set of valid payouts and missed payouts that represent
// the latest revision for the storage obligation.
func (so storageObligation) payouts() (valid []types.UplocoinOutput, missed []types.UplocoinOutput) {
	valid = make([]types.UplocoinOutput, 2)
	missed = make([]types.UplocoinOutput, 3)
	if len(so.RevisionTransactionSet) > 0 {
		copy(valid, so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewValidProofOutputs)
		copy(missed, so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewMissedProofOutputs)
		return
	}
	copy(valid, so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].ValidProofOutputs)
	copy(missed, so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].MissedProofOutputs)
	return
}

// revisionNumber returns the last revision number of the latest revision
// for the storage obligation
func (so storageObligation) revisionNumber() uint64 {
	if len(so.RevisionTransactionSet) > 0 {
		return so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewRevisionNumber
	}
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].RevisionNumber
}

// requiresProof is a helper to determine whether the storage obligation
// requires a proof.
func (so storageObligation) requiresProof() bool {
	// No need for a proof if the obligation doesn't have a revision.
	rev, err := so.recentRevision()
	if err != nil {
		return false
	}
	// No need for a proof if the valid outputs match the invalid ones. Right
	// now this is only the case if the contract was renewed.
	if reflect.DeepEqual(rev.NewValidProofOutputs, rev.NewMissedProofOutputs) {
		return false
	}
	// Every other case requires a proof.
	return true
}

// proofDeadline returns the height by which the storage proof must be
// submitted.
func (so storageObligation) proofDeadline() types.BlockHeight {
	if len(so.RevisionTransactionSet) > 0 {
		return so.RevisionTransactionSet[len(so.RevisionTransactionSet)-1].FileContractRevisions[0].NewWindowEnd
	}
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].FileContracts[0].WindowEnd
}

// transactionID returns the ID of the transaction containing the file
// contract.
func (so storageObligation) transactionID() types.TransactionID {
	return so.OriginTransactionSet[len(so.OriginTransactionSet)-1].ID()
}

// value returns the value of fulfilling the storage obligation to the host.
func (so storageObligation) value() types.Currency {
	return so.ContractCost.Add(so.PotentialDownloadRevenue).Add(so.PotentialStorageRevenue).Add(so.PotentialUploadRevenue).Add(so.RiskedCollateral)
}

// recentRevision returns the most recent file contract revision in this storage
// obligation.
func (so storageObligation) recentRevision() (types.FileContractRevision, error) {
	numRevisions := len(so.RevisionTransactionSet)
	if numRevisions == 0 {
		return types.FileContractRevision{}, errors.New("Could not get recent revision, there are no revision in the txn set")
	}
	revisionTxn := so.RevisionTransactionSet[numRevisions-1]
	return revisionTxn.FileContractRevisions[0], nil
}

// managedGetStorageObligation fetches a storage obligation from the database.
func (h *Host) managedGetStorageObligation(fcid types.FileContractID) (so storageObligation, err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	err = h.db.View(func(tx *bolt.Tx) error {
		so, err = h.getStorageObligation(tx, fcid)
		return err
	})
	return
}

// deleteStorageObligations deletes obligations from the database. It is assumed
// the deleted obligations don't belong in the database in the first place, so
// no financial metrics are updated.
func (h *Host) deleteStorageObligations(soids []types.FileContractID) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	err := h.db.Update(func(tx *bolt.Tx) error {
		// Delete obligations.
		b := tx.Bucket(bucketStorageObligations)
		for _, soid := range soids {
			err := b.Delete([]byte(soid[:]))
			if err != nil {
				return build.ExtendErr("unable to delete transaction id:", err)
			}
		}
		return nil
	})
	if err != nil {
		h.log.Println(build.ExtendErr("database failed to delete storage obligations:", err))
		return err
	}
	return nil
}

// queueActionItem adds an action item to the host at the input height so that
// the host knows to perform maintenance on the associated storage obligation
// when that height is reached.
func (h *Host) queueActionItem(height types.BlockHeight, id types.FileContractID) error {
	// Sanity check - action item should be at a higher height than the current
	// block height.
	if height <= h.blockHeight {
		h.log.Println("action item queued improperly")
	}
	return h.db.Update(func(tx *bolt.Tx) error {
		// Translate the height into a byte slice.
		heightBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(heightBytes, uint64(height))

		// Get the list of action items already at this height and extend it.
		bai := tx.Bucket(bucketActionItems)
		existingItems := bai.Get(heightBytes)
		var extendedItems = make([]byte, len(existingItems), len(existingItems)+len(id[:]))
		copy(extendedItems, existingItems)
		extendedItems = append(extendedItems, id[:]...)
		return bai.Put(heightBytes, extendedItems)
	})
}

// managedAddStorageObligation adds a storage obligation to the host. Because
// this operation can return errors, the transactions should not be submitted to
// the blockchain until after this function has indicated success. All of the
// sectors that are present in the storage obligation should already be on disk,
// which means that addStorageObligation should be exclusively called when
// creating a new, empty file contract or when renewing an existing file
// contract.
func (h *Host) managedAddStorageObligation(so storageObligation) error {
	var soid types.FileContractID
	err := func() error {
		h.mu.Lock()
		defer h.mu.Unlock()

		// Sanity check - obligation should be under lock while being added.
		soid = so.id()
		_, exists := h.lockedStorageObligations[soid]
		if !exists {
			err := errors.New("addStorageObligation called with an obligation that is not locked")
			h.log.Print(err)
			return err
		}
		// Sanity check - There needs to be enough time left on the file contract
		// for the host to safely submit the file contract revision.
		if h.blockHeight+revisionSubmissionBuffer >= so.expiration() {
			h.log.Critical("submission window was not verified before trying to submit a storage obligation")
			return errNoBuffer
		}
		// Sanity check - the resubmission timeout needs to be smaller than storage
		// proof window.
		if so.expiration()+resubmissionTimeout >= so.proofDeadline() {
			h.log.Critical("host is misconfigured - the storage proof window needs to be long enough to resubmit if needed")
			return errors.New("fill me in")
		}

		// Add the storage obligation information to the database.
		err := h.db.Update(func(tx *bolt.Tx) error {
			// Sanity check - a storage obligation using the same file contract id
			// should not already exist. This situation can happen if the
			// transaction pool ejects a file contract and then a new one is
			// created. Though the file contract will have the same terms, some
			// other conditions might cause problems. The check for duplicate file
			// contract ids should happen during the negotiation phase, and not
			// during the 'addStorageObligation' phase.

			// If the storage obligation already has sectors, it means that the
			// file contract is being renewed, and that the sector should be
			// re-added with a new expiration height. If there is an error at any
			// point, all of the sectors should be removed.
			if len(so.SectorRoots) != 0 {
				err := h.AddSectorBatch(so.SectorRoots)
				if err != nil {
					return err
				}
			}

			// Store the new obligation too.
			return putStorageObligation(tx, so)
		})
		if err != nil {
			return err
		}

		// Update the host financial metrics with regards to this storage
		// obligation.
		h.updateFinancialMetricsAddSO(so)
		return nil
	}()
	if err != nil {
		return err
	}

	// Check that the transaction is fully valid and submit it to the
	// transaction pool.
	// TODO: There is a chance that we crash here before the txn gets submitted.
	// This will result in the obligation existing on disk and the renter not
	// realizing that the host already updated the obligation. The only way to
	// mitigate this is by finding a way to realize that we crashed an
	// resubmitting the transaction set.
	err = h.tpool.AcceptTransactionSet(so.OriginTransactionSet)
	if err != nil {
		h.log.Println("Failed to add storage obligation, transaction set was not accepted:", err)
		return err
	}

	// Queue the action items.
	err = h.managedQueueActionItemsForNewSO(so)
	if err != nil {
		h.log.Println("Error with transaction set, redacting obligation, id", so.id())
		return composeErrors(err, h.removeStorageObligation(so, obligationRejected))
	}
	return nil
}

// managedAddRenewedStorageObligation adds a new obligation to the host and
// modifies the old obligation from which it was renewed from atomically.
func (h *Host) managedAddRenewedStorageObligation(oldSO, newSO storageObligation) error {
	// Sanity check - obligations should be under lock while being modified.
	h.mu.Lock()
	_, exists1 := h.lockedStorageObligations[oldSO.id()]
	_, exists2 := h.lockedStorageObligations[newSO.id()]
	if !exists1 || !exists2 {
		h.mu.Unlock()
		err := errors.New("managedAddRenewedStorageObligation called with an obligation that is not locked")
		h.log.Print(err)
		return err
	}

	// Update the database to contain the new storage obligation.
	var err error
	var oldSOBefore storageObligation
	err = h.db.Update(func(tx *bolt.Tx) error {
		// Get the old storage obligation as a reference to know how to upate
		// the host financial stats.
		oldSOBefore, err = h.getStorageObligation(tx, oldSO.id())
		if err != nil {
			return err
		}

		// Store the obligation to replace the old entry.
		err = putStorageObligation(tx, oldSO)
		if err != nil {
			return err
		}

		// Store the new obligation too.
		err = putStorageObligation(tx, newSO)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		h.mu.Unlock()
		return errors.New("managedAddRenewedStorageObligation: failed to modify oldSO")
	}

	// Update the metrics.
	h.updateFinancialMetricsUpdateSO(oldSOBefore, oldSO)
	h.updateFinancialMetricsAddSO(newSO)
	h.mu.Unlock()

	// Check that the transaction is fully valid and submit it to the
	// transaction pool.
	// TODO: There is a chance that we crash here before the txn gets submitted.
	// This will result in the obligation existing on disk and the renter not
	// realizing that the host already updated the obligation. The only way to
	// mitigate this is by finding a way to realize that we crashed an
	// resubmitting the transaction set.
	err = h.tpool.AcceptTransactionSet(newSO.OriginTransactionSet)
	if err != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		// If we can't submit the txn, we need to undo the changes to the oldSO
		// and remove the newSO from the db.
		err2 := h.db.Update(func(tx *bolt.Tx) error {
			return putStorageObligation(tx, oldSOBefore)
		})
		h.updateFinancialMetricsUpdateSO(oldSO, oldSOBefore)
		err3 := h.removeStorageObligation(newSO, obligationRejected)
		h.log.Println("Failed to add storage obligation, transaction set was not accepted:", errors.Compose(err, err2, err3))
		return err
	}

	// Queue the action items.
	// TODO: When we crash here we might end up with a valid obligation and a
	// renter using that contract but never getting remembered to actually
	// provide a proof since we didn't register in the queue.
	err = h.managedQueueActionItemsForNewSO(newSO)
	if err != nil {
		// If queueing the action items failed, but broadcasting the txn worked,
		// we can only remove the newSO. The txn will still be mined.
		h.log.Println("Error with transaction set, redacting obligation, id", newSO.id())
		return composeErrors(err, h.removeStorageObligation(newSO, obligationRejected))
	}
	return nil
}

// managedQueueActionItemsForNewSO queues the action items for a newly created
// storage obligation.
func (h *Host) managedQueueActionItemsForNewSO(so storageObligation) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	soid := so.id()

	// The file contract was already submitted to the blockchain, need to check
	// after the resubmission timeout that it was submitted successfully.
	err1 := h.queueActionItem(h.blockHeight+resubmissionTimeout, soid)
	err2 := h.queueActionItem(h.blockHeight+resubmissionTimeout*2, soid) // Paranoia
	// Queue an action item to submit the file contract revision - if there is
	// never a file contract revision, the handling of this action item will be
	// a no-op.
	err3 := h.queueActionItem(so.expiration()-revisionSubmissionBuffer, soid)
	err4 := h.queueActionItem(so.expiration()-revisionSubmissionBuffer+resubmissionTimeout, soid) // Paranoia
	// The storage proof should be submitted
	err5 := h.queueActionItem(so.expiration()+resubmissionTimeout, soid)
	err6 := h.queueActionItem(so.expiration()+resubmissionTimeout*2, soid) // Paranoia
	return composeErrors(err1, err2, err3, err4, err5, err6)
}

// updateFinancialMetricsAddSO updates the host's financial metrics for a newly
// added storage obligation.
func (h *Host) updateFinancialMetricsAddSO(so storageObligation) {
	h.financialMetrics.ContractCount++
	h.financialMetrics.PotentialContractCompensation = h.financialMetrics.PotentialContractCompensation.Add(so.ContractCost)
	h.financialMetrics.LockedStorageCollateral = h.financialMetrics.LockedStorageCollateral.Add(so.LockedCollateral)
	h.financialMetrics.PotentialStorageRevenue = h.financialMetrics.PotentialStorageRevenue.Add(so.PotentialStorageRevenue)
	h.financialMetrics.PotentialAccountFunding = h.financialMetrics.PotentialAccountFunding.Add(so.PotentialAccountFunding)
	h.financialMetrics.PotentialDownloadBandwidthRevenue = h.financialMetrics.PotentialDownloadBandwidthRevenue.Add(so.PotentialDownloadRevenue)
	h.financialMetrics.PotentialUploadBandwidthRevenue = h.financialMetrics.PotentialUploadBandwidthRevenue.Add(so.PotentialUploadRevenue)
	h.financialMetrics.RiskedStorageCollateral = h.financialMetrics.RiskedStorageCollateral.Add(so.RiskedCollateral)
	h.financialMetrics.TransactionFeeExpenses = h.financialMetrics.TransactionFeeExpenses.Add(so.TransactionFeesAdded)
}

// updateFinancialMetricsAddSO updates the host's financial metrics for a
// modified storage obligation.
func (h *Host) updateFinancialMetricsUpdateSO(oldSO, newSO storageObligation) {
	// Update the financial information for the storage obligation - apply the
	// new values.
	h.financialMetrics.PotentialContractCompensation = h.financialMetrics.PotentialContractCompensation.Add(newSO.ContractCost)
	h.financialMetrics.LockedStorageCollateral = h.financialMetrics.LockedStorageCollateral.Add(newSO.LockedCollateral)
	h.financialMetrics.PotentialAccountFunding = h.financialMetrics.PotentialAccountFunding.Add(newSO.PotentialAccountFunding)
	h.financialMetrics.PotentialStorageRevenue = h.financialMetrics.PotentialStorageRevenue.Add(newSO.PotentialStorageRevenue)
	h.financialMetrics.PotentialDownloadBandwidthRevenue = h.financialMetrics.PotentialDownloadBandwidthRevenue.Add(newSO.PotentialDownloadRevenue)
	h.financialMetrics.PotentialUploadBandwidthRevenue = h.financialMetrics.PotentialUploadBandwidthRevenue.Add(newSO.PotentialUploadRevenue)
	h.financialMetrics.RiskedStorageCollateral = h.financialMetrics.RiskedStorageCollateral.Add(newSO.RiskedCollateral)
	h.financialMetrics.TransactionFeeExpenses = h.financialMetrics.TransactionFeeExpenses.Add(newSO.TransactionFeesAdded)

	// Update the financial information for the storage obligation - remove the
	// old values.
	h.financialMetrics.PotentialContractCompensation = h.financialMetrics.PotentialContractCompensation.Sub(oldSO.ContractCost)
	h.financialMetrics.LockedStorageCollateral = h.financialMetrics.LockedStorageCollateral.Sub(oldSO.LockedCollateral)
	h.financialMetrics.PotentialAccountFunding = h.financialMetrics.PotentialAccountFunding.Sub(oldSO.PotentialAccountFunding)
	h.financialMetrics.PotentialStorageRevenue = h.financialMetrics.PotentialStorageRevenue.Sub(oldSO.PotentialStorageRevenue)
	h.financialMetrics.PotentialDownloadBandwidthRevenue = h.financialMetrics.PotentialDownloadBandwidthRevenue.Sub(oldSO.PotentialDownloadRevenue)
	h.financialMetrics.PotentialUploadBandwidthRevenue = h.financialMetrics.PotentialUploadBandwidthRevenue.Sub(oldSO.PotentialUploadRevenue)
	h.financialMetrics.RiskedStorageCollateral = h.financialMetrics.RiskedStorageCollateral.Sub(oldSO.RiskedCollateral)
	h.financialMetrics.TransactionFeeExpenses = h.financialMetrics.TransactionFeeExpenses.Sub(oldSO.TransactionFeesAdded)

	// The locked storage collateral was altered, we potentially want to
	// unregister the insufficient collateral budget alert
	h.tryUnregisterInsufficientCollateralBudgetAlert()
}

// managedModifyStorageObligation will take an updated storage obligation along
// with a list of sector changes and update the database to account for all of
// it. The sector modifications are only used to update the sector database,
// they will not be used to modify the storage obligation (most importantly,
// this means that sectorRoots needs to be updated by the calling function).
// Virtual sectors will be removed the number of times that they are listed, to
// remove multiple instances of the same virtual sector, the virtual sector
// will need to appear in 'sectorsRemoved' multiple times. Same with
// 'sectorsGained'.
func (h *Host) managedModifyStorageObligation(so storageObligation, sectorsRemoved []crypto.Hash, sectorsGained map[crypto.Hash][]byte) error {
	// Sanity check - all of the sector data should be modules.SectorSize
	for _, data := range sectorsGained {
		if uint64(len(data)) != modules.SectorSize {
			h.log.Critical("modifying a revision with garbage sector sizes", len(data))
			return errInsaneStorageObligationRevision
		}
	}

	// TODO: remove this once the host was optimized for disk i/o
	// If the contract is too large we delay for a bit to prevent rapid updates
	// from clogging up disk i/o.
	if so.fileSize() >= largeContractSize {
		time.Sleep(largeContractUpdateDelay)
	}

	// Grab a couple of host state facts for sanity checks.
	soid := so.id()
	h.mu.Lock()
	hostHeight := h.blockHeight
	_, exists := h.lockedStorageObligations[soid]
	h.mu.Unlock()
	// Sanity check - obligation should be under lock while being modified.
	if !exists {
		err := errors.New("modifyStorageObligation called with an obligation that is not locked")
		h.log.Print(err)
		return err
	}
	// Sanity check - there needs to be enough time to submit the file contract
	// revision to the blockchain.
	if so.expiration()-revisionSubmissionBuffer <= hostHeight {
		return errNoBuffer
	}

	// Note, for safe error handling, the operation order should be: add
	// sectors, update database, remove sectors. If the adding or update fails,
	// the added sectors should be removed and the storage obligation shoud be
	// considered invalid. If the removing fails, this is okay, it's ignored
	// and left to consistency checks and user actions to fix (will reduce host
	// capacity, but will not inhibit the host's ability to submit storage
	// proofs)
	var added []crypto.Hash
	var err error
	for sectorRoot, data := range sectorsGained {
		err = h.AddSector(sectorRoot, data)
		if err != nil {
			break
		}
		added = append(added, sectorRoot)
	}
	if err != nil {
		// Because there was an error, all of the sectors that got added need
		// to be reverted.
		for _, sectorRoot := range added {
			// Error is not checked because there's nothing useful that can be
			// done about an error.
			_ = h.RemoveSector(sectorRoot)
		}
		return err
	}

	// Lock the host while we update storage obligation and financial metrics.
	h.mu.Lock()
	defer h.mu.Unlock()

	// Update the database to contain the new storage obligation.
	var oldSO storageObligation
	err = h.db.Update(func(tx *bolt.Tx) error {
		// Get the old storage obligation as a reference to know how to upate
		// the host financial stats.
		oldSO, err = h.getStorageObligation(tx, soid)
		if err != nil {
			return err
		}

		// Store the new storage obligation to replace the old one.
		return putStorageObligation(tx, so)
	})
	if err != nil {
		// Because there was an error, all of the sectors that got added need
		// to be reverted.
		for sectorRoot := range sectorsGained {
			// Error is not checked because there's nothing useful that can be
			// done about an error.
			_ = h.RemoveSector(sectorRoot)
		}
		return err
	}
	// Call removeSector for all of the sectors that have been removed.
	for k := range sectorsRemoved {
		// Error is not checkeed because there's nothing useful that can be
		// done about an error. Failing to remove a sector is not a terrible
		// place to be, especially if the host can run consistency checks.
		_ = h.RemoveSector(sectorsRemoved[k])
	}

	// Update the financial information for the storage obligation
	h.updateFinancialMetricsUpdateSO(oldSO, so)
	return nil
}

// PruneStaleStorageObligations will delete storage obligations from the host
// that, for whatever reason, did not make it on the block chain. As these stale
// storage obligations have an impact on the host financial metrics, this method
// updates the host financial metrics to show the correct values.
func (h *Host) PruneStaleStorageObligations() error {
	// Filter the stale obligations from the set of all obligations.
	sos := h.StorageObligations()
	var stale []types.FileContractID
	for _, so := range sos {
		conf, err := h.tpool.TransactionConfirmed(so.TransactionID)
		if err != nil {
			return build.ExtendErr("unable to get transaction ID:", err)
		}
		// An obligation is considered stale if it has not been confirmed
		// within RespendTimeout blocks after negotiation.
		if (h.blockHeight > so.NegotiationHeight+wallet.RespendTimeout) && !conf {
			stale = append(stale, so.ObligationId)
		}
	}
	// Delete stale obligations from the database.
	err := h.deleteStorageObligations(stale)
	if err != nil {
		return build.ExtendErr("unable to delete stale storage ids:", err)
	}
	// Update the financial metrics of the host.
	err = h.resetFinancialMetrics()
	if err != nil {
		h.log.Println(build.ExtendErr("unable to reset host financial metrics:", err))
		return err
	}
	return nil
}

// removeStorageObligation will remove a storage obligation from the host,
// either due to failure or success.
func (h *Host) removeStorageObligation(so storageObligation, sos storageObligationStatus) error {
	// Error is not checked, we want to call remove on every sector even if
	// there are problems - disk health information will be updated.
	_ = h.RemoveSectorBatch(so.SectorRoots)

	// Update the host revenue metrics based on the status of the obligation.
	if sos == obligationUnresolved {
		h.log.Critical("storage obligation 'unresolved' during call to removeStorageObligation, id", so.id())
	}

	if sos == obligationRejected {
		if h.financialMetrics.TransactionFeeExpenses.Cmp(so.TransactionFeesAdded) >= 0 {
			h.financialMetrics.TransactionFeeExpenses = h.financialMetrics.TransactionFeeExpenses.Sub(so.TransactionFeesAdded)

			// Remove the obligation statistics as potential risk and income.
			h.log.Printf("Rejecting storage obligation expiring at block %v, current height is %v. Potential revenue is %v.\n", so.expiration(), h.blockHeight, h.financialMetrics.PotentialContractCompensation.Add(h.financialMetrics.PotentialStorageRevenue).Add(h.financialMetrics.PotentialDownloadBandwidthRevenue).Add(h.financialMetrics.PotentialUploadBandwidthRevenue).Add(h.financialMetrics.PotentialAccountFunding))
			h.financialMetrics.PotentialAccountFunding = h.financialMetrics.PotentialAccountFunding.Sub(so.PotentialAccountFunding)
			h.financialMetrics.PotentialContractCompensation = h.financialMetrics.PotentialContractCompensation.Sub(so.ContractCost)
			h.financialMetrics.LockedStorageCollateral = h.financialMetrics.LockedStorageCollateral.Sub(so.LockedCollateral)
			h.financialMetrics.PotentialStorageRevenue = h.financialMetrics.PotentialStorageRevenue.Sub(so.PotentialStorageRevenue)
			h.financialMetrics.PotentialDownloadBandwidthRevenue = h.financialMetrics.PotentialDownloadBandwidthRevenue.Sub(so.PotentialDownloadRevenue)
			h.financialMetrics.PotentialUploadBandwidthRevenue = h.financialMetrics.PotentialUploadBandwidthRevenue.Sub(so.PotentialUploadRevenue)
			h.financialMetrics.RiskedStorageCollateral = h.financialMetrics.RiskedStorageCollateral.Sub(so.RiskedCollateral)

			// The locked storage collateral was altered, we potentially want to
			// unregister the insufficient collateral budget alert
			h.tryUnregisterInsufficientCollateralBudgetAlert()
		}
	}
	if sos == obligationSucceeded {
		// Some contracts don't require a storage proof. The revenue for such a
		// storage obligation should equal the contract cost of the obligation.
		revenue := so.ContractCost.Add(so.PotentialStorageRevenue).Add(so.PotentialDownloadRevenue).Add(so.PotentialUploadRevenue)
		if !so.requiresProof() {
			h.log.Printf("No need to submit a storage proof for the contract. Revenue is %v.\n", revenue)
		} else {
			h.log.Printf("Successfully submitted a storage proof. Revenue is %v.\n", revenue)
		}

		// Remove the obligation statistics as potential risk and income.
		h.financialMetrics.PotentialAccountFunding = h.financialMetrics.PotentialAccountFunding.Sub(so.PotentialAccountFunding)
		h.financialMetrics.PotentialContractCompensation = h.financialMetrics.PotentialContractCompensation.Sub(so.ContractCost)
		h.financialMetrics.LockedStorageCollateral = h.financialMetrics.LockedStorageCollateral.Sub(so.LockedCollateral)
		h.financialMetrics.PotentialStorageRevenue = h.financialMetrics.PotentialStorageRevenue.Sub(so.PotentialStorageRevenue)
		h.financialMetrics.PotentialDownloadBandwidthRevenue = h.financialMetrics.PotentialDownloadBandwidthRevenue.Sub(so.PotentialDownloadRevenue)
		h.financialMetrics.PotentialUploadBandwidthRevenue = h.financialMetrics.PotentialUploadBandwidthRevenue.Sub(so.PotentialUploadRevenue)
		h.financialMetrics.RiskedStorageCollateral = h.financialMetrics.RiskedStorageCollateral.Sub(so.RiskedCollateral)

		// Add the obligation statistics as actual income.
		h.financialMetrics.AccountFunding = h.financialMetrics.AccountFunding.Add(so.PotentialAccountFunding)
		h.financialMetrics.ContractCompensation = h.financialMetrics.ContractCompensation.Add(so.ContractCost)
		h.financialMetrics.StorageRevenue = h.financialMetrics.StorageRevenue.Add(so.PotentialStorageRevenue)
		h.financialMetrics.DownloadBandwidthRevenue = h.financialMetrics.DownloadBandwidthRevenue.Add(so.PotentialDownloadRevenue)
		h.financialMetrics.UploadBandwidthRevenue = h.financialMetrics.UploadBandwidthRevenue.Add(so.PotentialUploadRevenue)

		// The locked storage collateral was altered, we potentially want to
		// unregister the insufficient collateral budget alert
		h.tryUnregisterInsufficientCollateralBudgetAlert()
	}
	if sos == obligationFailed {
		// Remove the obligation statistics as potential risk and income.
		h.log.Printf("Missed storage proof. Revenue would have been %v.\n", so.ContractCost.Add(so.PotentialStorageRevenue).Add(so.PotentialDownloadRevenue).Add(so.PotentialUploadRevenue).Add(so.PotentialAccountFunding))
		h.financialMetrics.PotentialAccountFunding = h.financialMetrics.PotentialAccountFunding.Sub(so.PotentialAccountFunding)
		h.financialMetrics.PotentialContractCompensation = h.financialMetrics.PotentialContractCompensation.Sub(so.ContractCost)
		h.financialMetrics.LockedStorageCollateral = h.financialMetrics.LockedStorageCollateral.Sub(so.LockedCollateral)
		h.financialMetrics.PotentialStorageRevenue = h.financialMetrics.PotentialStorageRevenue.Sub(so.PotentialStorageRevenue)
		h.financialMetrics.PotentialDownloadBandwidthRevenue = h.financialMetrics.PotentialDownloadBandwidthRevenue.Sub(so.PotentialDownloadRevenue)
		h.financialMetrics.PotentialUploadBandwidthRevenue = h.financialMetrics.PotentialUploadBandwidthRevenue.Sub(so.PotentialUploadRevenue)
		h.financialMetrics.RiskedStorageCollateral = h.financialMetrics.RiskedStorageCollateral.Sub(so.RiskedCollateral)

		// Add the obligation statistics as loss.
		h.financialMetrics.LostStorageCollateral = h.financialMetrics.LostStorageCollateral.Add(so.RiskedCollateral)
		h.financialMetrics.LostRevenue = h.financialMetrics.LostRevenue.Add(so.ContractCost).Add(so.PotentialStorageRevenue).Add(so.PotentialDownloadRevenue).Add(so.PotentialUploadRevenue).Add(so.PotentialAccountFunding)

		// The locked storage collateral was altered, we potentially want to
		// unregister the insufficient collateral budget alert
		h.tryUnregisterInsufficientCollateralBudgetAlert()
	}

	// Update the storage obligation to be finalized but still in-database. The
	// obligation status is updated so that the user can see how the obligation
	// ended up, and the sector roots are removed because they are large
	// objects with little purpose once storage proofs are no longer needed.
	h.financialMetrics.ContractCount--
	so.ObligationStatus = sos
	so.SectorRoots = nil
	return h.db.Update(func(tx *bolt.Tx) error {
		return putStorageObligation(tx, so)
	})
}

// resetFinancialMetrics completely resets the host's financial metrics using
// the storage obligations that are currently present in the hostdb. This
// function is triggered after pruning stale obligations and is a way to ensure
// the financial metrics correctly reflect the host's financial statistics.
func (h *Host) resetFinancialMetrics() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	// Initialize new values for the host financial metrics.
	fm := modules.HostFinancialMetrics{}
	err := h.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketStorageObligations).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var so storageObligation
			if err := json.Unmarshal(v, &so); err != nil {
				return build.ExtendErr("unable to unmarshal storage obligation:", err)
			}

			// Transaction fees are always added.
			fm.TransactionFeeExpenses = fm.TransactionFeeExpenses.Add(so.TransactionFeesAdded)
			// Update the other financial values based on the obligation status.
			if so.ObligationStatus == obligationUnresolved {
				fm.ContractCount++
				fm.PotentialAccountFunding = fm.PotentialAccountFunding.Add(so.PotentialAccountFunding)
				fm.PotentialContractCompensation = fm.PotentialContractCompensation.Add(so.ContractCost)
				fm.LockedStorageCollateral = fm.LockedStorageCollateral.Add(so.LockedCollateral)
				fm.PotentialStorageRevenue = fm.PotentialStorageRevenue.Add(so.PotentialStorageRevenue)
				fm.RiskedStorageCollateral = fm.RiskedStorageCollateral.Add(so.RiskedCollateral)
				fm.PotentialDownloadBandwidthRevenue = fm.PotentialDownloadBandwidthRevenue.Add(so.PotentialDownloadRevenue)
				fm.PotentialUploadBandwidthRevenue = fm.PotentialUploadBandwidthRevenue.Add(so.PotentialUploadRevenue)
			}
			if so.ObligationStatus == obligationSucceeded {
				fm.AccountFunding = fm.AccountFunding.Add(so.PotentialAccountFunding)
				fm.ContractCompensation = fm.ContractCompensation.Add(so.ContractCost)
				fm.StorageRevenue = fm.StorageRevenue.Add(so.PotentialStorageRevenue)
				fm.DownloadBandwidthRevenue = fm.DownloadBandwidthRevenue.Add(so.PotentialDownloadRevenue)
				fm.UploadBandwidthRevenue = fm.UploadBandwidthRevenue.Add(so.PotentialUploadRevenue)
			}
			if so.ObligationStatus == obligationFailed {
				// If there was no risked collateral for the failed obligation,
				// we don't update anything since no revenues were lost. Only
				// the contract compensation and transaction fees are added.
				fm.ContractCompensation = fm.ContractCompensation.Add(so.ContractCost)
				if !so.RiskedCollateral.IsZero() {
					// Storage obligation failed with risked collateral.
					fm.LostRevenue = fm.LostRevenue.Add(so.PotentialStorageRevenue).Add(so.PotentialDownloadRevenue).Add(so.PotentialUploadRevenue).Add(so.PotentialAccountFunding)
					fm.LostStorageCollateral = fm.LostStorageCollateral.Add(so.RiskedCollateral)
				}
			}
		}
		return nil
	})
	if err != nil {
		h.log.Println(build.ExtendErr("unable to reset host financial metrics:", err))
		return err
	}
	h.financialMetrics = fm
	return nil
}

// threadedHandleActionItem will look at a storage obligation and determine
// which action is necessary for the storage obligation to succeed.
func (h *Host) threadedHandleActionItem(soid types.FileContractID) {
	err := h.tg.Add()
	if err != nil {
		return
	}
	defer h.tg.Done()

	// Lock the storage obligation in question.
	h.managedLockStorageObligation(soid)
	defer func() {
		h.managedUnlockStorageObligation(soid)
	}()

	// Fetch the storage obligation associated with the storage obligation id.
	var so storageObligation
	h.mu.RLock()
	blockHeight := h.blockHeight
	err = h.db.View(func(tx *bolt.Tx) error {
		so, err = h.getStorageObligation(tx, soid)
		return err
	})
	h.mu.RUnlock()
	if err != nil {
		h.log.Println("Could not get storage obligation:", err)
		return
	}

	// Check whether the storage obligation has already been completed.
	if so.ObligationStatus != obligationUnresolved {
		// Storage obligation has already been completed, skip action item.
		return
	}

	// Check whether the file contract has been seen. If not, resubmit and
	// queue another action item. Check for death. (signature should have a
	// kill height)
	if !so.OriginConfirmed {
		// Submit the transaction set again, try to get the transaction
		// confirmed.
		err := h.tpool.AcceptTransactionSet(so.OriginTransactionSet)
		if err != nil {
			h.log.Debugln("Could not get origin transaction set accepted", err)

			// Check if the transaction is invalid with the current consensus set.
			// If so, the transaction is highly unlikely to ever be confirmed, and
			// the storage obligation should be removed. This check should come
			// after logging the errror so that the function can quit.
			//
			// TODO: If the host or tpool is behind consensus, might be difficult
			// to have certainty about the issue. If some but not all of the
			// parents are confirmed, might be some difficulty.
			_, t := err.(modules.ConsensusConflict)
			if t {
				h.log.Println("Consensus conflict on the origin transaction set, id", so.id())
				h.mu.Lock()
				err = h.removeStorageObligation(so, obligationRejected)
				h.mu.Unlock()
				if err != nil {
					h.log.Println("Error removing storage obligation:", err)
				}
				return
			}
		}

		// Queue another action item to check the status of the transaction.
		h.mu.Lock()
		err = h.queueActionItem(h.blockHeight+resubmissionTimeout, so.id())
		h.mu.Unlock()
		if err != nil {
			h.log.Println("Error queuing action item:", err)
		}
	}

	// Check if the file contract revision is ready for submission. Check for death.
	if !so.RevisionConfirmed && len(so.RevisionTransactionSet) > 0 && blockHeight >= so.expiration()-revisionSubmissionBuffer {
		// Sanity check - there should be a file contract revision.
		rtsLen := len(so.RevisionTransactionSet)
		if rtsLen < 1 || len(so.RevisionTransactionSet[rtsLen-1].FileContractRevisions) != 1 {
			h.log.Critical("transaction revision marked as unconfirmed, yet there is no transaction revision")
			return
		}

		// Check if the revision has failed to submit correctly.
		if blockHeight > so.expiration() {
			// TODO: Check this error.
			//
			// TODO: this is not quite right, because a previous revision may
			// be confirmed, and the origin transaction may be confirmed, which
			// would confuse the revenue stuff a bit. Might happen frequently
			// due to the dynamic fee pool.
			h.log.Println("Full time has elapsed, but the revision transaction could not be submitted to consensus, id", so.id())
			h.mu.Lock()
			h.removeStorageObligation(so, obligationRejected)
			h.mu.Unlock()
			return
		}

		// Queue another action item to check the status of the transaction.
		h.mu.Lock()
		err := h.queueActionItem(blockHeight+resubmissionTimeout, so.id())
		h.mu.Unlock()
		if err != nil {
			h.log.Println("Error queuing action item:", err)
		}

		// Add a miner fee to the transaction and submit it to the blockchain.
		revisionTxnIndex := len(so.RevisionTransactionSet) - 1
		revisionParents := so.RevisionTransactionSet[:revisionTxnIndex]
		revisionTxn := so.RevisionTransactionSet[revisionTxnIndex]
		builder, err := h.wallet.RegisterTransaction(revisionTxn, revisionParents)
		if err != nil {
			h.log.Println("Error registering transaction:", err)
			return
		}
		_, feeRecommendation := h.tpool.FeeEstimation()
		if so.value().Div64(2).Cmp(feeRecommendation) < 0 {
			// There's no sense submitting the revision if the fee is more than
			// half of the anticipated revenue - fee market went up
			// unexpectedly, and the money that the renter paid to cover the
			// fees is no longer enough.
			builder.Drop()
			return
		}
		txnSize := uint64(len(encoding.MarshalAll(so.RevisionTransactionSet)) + txnFeeSizeBuffer)
		requiredFee := feeRecommendation.Mul64(txnSize)
		err = builder.FundUplocoins(requiredFee)
		if err != nil {
			h.log.Println("Error funding transaction fees", err)
			builder.Drop()
		}
		builder.AddMinerFee(requiredFee)
		if err != nil {
			h.log.Println("Error adding miner fees", err)
			builder.Drop()
		}
		feeAddedRevisionTransactionSet, err := builder.Sign(true)
		if err != nil {
			h.log.Println("Error signing transaction", err)
			builder.Drop()
		}
		err = h.tpool.AcceptTransactionSet(feeAddedRevisionTransactionSet)
		if err != nil {
			h.log.Println("Error submitting transaction to transaction pool", err)
			builder.Drop()
		}
		so.TransactionFeesAdded = so.TransactionFeesAdded.Add(requiredFee)
		// return
	}

	// Check whether a storage proof is ready to be provided, and whether it
	// has been accepted. Check for death.
	if !so.ProofConfirmed && blockHeight >= so.expiration()+resubmissionTimeout {
		h.log.Debugln("Host is attempting a storage proof for", so.id())

		// If the obligation doesn't require a proof, we can remove the
		// obligation and avoid submitting a storage proof. In that case the
		// host payout for the contract includes the contract cost and locked
		// collateral.
		if !so.requiresProof() {
			h.log.Debugln("storage proof not submitted for unrevised contract, id", so.id())
			h.mu.Lock()
			err := h.removeStorageObligation(so, obligationSucceeded)
			h.mu.Unlock()
			if err != nil {
				h.log.Println("Error removing storage obligation:", err)
			}
			return
		}
		// If the window has closed, the host has failed and the obligation can
		// be removed.
		if so.proofDeadline() < blockHeight {
			h.log.Debugln("storage proof not confirmed by deadline, id", so.id())
			h.mu.Lock()
			err := h.removeStorageObligation(so, obligationFailed)
			h.mu.Unlock()
			if err != nil {
				h.log.Println("Error removing storage obligation:", err)
			}
			return
		}

		// Get the index of the segment for which to build the proof.
		segmentIndex, err := h.cs.StorageProofSegment(so.id())
		if err != nil {
			h.log.Debugln("Host got an error when fetching a storage proof segment:", err)
			return
		}

		// Build StorageProof.
		sp, err := h.managedBuildStorageProof(so, segmentIndex)
		if err != nil {
			h.log.Debugln("Host encountered an error when building the storage proof", err)
			return
		}

		// Create and build the transaction with the storage proof.
		builder, err := h.wallet.StartTransaction()
		if err != nil {
			h.log.Println("Failed to start transaction:", err)
			return
		}
		_, feeRecommendation := h.tpool.FeeEstimation()
		txnSize := uint64(len(encoding.Marshal(sp)) + txnFeeSizeBuffer)
		requiredFee := feeRecommendation.Mul64(txnSize)
		if so.value().Cmp(requiredFee) < 0 {
			// There's no sense submitting the storage proof if the fee is more
			// than the anticipated revenue.
			h.log.Debugln("Host not submitting storage proof due to a value that does not sufficiently exceed the fee cost")
			builder.Drop()
			return
		}
		err = builder.FundUplocoins(requiredFee)
		if err != nil {
			h.log.Println("Host error when funding a storage proof transaction fee:", err)
			builder.Drop()
			return
		}
		builder.AddMinerFee(requiredFee)
		builder.AddStorageProof(sp)
		storageProofSet, err := builder.Sign(true)
		if err != nil {
			h.log.Println("Host error when signing the storage proof transaction:", err)
			builder.Drop()
			return
		}
		err = h.tpool.AcceptTransactionSet(storageProofSet)
		if err != nil {
			h.log.Println("Host unable to submit storage proof transaction to transaction pool:", err)
			builder.Drop()
			return
		}
		so.TransactionFeesAdded = so.TransactionFeesAdded.Add(requiredFee)

		// Queue another action item to check whether the storage proof
		// got confirmed.
		h.mu.Lock()
		err = h.queueActionItem(so.proofDeadline(), so.id())
		h.mu.Unlock()
		if err != nil {
			h.log.Println("Error queuing action item:", err)
		}
	}

	// Save the storage obligation to account for any fee changes.
	err = h.db.Update(func(tx *bolt.Tx) error {
		soBytes, err := json.Marshal(so)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketStorageObligations).Put(soid[:], soBytes)
	})
	if err != nil {
		h.log.Println("Error updating the storage obligations", err)
	}

	// Check if all items have succeeded with the required confirmations. Report
	// success, delete the obligation.
	if so.ProofConfirmed && blockHeight >= so.proofDeadline() {
		h.log.Println("file contract complete, id", so.id())
		h.mu.Lock()
		h.removeStorageObligation(so, obligationSucceeded)
		h.mu.Unlock()
	}
}

// managedBuildStorageProof builds a storage proof for a given storageObligation
// for the host to submit.
func (h *Host) managedBuildStorageProof(so storageObligation, segmentIndex uint64) (types.StorageProof, error) {
	// Handle empty contract edge case.
	if len(so.SectorRoots) == 0 {
		return types.StorageProof{
			ParentID: so.id(),
		}, nil
	}
	sectorIndex := segmentIndex / (modules.SectorSize / crypto.SegmentSize)
	// Pull the corresponding sector into memory.
	sectorRoot := so.SectorRoots[sectorIndex]
	sectorBytes, err := h.ReadSector(sectorRoot)
	if err != nil {
		return types.StorageProof{}, errors.AddContext(err, "managedBuildStorageProof: failed to read sector")
	}

	// Build the storage proof for just the sector.
	sectorSegment := segmentIndex % (modules.SectorSize / crypto.SegmentSize)
	base, cachedHashSet := crypto.MerkleProof(sectorBytes, sectorSegment)

	// Using the sector, build a cached root.
	log2SectorSize := uint64(0)
	for 1<<log2SectorSize < (modules.SectorSize / crypto.SegmentSize) {
		log2SectorSize++
	}
	ct := crypto.NewCachedTree(log2SectorSize)
	ct.SetIndex(segmentIndex)
	for _, root := range so.SectorRoots {
		if err := ct.PushSubTree(0, root); err != nil {
			return types.StorageProof{}, errors.AddContext(err, "managedBuildStorageProof: failed to push subtree")
		}
	}
	hashSet := ct.Prove(base, cachedHashSet)
	sp := types.StorageProof{
		ParentID: so.id(),
		HashSet:  hashSet,
	}
	copy(sp.Segment[:], base)
	return sp, nil
}

// StorageObligations fetches the set of storage obligations in the host and
// returns metadata on them.
func (h *Host) StorageObligations() (sos []modules.StorageObligation) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	err := h.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketStorageObligations)
		err := b.ForEach(func(idBytes, soBytes []byte) error {
			var so storageObligation
			err := json.Unmarshal(soBytes, &so)
			if err != nil {
				return build.ExtendErr("unable to unmarshal storage obligation:", err)
			}

			valid, missed := so.payouts()
			mso := modules.StorageObligation{
				ContractCost:             so.ContractCost,
				DataSize:                 so.fileSize(),
				RevisionNumber:           so.revisionNumber(),
				LockedCollateral:         so.LockedCollateral,
				ObligationId:             so.id(),
				PotentialAccountFunding:  so.PotentialAccountFunding,
				PotentialDownloadRevenue: so.PotentialDownloadRevenue,
				PotentialStorageRevenue:  so.PotentialStorageRevenue,
				PotentialUploadRevenue:   so.PotentialUploadRevenue,
				RiskedCollateral:         so.RiskedCollateral,
				SectorRootsCount:         uint64(len(so.SectorRoots)),
				TransactionFeesAdded:     so.TransactionFeesAdded,
				TransactionID:            so.transactionID(),

				ExpirationHeight:  so.expiration(),
				NegotiationHeight: so.NegotiationHeight,
				ProofDeadLine:     so.proofDeadline(),

				ObligationStatus:    so.ObligationStatus.String(),
				OriginConfirmed:     so.OriginConfirmed,
				ProofConfirmed:      so.ProofConfirmed,
				ProofConstructed:    so.ProofConstructed,
				RevisionConfirmed:   so.RevisionConfirmed,
				RevisionConstructed: so.RevisionConstructed,

				ValidProofOutputs:  valid,
				MissedProofOutputs: missed,
			}

			sos = append(sos, mso)
			return nil
		})
		if err != nil {
			return build.ExtendErr("ForEach failed to get next storage obligation:", err)
		}
		return nil
	})
	if err != nil {
		h.log.Println(build.ExtendErr("database failed to provide storage obligations:", err))
	}

	return sos
}
