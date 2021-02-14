package modules

import (
	"errors"
	"io"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

const (
	// ConsensusDir is the name of the directory used for all of the consensus
	// persistence files.
	ConsensusDir = "consensus"

	// DiffApply indicates that a diff is being applied to the consensus set.
	DiffApply DiffDirection = true

	// DiffRevert indicates that a diff is being reverted from the consensus
	// set.
	DiffRevert DiffDirection = false
)

var (
	// ConsensusChangeBeginning is a special consensus change id that tells the
	// consensus set to provide all consensus changes starting from the very
	// first diff, which includes the genesis block diff.
	ConsensusChangeBeginning = ConsensusChangeID{}

	// ConsensusChangeRecent is a special consensus change id that tells the
	// consensus set to provide the most recent consensus change, instead of
	// starting from a specific value (which may not be known to the caller).
	ConsensusChangeRecent = ConsensusChangeID{1}

	// ErrBlockKnown is an error indicating that a block is already in the
	// database.
	ErrBlockKnown = errors.New("block already present in database")

	// ErrBlockUnsolved indicates that a block did not meet the required POW
	// target.
	ErrBlockUnsolved = errors.New("block does not meet target")

	// ErrInvalidConsensusChangeID indicates that ConsensusSetPersistSubscribe
	// was called with a consensus change id that is not recognized. Most
	// commonly, this means that the consensus set was deleted or replaced and
	// now the module attempting the subscription has desynchronized. This error
	// should be handled by the module, and not reported to the user.
	ErrInvalidConsensusChangeID = errors.New("consensus subscription has invalid id - files are inconsistent")

	// ErrNonExtendingBlock indicates that a block is valid but does not result
	// in a fork that is the heaviest known fork - the consensus set has not
	// changed as a result of seeing the block.
	ErrNonExtendingBlock = errors.New("block does not extend the longest fork")
)

type (
	// ConsensusChangeID is the id of a consensus change.
	ConsensusChangeID crypto.Hash

	// A DiffDirection indicates the "direction" of a diff, either applied or
	// reverted. A bool is used to restrict the value to these two possibilities.
	DiffDirection bool

	// A ConsensusSetSubscriber is an object that receives updates to the consensus
	// set every time there is a change in consensus.
	ConsensusSetSubscriber interface {
		// ProcessConsensusChange sends a consensus update to a module through
		// a function call. Updates will always be sent in the correct order.
		// There may not be any reverted blocks, but there will always be
		// applied blocks.
		ProcessConsensusChange(ConsensusChange)
	}

	// ConsensusChangeDiffs is a collection of diffs caused by a single block.
	// If the block was reverted, the individual diff directions are inverted.
	// For example, a block that spends an output and creates a miner payout
	// would have one UplocoinOutputDiff with direction DiffRevert and one
	// DelayedUplocoinOutputDiff with direction DiffApply. If the same block
	// were reverted, the SCOD would have direction DiffApply and the DSCOD
	// would have direction DiffRevert.
	ConsensusChangeDiffs struct {
		UplocoinOutputDiffs        []UplocoinOutputDiff
		FileContractDiffs         []FileContractDiff
		UplofundOutputDiffs        []UplofundOutputDiff
		DelayedUplocoinOutputDiffs []DelayedUplocoinOutputDiff
		UplofundPoolDiffs          []UplofundPoolDiff
	}

	// A ConsensusChange enumerates a set of changes that occurred to the consensus set.
	ConsensusChange struct {
		// ID is a unique id for the consensus change derived from the reverted
		// and applied blocks.
		ID ConsensusChangeID

		// RevertedBlocks is the list of blocks that were reverted by the change.
		// The reverted blocks were always all reverted before the applied blocks
		// were applied. The revered blocks are presented in the order that they
		// were reverted.
		RevertedBlocks []types.Block

		// AppliedBlocks is the list of blocks that were applied by the change. The
		// applied blocks are always all applied after all the reverted blocks were
		// reverted. The applied blocks are presented in the order that they were
		// applied.
		AppliedBlocks []types.Block

		// RevertedDiffs is the set of diffs caused by reverted blocks. Each
		// element corresponds to a block in RevertedBlocks.
		RevertedDiffs []ConsensusChangeDiffs

		// AppliedDiffs is the set of diffs caused by applied blocks. Each
		// element corresponds to a block in AppliedBlocks.
		AppliedDiffs []ConsensusChangeDiffs

		// ConsensusChangeDiffs is the concatenation of all RevertedDiffs and
		// AppliedDiffs.
		ConsensusChangeDiffs

		// ChildTarget defines the target of any block that would be the child
		// of the block most recently appended to the consensus set.
		ChildTarget types.Target

		// MinimumValidChildTimestamp defines the minimum allowed timestamp for
		// any block that is the child of the block most recently appended to
		// the consensus set.
		MinimumValidChildTimestamp types.Timestamp

		// Synced indicates whether or not the ConsensusSet is synced with its
		// peers.
		Synced bool

		// TryTransactionSet is an unlocked version of
		// ConsensusSet.TryTransactionSet. This allows the TryTransactionSet
		// function to be called by a subscriber during
		// ProcessConsensusChange.
		TryTransactionSet func([]types.Transaction) (ConsensusChange, error)
	}

	// A UplocoinOutputDiff indicates the addition or removal of a UplocoinOutput in
	// the consensus set.
	UplocoinOutputDiff struct {
		Direction     DiffDirection
		ID            types.UplocoinOutputID
		UplocoinOutput types.UplocoinOutput
	}

	// A FileContractDiff indicates the addition or removal of a FileContract in
	// the consensus set.
	FileContractDiff struct {
		Direction    DiffDirection
		ID           types.FileContractID
		FileContract types.FileContract
	}

	// A UplofundOutputDiff indicates the addition or removal of a UplofundOutput in
	// the consensus set.
	UplofundOutputDiff struct {
		Direction     DiffDirection
		ID            types.UplofundOutputID
		UplofundOutput types.UplofundOutput
	}

	// A DelayedUplocoinOutputDiff indicates the introduction of a Uplocoin output
	// that cannot be spent until after maturing for 144 blocks. When the output
	// has matured, a UplocoinOutputDiff will be provided.
	DelayedUplocoinOutputDiff struct {
		Direction      DiffDirection
		ID             types.UplocoinOutputID
		UplocoinOutput  types.UplocoinOutput
		MaturityHeight types.BlockHeight
	}

	// A UplofundPoolDiff contains the value of the uplofundPool before the block
	// was applied, and after the block was applied. When applying the diff, set
	// uplofundPool to 'Adjusted'. When reverting the diff, set uplofundPool to
	// 'Previous'.
	UplofundPoolDiff struct {
		Direction DiffDirection
		Previous  types.Currency
		Adjusted  types.Currency
	}

	// A ConsensusSet accepts blocks and builds an understanding of network
	// consensus.
	ConsensusSet interface {
		Alerter

		// AcceptBlock adds a block to consensus. An error will be returned if the
		// block is invalid, has been seen before, is an orphan, or doesn't
		// contribute to the heaviest fork known to the consensus set. If the block
		// does not become the head of the heaviest known fork but is otherwise
		// valid, it will be remembered by the consensus set but an error will
		// still be returned.
		AcceptBlock(types.Block) error

		// BlockAtHeight returns the block found at the input height, with a
		// bool to indicate whether that block exists.
		BlockAtHeight(types.BlockHeight) (types.Block, bool)

		// BlocksByID returns a block found for a given ID and its height, with
		// a bool to indicate whether that block exists.
		BlockByID(types.BlockID) (types.Block, types.BlockHeight, bool)

		// ChildTarget returns the target required to extend the current heaviest
		// fork. This function is typically used by miners looking to extend the
		// heaviest fork.
		ChildTarget(types.BlockID) (types.Target, bool)

		// Close will shut down the consensus set, giving the module enough time to
		// run any required closing routines.
		Close() error

		// ConsensusSetSubscribe adds a subscriber to the list of subscribers
		// and gives them every consensus change that has occurred since the
		// change with the provided id. There are a few special cases,
		// described by the ConsensusChangeX variables in this package.
		// A channel can be provided to abort the subscription process.
		ConsensusSetSubscribe(ConsensusSetSubscriber, ConsensusChangeID, <-chan struct{}) error

		// CurrentBlock returns the latest block in the heaviest known
		// blockchain.
		CurrentBlock() types.Block

		// Height returns the current height of consensus.
		Height() types.BlockHeight

		// Synced returns true if the consensus set is synced with the network.
		Synced() bool

		// InCurrentPath returns true if the block id presented is found in the
		// current path, false otherwise.
		InCurrentPath(types.BlockID) bool

		// MinimumValidChildTimestamp returns the earliest timestamp that is
		// valid on the current longest fork according to the consensus set. This is
		// a required piece of information for the miner, who could otherwise be at
		// risk of mining invalid blocks.
		MinimumValidChildTimestamp(types.BlockID) (types.Timestamp, bool)

		// StorageProofSegment returns the segment to be used in the storage proof for
		// a given file contract.
		StorageProofSegment(types.FileContractID) (uint64, error)

		// FoundationUnlockHashes returns the current primary and failsafe
		// Foundation UnlockHashes.
		FoundationUnlockHashes() (primary, failsafe types.UnlockHash)

		// TryTransactionSet checks whether the transaction set would be valid if
		// it were added in the next block. A consensus change is returned
		// detailing the diffs that would result from the application of the
		// transaction.
		TryTransactionSet([]types.Transaction) (ConsensusChange, error)

		// Unsubscribe removes a subscriber from the list of subscribers,
		// allowing for garbage collection and rescanning. If the subscriber is
		// not found in the subscriber database, no action is taken.
		Unsubscribe(ConsensusSetSubscriber)
	}
)

// AppendDiffs appends a set of diffs to cc.
func (cc *ConsensusChange) AppendDiffs(diffs ConsensusChangeDiffs) {
	cc.UplocoinOutputDiffs = append(cc.UplocoinOutputDiffs, diffs.UplocoinOutputDiffs...)
	cc.FileContractDiffs = append(cc.FileContractDiffs, diffs.FileContractDiffs...)
	cc.UplofundOutputDiffs = append(cc.UplofundOutputDiffs, diffs.UplofundOutputDiffs...)
	cc.DelayedUplocoinOutputDiffs = append(cc.DelayedUplocoinOutputDiffs, diffs.DelayedUplocoinOutputDiffs...)
	cc.UplofundPoolDiffs = append(cc.UplofundPoolDiffs, diffs.UplofundPoolDiffs...)
}

// MarshalUplo implements encoding.UploMarshaler.
func (cc ConsensusChange) MarshalUplo(w io.Writer) error {
	return encoding.NewEncoder(w).EncodeAll(
		cc.ID,
		cc.RevertedBlocks,
		cc.AppliedBlocks,
		cc.RevertedDiffs,
		cc.AppliedDiffs,
		cc.ChildTarget,
		cc.MinimumValidChildTimestamp,
		cc.Synced,
	)
}

// UnmarshalUplo implements encoding.UploUnmarshaler.
func (cc *ConsensusChange) UnmarshalUplo(r io.Reader) error {
	const maxSize = 100e6 // consensus changes can be arbitrarily large
	err := encoding.NewDecoder(r, maxSize).DecodeAll(
		&cc.ID,
		&cc.RevertedBlocks,
		&cc.AppliedBlocks,
		&cc.RevertedDiffs,
		&cc.AppliedDiffs,
		&cc.ChildTarget,
		&cc.MinimumValidChildTimestamp,
		&cc.Synced,
	)
	if err != nil {
		return err
	}
	// reconstruct diffs
	for _, diffs := range cc.RevertedDiffs {
		cc.AppendDiffs(diffs)
	}
	for _, diffs := range cc.AppliedDiffs {
		cc.AppendDiffs(diffs)
	}
	return nil
}

// String returns the ConsensusChangeID as a string.
func (ccID ConsensusChangeID) String() string {
	return crypto.Hash(ccID).String()
}
