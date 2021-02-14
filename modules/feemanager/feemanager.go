package feemanager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/threadgroup"
)

var (
	// Nil dependency errors.
	errNilCS     = errors.New("cannot create FeeManager with nil consensus set")
	errNilDeps   = errors.New("cannot create FeeManager with nil dependencies")
	errNilTpool  = errors.New("cannot create FeeManager with nil tpool")
	errNilWallet = errors.New("cannot create FeeManager with nil wallet")

	// Enforce that FeeManager satisfies the modules.FeeManager interface.
	_ modules.FeeManager = (*FeeManager)(nil)
)

var (
	// ErrFeeNotFound is returned if a fee is not found in the FeeManager
	ErrFeeNotFound = errors.New("fee not found")
)

var (
	// PayoutInterval is the interval at which the payoutheight is set in the
	// future
	PayoutInterval = build.Select(build.Var{
		Standard: types.BlocksPerMonth,
		Dev:      types.BlocksPerDay,
		Testing:  types.BlocksPerHour,
	}).(types.BlockHeight)
)

type (
	// FeeManager is responsible for tracking any application fees that are
	// being charged to this uplod instance
	FeeManager struct {
		// fees are all the fees that are currently charging this uplod instance
		fees map[modules.FeeUID]*modules.AppFee

		staticCommon *feeManagerCommon
		mu           sync.RWMutex
	}

	// feeManagerCommon contains fields that are common to all of the subsystems
	// in the fee manager.
	feeManagerCommon struct {
		// Dependencies
		staticCS     modules.ConsensusSet
		staticTpool  modules.TransactionPool
		staticWallet modules.Wallet

		// Subsystems
		staticPersist  *persistSubsystem
		staticWatchdog *watchdog

		// Utilities
		staticDeps modules.Dependencies
		staticLog  *persist.Logger
		staticTG   threadgroup.ThreadGroup
	}
)

// New creates a new FeeManager.
func New(cs modules.ConsensusSet, tp modules.TransactionPool, w modules.Wallet, persistDir string) (*FeeManager, error) {
	return NewCustomFeeManager(cs, tp, w, persistDir, modules.ProdDependencies)
}

// NewCustomFeeManager creates a new FeeManager using custom dependencies.
func NewCustomFeeManager(cs modules.ConsensusSet, tp modules.TransactionPool, w modules.Wallet, persistDir string, deps modules.Dependencies) (*FeeManager, error) {
	// Check for nil inputs
	if cs == nil {
		return nil, errNilCS
	}
	if deps == nil {
		return nil, errNilDeps
	}
	if tp == nil {
		return nil, errNilTpool
	}
	if w == nil {
		return nil, errNilWallet
	}

	// Create the persist directory.
	err := os.MkdirAll(persistDir, modules.DefaultDirPerm)
	if err != nil {
		return nil, errors.AddContext(err, "unable to make fee manager persist directory")
	}

	// Create the common struct.
	common := &feeManagerCommon{
		staticCS:     cs,
		staticTpool:  tp,
		staticWallet: w,

		staticDeps: deps,
	}
	// Create FeeManager
	fm := &FeeManager{
		fees: make(map[modules.FeeUID]*modules.AppFee),

		staticCommon: common,
	}
	// Create the persist subsystem.
	ps := &persistSubsystem{
		partialTxns: make(map[types.TransactionID]partialTransactions),

		staticCommon:     common,
		staticPersistDir: persistDir,
	}
	common.staticPersist = ps
	// Create the sync coordinator
	sc := &syncCoordinator{
		staticCommon: common,
	}
	ps.staticSyncCoordinator = sc
	// Create the watchdog
	wd := &watchdog{
		feeUIDToTxnID: make(map[modules.FeeUID]types.TransactionID),
		txns:          make(map[types.TransactionID]trackedTransaction),
		staticCommon:  common,
	}
	common.staticWatchdog = wd

	// Initialize the logger.
	common.staticLog, err = persist.NewFileLogger(filepath.Join(ps.staticPersistDir, logFile))
	if err != nil {
		return nil, errors.AddContext(err, "unable to create logger")
	}
	if err := common.staticTG.AfterStop(common.staticLog.Close); err != nil {
		tgErr := errors.AddContext(err, "unable to set up an AfterStop to close logger")
		return nil, errors.Compose(tgErr, common.staticLog.Close())
	}

	// Initialize the FeeManager persistence
	err = fm.callInitPersist()
	if err != nil {
		return nil, errors.AddContext(err, "unable to initialize the FeeManager's persistence")
	}

	// Launch background threads
	go fm.threadedProcessFees()
	go fm.threadedMonitorTransactions()

	return fm, nil
}

// feeNotFoundError returns the ErrFeeNotFound error composed with an error
// including the fee's UID
func feeNotFoundError(feeUID modules.FeeUID) error {
	return errors.Compose(ErrFeeNotFound, fmt.Errorf("feeUID %v", feeUID))
}

// uniqueID creates a random unique FeeUID.
func uniqueID() modules.FeeUID {
	return modules.FeeUID(persist.UID())
}

