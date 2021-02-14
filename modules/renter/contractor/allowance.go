package contractor

import (
	"errors"
	"reflect"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

var (
	errAllowanceNotSynced = errors.New("you must be synced to set an allowance")

	// ErrAllowanceZeroFunds is returned if the allowance funds are being set to
	// zero when not cancelling the allowance
	ErrAllowanceZeroFunds = errors.New("funds must be non-zero")
	// ErrAllowanceZeroPeriod is returned if the allowance period is being set
	// to zero when not cancelling the allowance
	ErrAllowanceZeroPeriod = errors.New("period must be non-zero")
	// ErrAllowanceZeroWindow is returned if the allowance renew window is being
	// set to zero when not cancelling the allowance
	ErrAllowanceZeroWindow = errors.New("renew window must be non-zero")
	// ErrAllowanceNoHosts is returned if the allowance hosts are being set to
	// zero when not cancelling the allowance
	ErrAllowanceNoHosts = errors.New("hosts must be non-zero")
	// ErrAllowanceZeroExpectedStorage is returned if the allowance expected
	// storage is being set to zero when not cancelling the allowance
	ErrAllowanceZeroExpectedStorage = errors.New("expected storage must be non-zero")
	// ErrAllowanceZeroExpectedUpload is returned if the allowance expected
	// upload is being set to zero when not cancelling the allowance
	ErrAllowanceZeroExpectedUpload = errors.New("expected upload  must be non-zero")
	// ErrAllowanceZeroExpectedDownload is returned if the allowance expected
	// download is being set to zero when not cancelling the allowance
	ErrAllowanceZeroExpectedDownload = errors.New("expected download  must be non-zero")
	// ErrAllowanceZeroExpectedRedundancy is returned if the allowance expected
	// redundancy is being set to zero when not cancelling the allowance
	ErrAllowanceZeroExpectedRedundancy = errors.New("expected redundancy must be non-zero")
	// ErrAllowanceZeroMaxPeriodChurn is returned if the allowance max period
	// churn is being set to zero when not cancelling the allowance
	ErrAllowanceZeroMaxPeriodChurn = errors.New("max period churn must be non-zero")
)

// SetAllowance sets the amount of money the Contractor is allowed to spend on
// contracts over a given time period, divided among the number of hosts
// specified. Note that Contractor can start forming contracts as soon as
// SetAllowance is called; that is, it may block.
//
// In most cases, SetAllowance will renew existing contracts instead of
// forming new ones. This preserves the data on those hosts. When this occurs,
// the renewed contracts will atomically replace their previous versions. If
// SetAllowance is interrupted, renewed contracts may be lost, though the
// allocated funds will eventually be returned.
//
// If a is the empty allowance, SetAllowance will archive the current contract
// set. The contracts cannot be used to create Editors or Downloads, and will
// not be renewed.
//
// NOTE: At this time, transaction fees are not counted towards the allowance.
// This means the contractor may spend more than allowance.Funds.
func (c *Contractor) SetAllowance(a modules.Allowance) error {
	if reflect.DeepEqual(a, modules.Allowance{}) {
		return c.managedCancelAllowance()
	}
	if reflect.DeepEqual(a, c.allowance) {
		return nil
	}

	// sanity checks
	if a.Funds.Cmp(types.ZeroCurrency) <= 0 {
		return ErrAllowanceZeroFunds
	} else if a.Hosts == 0 {
		return ErrAllowanceNoHosts
	} else if a.Period == 0 {
		return ErrAllowanceZeroPeriod
	} else if a.RenewWindow == 0 {
		return ErrAllowanceZeroWindow
	} else if a.ExpectedStorage == 0 {
		return ErrAllowanceZeroExpectedStorage
	} else if a.ExpectedUpload == 0 {
		return ErrAllowanceZeroExpectedUpload
	} else if a.ExpectedDownload == 0 {
		return ErrAllowanceZeroExpectedDownload
	} else if a.ExpectedRedundancy == 0 {
		return ErrAllowanceZeroExpectedRedundancy
	} else if a.MaxPeriodChurn == 0 {
		return ErrAllowanceZeroMaxPeriodChurn
	} else if !c.cs.Synced() {
		return errAllowanceNotSynced
	}
	c.log.Println("INFO: setting allowance to", a)

	// Set the current period if the existing allowance is empty.
	//
	// When setting the current period we want to ensure that it aligns with the
	// start and endheights of the contracts as we would expect. To do this we
	// have to consider the following. First, that the current period value is
	// incremented by the allowance period, and second, that the total length of
	// a contract is the period + renew window. This means the that contracts are
	// always overlapping periods, and we want that overlap to be the renew
	// window. In order to create this overlap we set the current period as such.
	//
	// If the renew window is less than the period the current period is set in
	// the past by the renew window.
	//
	// If the renew window is greater than or equal to the period we set the
	// current period to the current block height.
	//
	// Also remember that we might have to unlock our contracts if the allowance
	// was set to the empty allowance before.
	c.mu.Lock()
	unlockContracts := false
	if reflect.DeepEqual(c.allowance, modules.Allowance{}) {
		c.currentPeriod = c.blockHeight
		if a.Period > a.RenewWindow {
			c.currentPeriod -= a.RenewWindow
		}
		unlockContracts = true
	}
	c.allowance = a
	err := c.save()
	c.mu.Unlock()
	if err != nil {
		c.log.Println("Unable to save contractor after setting allowance:", err)
	}

	// Cycle through all contracts and unlock them again since they might have
	// been locked by managedCancelAllowance previously.
	if unlockContracts {
		ids := c.staticContracts.IDs()
		for _, id := range ids {
			contract, exists := c.staticContracts.Acquire(id)
			if !exists {
				continue
			}
			utility := contract.Utility()
			utility.Locked = false
			err := c.callUpdateUtility(contract, utility, false)
			c.staticContracts.Return(contract)
			if err != nil {
				return err
			}
		}
	}

	// Inform the watchdog about the allowance change.
	c.staticWatchdog.callAllowanceUpdated(a)

	// We changed the allowance successfully. Update the hostdb.
	err = c.hdb.SetAllowance(a)
	if err != nil {
		return err
	}

	// Interrupt any existing maintenance and launch a new round of
	// maintenance.
	if err := c.tg.Add(); err != nil {
		return err
	}
	go func() {
		defer c.tg.Done()
		c.callInterruptContractMaintenance()
		c.threadedContractMaintenance()
	}()
	return nil
}

// managedCancelAllowance handles the special case where the allowance is empty.
func (c *Contractor) managedCancelAllowance() error {
	c.log.Println("INFO: canceling allowance")
	// first need to invalidate any active editors
	// NOTE: this code is the same as in managedRenewContracts
	ids := c.staticContracts.IDs()
	c.mu.Lock()
	for _, id := range ids {
		// we aren't renewing, but we don't want new editors or downloaders to
		// be created
		c.renewing[id] = true
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		for _, id := range ids {
			delete(c.renewing, id)
		}
		c.mu.Unlock()
	}()
	for _, id := range ids {
		c.mu.RLock()
		e, eok := c.editors[id]
		s, sok := c.sessions[id]
		c.mu.RUnlock()
		if eok {
			e.invalidate()
		}
		if sok {
			s.invalidate()
		}
	}

	// Clear out the allowance and save.
	c.mu.Lock()
	c.allowance = modules.Allowance{}
	c.currentPeriod = 0
	err := c.save()
	c.mu.Unlock()
	if err != nil {
		return err
	}

	// Issue an interrupt to any in-progress contract maintenance thread.
	c.callInterruptContractMaintenance()

	// Cycle through all contracts and mark them as !goodForRenew and !goodForUpload
	ids = c.staticContracts.IDs()
	for _, id := range ids {
		contract, exists := c.staticContracts.Acquire(id)
		if !exists {
			continue
		}
		utility := contract.Utility()
		utility.GoodForRenew = false
		utility.GoodForUpload = false
		utility.Locked = true
		err := c.callUpdateUtility(contract, utility, false)
		c.staticContracts.Return(contract)
		if err != nil {
			return err
		}
	}
	return nil
}
