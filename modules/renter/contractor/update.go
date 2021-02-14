package contractor

import (
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// hasFCIdentifier checks the transaction for a ContractSignedIdentifier and
// returns the first one it finds with a bool indicating if an identifier was
// found.
func hasFCIdentifier(txn types.Transaction) (modules.ContractSignedIdentifier, crypto.Ciphertext, bool) {
	// We don't verify the host key here so we only need to make sure the
	// identifier fits into the arbitrary data.
	if len(txn.ArbitraryData) != 1 || len(txn.ArbitraryData[0]) < modules.FCSignedIdentiferSize {
		return modules.ContractSignedIdentifier{}, nil, false
	}
	// Verify the prefix.
	var prefix types.Specifier
	copy(prefix[:], txn.ArbitraryData[0])
	if prefix != modules.PrefixFileContractIdentifier &&
		prefix != modules.PrefixNonUplo {
		return modules.ContractSignedIdentifier{}, nil, false
	}
	// We found an identifier.
	var csi modules.ContractSignedIdentifier
	n := copy(csi[:], txn.ArbitraryData[0])
	hostKey := txn.ArbitraryData[0][n:]
	return csi, hostKey, true
}

// managedArchiveContracts will figure out which contracts are no longer needed
// and move them to the historic set of contracts.
func (c *Contractor) managedArchiveContracts() {
	// Determine the current block height.
	c.mu.RLock()
	currentHeight := c.blockHeight
	c.mu.RUnlock()

	// Loop through the current set of contracts and migrate any expired ones to
	// the set of old contracts.
	var expired []types.FileContractID
	for _, contract := range c.staticContracts.ViewAll() {
		// Check map of renewedTo in case renew code was interrupted before
		// archiving old contract
		c.mu.RLock()
		_, renewed := c.renewedTo[contract.ID]
		c.mu.RUnlock()
		if currentHeight > contract.EndHeight || renewed {
			id := contract.ID
			c.mu.Lock()
			c.oldContracts[id] = contract
			c.mu.Unlock()
			expired = append(expired, id)
			c.log.Println("INFO: archived expired contract", id)
		}
	}

	// Save.
	c.mu.Lock()
	c.save()
	c.mu.Unlock()

	// Delete all the expired contracts from the contract set.
	for _, id := range expired {
		if sc, ok := c.staticContracts.Acquire(id); ok {
			c.staticContracts.Delete(sc)
		}
	}
}

// ProcessConsensusChange will be called by the consensus set every time there
// is a change in the blockchain. Updates will always be called in order.
func (c *Contractor) ProcessConsensusChange(cc modules.ConsensusChange) {
	// Get the wallet's seed for contract recovery.
	haveSeed := true
	missedRecovery := false
	s, _, err := c.wallet.PrimarySeed()
	if err != nil {
		haveSeed = false
	}
	// Get the master renter seed and wipe it once we are done with it.
	var renterSeed modules.RenterSeed
	if haveSeed {
		renterSeed = modules.DeriveRenterSeed(s)
		defer fastrand.Read(renterSeed[:])
	}

	c.mu.Lock()
	for _, block := range cc.RevertedBlocks {
		if block.ID() != types.GenesisID {
			c.blockHeight--
		}
		// Remove recoverable contracts found in reverted block.
		c.removeRecoverableContracts(block)
	}
	for _, block := range cc.AppliedBlocks {
		if block.ID() != types.GenesisID {
			c.blockHeight++
		}
		// Find lost contracts for recovery.
		if haveSeed {
			c.findRecoverableContracts(renterSeed, block)
		} else {
			missedRecovery = true
		}
	}
	c.staticWatchdog.callScanConsensusChange(cc)

	// If we didn't miss the recover, we update the recentRecoverChange
	if !missedRecovery && c.recentRecoveryChange == c.lastChange {
		c.recentRecoveryChange = cc.ID
	}

	// If the allowance is set and we have entered the next period, update
	// currentPeriod.
	if c.allowance.Active() && c.blockHeight >= c.currentPeriod+c.allowance.Period {
		c.currentPeriod += c.allowance.Period
		c.staticChurnLimiter.callResetAggregateChurn()

		// COMPATv1.0.4-lts
		// if we were storing a special metrics contract, it will be invalid
		// after we enter the next period.
		delete(c.oldContracts, metricsContractID)
	}

	// Check if c.synced already signals that the contractor is synced.
	synced := false
	select {
	case <-c.synced:
		synced = true
	default:
	}
	// If we weren't synced but are now, we close the channel. If we were
	// synced but aren't anymore, we need a new channel.
	if !synced && cc.Synced {
		close(c.synced)
	} else if synced && !cc.Synced {
		c.synced = make(chan struct{})
	}
	// Let the watchdog take any necessary actions and update its state. We do
	// this before persisting the contractor so that the watchdog is up-to-date on
	// reboot. Otherwise it is possible that e.g. that the watchdog thinks a
	// storage proof was missed and marks down a host for that. Other watchdog
	// actions are innocuous.
	if cc.Synced {
		c.staticWatchdog.callCheckContracts()
	}

	c.lastChange = cc.ID
	err = c.save()
	if err != nil {
		c.log.Println("Unable to save while processing a consensus change:", err)
	}
	c.mu.Unlock()

	// Add to churnLimiter budget.
	numBlocksAdded := len(cc.AppliedBlocks) - len(cc.RevertedBlocks)
	c.staticChurnLimiter.callBumpChurnBudget(numBlocksAdded, c.allowance.Period)

	// Perform contract maintenance if our blockchain is synced. Use a separate
	// goroutine so that the rest of the contractor is not blocked during
	// maintenance.
	if cc.Synced {
		go c.threadedContractMaintenance()
	}
}
