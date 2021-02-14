package contractor

import (
	"math"
	"math/big"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/proto"
	"github.com/uplo-tech/uplo/types"
)

type utilityUpdateStatus int

const (
	_ = iota
	noUpdate
	suggestedUtilityUpdate
	necessaryUtilityUpdate
)

// badContractCheck checks whether the contract has been marked as bad. If the
// contract has been marked as bad, GoodForUpload and GoodForRenew need to be
// set to false to prevent the renter from using this contract.
func (c *Contractor) badContractCheck(u modules.ContractUtility) (modules.ContractUtility, bool) {
	if u.BadContract {
		u.GoodForUpload = false
		u.GoodForRenew = false
		return u, true
	}
	return u, false
}

// maxRevisionCheck will return a locked utility if the contract has reached its
// max revision.
func (c *Contractor) maxRevisionCheck(u modules.ContractUtility, revisionNumber uint64) (modules.ContractUtility, bool) {
	if revisionNumber == math.MaxUint64 {
		u.GoodForUpload = false
		u.GoodForRenew = false
		u.Locked = true
		return u, true
	}
	return u, false
}

// renewedCheck will return a contract with no utility and a required update if
// the contract has been renewed, no changes otherwise.
func (c *Contractor) renewedCheck(u modules.ContractUtility, renewed bool) (modules.ContractUtility, bool) {
	if renewed {
		u.GoodForUpload = false
		u.GoodForRenew = false
		return u, true
	}
	return u, false
}

// managedCheckHostScore checks host scorebreakdown against minimum accepted
// scores.  forceUpdate is true if the utility change must be taken.
func (c *Contractor) managedCheckHostScore(contract modules.RenterContract, sb modules.HostScoreBreakdown, minScoreGFR, minScoreGFU types.Currency) (modules.ContractUtility, utilityUpdateStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	u := contract.Utility

	// Check whether the contract is a payment contract. Payment contracts
	// cannot be marked !GFR for poor score.
	var size uint64
	if len(contract.Transaction.FileContractRevisions) > 0 {
		size = contract.Transaction.FileContractRevisions[0].NewFileSize
	}
	paymentContract := !c.allowance.PaymentContractInitialFunding.IsZero() && size == 0

	// Contract has no utility if the score is poor. Cannot be marked as bad if
	// the contract is a payment contract.
	deadScore := sb.Score.Cmp(types.NewCurrency64(1)) <= 0
	badScore := !minScoreGFR.IsZero() && sb.Score.Cmp(minScoreGFR) < 0
	if deadScore || (badScore && !paymentContract) {
		// Log if the utility has changed.
		if u.GoodForUpload || u.GoodForRenew {
			c.log.Printf("Marking contract as having no utility because of host score: %v", contract.ID)
			c.log.Println("Min Score:", minScoreGFR)
			c.log.Println("Score:    ", sb.Score)
			c.log.Println("Age Adjustment:        ", sb.AgeAdjustment)
			c.log.Println("Base Price Adjustment: ", sb.BasePriceAdjustment)
			c.log.Println("Burn Adjustment:       ", sb.BurnAdjustment)
			c.log.Println("Collateral Adjustment: ", sb.CollateralAdjustment)
			c.log.Println("Duration Adjustment:   ", sb.DurationAdjustment)
			c.log.Println("Interaction Adjustment:", sb.InteractionAdjustment)
			c.log.Println("Price Adjustment:      ", sb.PriceAdjustment)
			c.log.Println("Storage Adjustment:    ", sb.StorageRemainingAdjustment)
			c.log.Println("Uptime Adjustment:     ", sb.UptimeAdjustment)
			c.log.Println("Version Adjustment:    ", sb.VersionAdjustment)
		}
		u.GoodForUpload = false
		u.GoodForRenew = false

		// Only force utility updates if the score is the min possible score.
		// Otherwise defer update decision for low-score contracts to the
		// churnLimiter.
		if deadScore {
			return u, necessaryUtilityUpdate
		}
		c.log.Println("Adding contract utility update to churnLimiter queue")
		return u, suggestedUtilityUpdate
	}

	// Contract should not be used for uplodaing if the score is poor.
	if !minScoreGFU.IsZero() && sb.Score.Cmp(minScoreGFU) < 0 {
		if u.GoodForUpload {
			c.log.Printf("Marking contract as not good for upload because of a poor score: %v", contract.ID)
			c.log.Println("Min Score:", minScoreGFU)
			c.log.Println("Score:    ", sb.Score)
			c.log.Println("Age Adjustment:        ", sb.AgeAdjustment)
			c.log.Println("Base Price Adjustment: ", sb.BasePriceAdjustment)
			c.log.Println("Burn Adjustment:       ", sb.BurnAdjustment)
			c.log.Println("Collateral Adjustment: ", sb.CollateralAdjustment)
			c.log.Println("Duration Adjustment:   ", sb.DurationAdjustment)
			c.log.Println("Interaction Adjustment:", sb.InteractionAdjustment)
			c.log.Println("Price Adjustment:      ", sb.PriceAdjustment)
			c.log.Println("Storage Adjustment:    ", sb.StorageRemainingAdjustment)
			c.log.Println("Uptime Adjustment:     ", sb.UptimeAdjustment)
			c.log.Println("Version Adjustment:    ", sb.VersionAdjustment)
		}
		if !u.GoodForRenew {
			c.log.Println("Marking contract as being good for renew", contract.ID)
		}
		u.GoodForUpload = false
		u.GoodForRenew = true
		return u, necessaryUtilityUpdate
	}

	return u, noUpdate
}