// AddFee adds a fee to the fee manager.
func (fm *FeeManager) AddFee(address types.UnlockHash, amount types.Currency, appUID modules.AppUID, recurring bool) (modules.FeeUID, error) {
	if err := fm.staticCommon.staticTG.Add(); err != nil {
		return "", err
	}
	defer fm.staticCommon.staticTG.Done()
	ps := fm.staticCommon.staticPersist

	// Determine the payoutHeight, payoutHeight will be 0 if the consensus is
	// not synced
	payoutHeight := types.BlockHeight(0)
	if fm.staticCommon.staticCS.Synced() {
		// Consensus is synced, set to the following payout period
		ps.mu.Lock()
		payoutHeight = ps.nextPayoutHeight + PayoutInterval
		ps.mu.Unlock()
	}

	// Create the fee.
	fee := modules.AppFee{
		Address:            address,
		Amount:             amount,
		AppUID:             appUID,
		PaymentCompleted:   false,
		PayoutHeight:       payoutHeight,
		Recurring:          recurring,
		Timestamp:          time.Now().Unix(),
		TransactionCreated: false,
		FeeUID:             uniqueID(),
	}

	// Persist the fee.
	err := ps.callPersistNewFee(fee)
	if err != nil {
		return "", errors.AddContext(err, "unable to persist the new fee")
	}

	// Add the fee once the persist event was successful. Don't need to check
	// for existence because we just generated a unique ID.
	fm.mu.Lock()
	fm.fees[fee.FeeUID] = &fee
	fm.mu.Unlock()
	return fee.FeeUID, nil
}

// CancelFee cancels a fee by removing it from the FeeManager's map
func (fm *FeeManager) CancelFee(feeUID modules.FeeUID) error {
	// Add thread group
	if err := fm.staticCommon.staticTG.Add(); err != nil {
		return err
	}
	defer fm.staticCommon.staticTG.Done()

	// Check if the fee can be canceled
	fm.mu.Lock()
	fee, exists := fm.fees[feeUID]
	if !exists {
		fm.mu.Unlock()
		return feeNotFoundError(feeUID)
	}
	if fee.PaymentCompleted {
		fm.mu.Unlock()
		return errors.New("Cannot cancel a fee if the payment has already been completed")
	}
	if fee.TransactionCreated {
		fm.mu.Unlock()
		return errors.New("Cannot cancel a fee if the transaction has already been created")
	}
	_, tracked := fm.staticCommon.staticWatchdog.callFeeTracked(feeUID)
	if tracked {
		fm.mu.Unlock()
		return errors.New("Cannot cancel a fee that is being tracked by the watchdog")
	}

	// Erase the fee from memory.
	delete(fm.fees, feeUID)
	fm.mu.Unlock()

	// Mark a cancellation of the fee on disk.
	err := fm.staticCommon.staticPersist.callPersistFeeCancelation(feeUID)
	if err != nil {
		// Revert in memory change due to error with fee cancelation persistence
		fm.mu.Lock()
		fm.fees[feeUID] = fee
		fm.mu.Unlock()
		return errors.AddContext(err, "unable to persist fee cancelation")
	}
	return nil
}

// Close closes the FeeManager
func (fm *FeeManager) Close() error {
	return fm.staticCommon.staticTG.Stop()
}

// PaidFees returns all the paid fees that are being tracked by the FeeManager
func (fm *FeeManager) PaidFees() ([]modules.AppFee, error) {
	// Add thread group
	if err := fm.staticCommon.staticTG.Add(); err != nil {
		return nil, err
	}
	defer fm.staticCommon.staticTG.Done()

	var paidFees []modules.AppFee
	fm.mu.Lock()
	for _, fee := range fm.fees {
		if fee.PaymentCompleted {
			paidFees = append(paidFees, *fee)
		}
	}
	fm.mu.Unlock()
	// Sort by timestamp.
	sort.Sort(modules.AppFeeByTimestamp(paidFees))

	return paidFees, nil
}

// PayoutHeight returns the nextPayoutHeight of the FeeManager
func (fm *FeeManager) PayoutHeight() (types.BlockHeight, error) {
	if err := fm.staticCommon.staticTG.Add(); err != nil {
		return 0, err
	}
	defer fm.staticCommon.staticTG.Done()

	fm.staticCommon.staticPersist.mu.Lock()
	defer fm.staticCommon.staticPersist.mu.Unlock()
	return fm.staticCommon.staticPersist.nextPayoutHeight, nil
}

// PendingFees returns all the pending fees that are being tracked by the
// FeeManager
func (fm *FeeManager) PendingFees() ([]modules.AppFee, error) {
	// Add thread group
	if err := fm.staticCommon.staticTG.Add(); err != nil {
		return nil, err
	}
	defer fm.staticCommon.staticTG.Done()

	var pendingFees []modules.AppFee
	fm.mu.Lock()
	for _, fee := range fm.fees {
		if !fee.PaymentCompleted {
			pendingFees = append(pendingFees, *fee)
		}
	}
	fm.mu.Unlock()
	// Sort by timestamp.
	sort.Sort(modules.AppFeeByTimestamp(pendingFees))

	return pendingFees, nil
}