// managedCriticalUtilityChecks performs critical checks on a contract that
// would require, with no exceptions, marking the contract as !GFR and/or !GFU.
// Returns true if and only if and of the checks passed and require the utility
// to be updated.
//
// NOTE: 'needsUpdate' should return 'true' if the contract should be marked as
// !GFR and !GFU, even if the contract is already marked as such. If
// 'needsUpdate' is set to true, other checks which may change those values will
// be ignored and the contract will remain marked as having no utility.
func (c *Contractor) managedCriticalUtilityChecks(sc *proto.SafeContract, host modules.HostDBEntry) (modules.ContractUtility, bool) {
	contract := sc.Metadata()

	c.mu.RLock()
	blockHeight := c.blockHeight
	renewWindow := c.allowance.RenewWindow
	period := c.allowance.Period
	_, renewed := c.renewedTo[contract.ID]
	c.mu.RUnlock()

	// A contract that has been renewed should be set to !GFU and !GFR.
	u, needsUpdate := c.renewedCheck(contract.Utility, renewed)
	if needsUpdate {
		return u, needsUpdate
	}

	u, needsUpdate = c.maxRevisionCheck(contract.Utility, sc.LastRevision().NewRevisionNumber)
	if needsUpdate {
		return u, needsUpdate
	}

	u, needsUpdate = c.badContractCheck(contract.Utility)
	if needsUpdate {
		return u, needsUpdate
	}

	u, needsUpdate = c.offlineCheck(contract, host)
	if needsUpdate {
		return u, needsUpdate
	}

	u, needsUpdate = c.upForRenewalCheck(contract, renewWindow, blockHeight)
	if needsUpdate {
		return u, needsUpdate
	}

	u, needsUpdate = c.sufficientFundsCheck(contract, host, period)
	if needsUpdate {
		return u, needsUpdate
	}

	u, needsUpdate = c.outOfStorageCheck(contract, blockHeight)
	if needsUpdate {
		return u, needsUpdate
	}

	return contract.Utility, false
}

// managedHostInHostDBCheck checks if the host is in the hostdb and not
// filtered.  Returns true if a check fails and the utility returned must be
// used to update the contract state.
func (c *Contractor) managedHostInHostDBCheck(contract modules.RenterContract) (modules.HostDBEntry, modules.ContractUtility, bool) {
	u := contract.Utility
	host, exists, err := c.hdb.Host(contract.HostPublicKey)
	// Contract has no utility if the host is not in the database. Or is
	// filtered by the blacklist or whitelist. Or if there was an error
	if !exists || host.Filtered || err != nil {
		// Log if the utility has changed.
		if u.GoodForUpload || u.GoodForRenew {
			c.log.Printf("Marking contract as having no utility because found in hostDB: %v, or host is Filtered: %v - %v", exists, host.Filtered, contract.ID)
		}
		u.GoodForUpload = false
		u.GoodForRenew = false
		return host, u, true
	}

	// TODO: If the host is not in the hostdb, we need to do some sort of rescan
	// to recover the host. The hostdb is not supposed to be dropping hosts that
	// we have formed contracts with. We should do what we can to get the host
	// back.

	return host, u, false
}

// offLineCheck checks if the host for this contract is offline.
// Returns true if a check fails and the utility returned must be used to update
// the contract state.
func (c *Contractor) offlineCheck(contract modules.RenterContract, host modules.HostDBEntry) (modules.ContractUtility, bool) {
	u := contract.Utility
	// Contract has no utility if the host is offline.
	if isOffline(host) {
		// Log if the utility has changed.
		if u.GoodForUpload || u.GoodForRenew {
			c.log.Println("Marking contract as having no utility because of host being offline", contract.ID)
		}
		u.GoodForUpload = false
		u.GoodForRenew = false
		return u, true
	}
	return u, false
}

// upForRenewalCheck checks if this contract is up for renewal.
// Returns true if a check fails and the utility returned must be used to update
// the contract state.
func (c *Contractor) upForRenewalCheck(contract modules.RenterContract, renewWindow, blockHeight types.BlockHeight) (modules.ContractUtility, bool) {
	u := contract.Utility
	// Contract should not be used for uploading if the time has come to
	// renew the contract.
	if blockHeight+renewWindow >= contract.EndHeight {
		if u.GoodForUpload {
			c.log.Println("Marking contract as not good for upload because it is time to renew the contract", contract.ID)
		}
		if !u.GoodForRenew {
			c.log.Println("Marking contract as being good for renew:", contract.ID)
		}
		u.GoodForUpload = false
		u.GoodForRenew = true
		return u, true
	}
	return u, false
}

// sufficientFundsCheck checks if there are enough funds left in the contract
// for uploads.
// Returns true if a check fails and the utility returned must be used to update
// the contract state.
func (c *Contractor) sufficientFundsCheck(contract modules.RenterContract, host modules.HostDBEntry, period types.BlockHeight) (modules.ContractUtility, bool) {
	u := contract.Utility

	// Contract should not be used for uploading if the contract does
	// not have enough money remaining to perform the upload.
	blockBytes := types.NewCurrency64(modules.SectorSize * uint64(period))
	sectorStoragePrice := host.StoragePrice.Mul(blockBytes)
	sectorUploadBandwidthPrice := host.UploadBandwidthPrice.Mul64(modules.SectorSize)
	sectorDownloadBandwidthPrice := host.DownloadBandwidthPrice.Mul64(modules.SectorSize)
	sectorBandwidthPrice := sectorUploadBandwidthPrice.Add(sectorDownloadBandwidthPrice)
	sectorPrice := sectorStoragePrice.Add(sectorBandwidthPrice)
	percentRemaining, _ := big.NewRat(0, 1).SetFrac(contract.RenterFunds.Big(), contract.TotalCost.Big()).Float64()
	if contract.RenterFunds.Cmp(sectorPrice.Mul64(3)) < 0 || percentRemaining < MinContractFundUploadThreshold {
		if u.GoodForUpload {
			c.log.Printf("Marking contract as not good for upload because of insufficient funds: %v vs. %v - %v", contract.RenterFunds.Cmp(sectorPrice.Mul64(3)) < 0, percentRemaining, contract.ID)
		}
		if !u.GoodForRenew {
			c.log.Println("Marking contract as being good for renew:", contract.ID)
		}
		u.GoodForUpload = false
		u.GoodForRenew = true
		return u, true
	}
	return u, false
}

// outOfStorageCheck checks if the host is running out of storage.
// Returns true if a check fails and the utility returned must be used to update
// the contract state.
func (c *Contractor) outOfStorageCheck(contract modules.RenterContract, blockHeight types.BlockHeight) (modules.ContractUtility, bool) {
	u := contract.Utility
	// If LastOOSErr has never been set, return false.
	if u.LastOOSErr == 0 {
		return u, false
	}
	// Contract should not be used for uploading if the host is out of storage.
	if blockHeight-u.LastOOSErr <= oosRetryInterval {
		if u.GoodForUpload {
			c.log.Println("Marking contract as not being good for upload due to the host running out of storage:", contract.ID)
		}
		if !u.GoodForRenew {
			c.log.Println("Marking contract as being good for renew:", contract.ID)
		}
		u.GoodForUpload = false
		u.GoodForRenew = true
		return u, true
	}
	return u, false
}
