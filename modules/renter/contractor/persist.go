package contractor

import (
	"os"
	"path/filepath"
	"reflect"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/ratelimit"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/proto"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
)

var (
	persistMeta = persist.Metadata{
		Header:  "Contractor Persistence",
		Version: "1.3.1",
	}

	// PersistFilename is the filename to be used when persisting contractor
	// information to a JSON file
	PersistFilename = "contractor.json"
)

// contractorPersist defines what Contractor data persists across sessions.
type contractorPersist struct {
	Allowance            modules.Allowance               `json:"allowance"`
	BlockHeight          types.BlockHeight               `json:"blockheight"`
	CurrentPeriod        types.BlockHeight               `json:"currentperiod"`
	LastChange           modules.ConsensusChangeID       `json:"lastchange"`
	RecentRecoveryChange modules.ConsensusChangeID       `json:"recentrecoverychange"`
	OldContracts         []modules.RenterContract        `json:"oldcontracts"`
	DoubleSpentContracts map[string]types.BlockHeight    `json:"doublespentcontracts"`
	RecoverableContracts []modules.RecoverableContract   `json:"recoverablecontracts"`
	RenewedFrom          map[string]types.FileContractID `json:"renewedfrom"`
	RenewedTo            map[string]types.FileContractID `json:"renewedto"`
	Synced               bool                            `json:"synced"`

	// Subsystem persistence:
	ChurnLimiter churnLimiterPersist `json:"churnlimiter"`
	WatchdogData watchdogPersist     `json:"watchdogdata"`
}

// persistData returns the data in the Contractor that will be saved to disk.
func (c *Contractor) persistData() contractorPersist {
	synced := false
	select {
	case <-c.synced:
		synced = true
	default:
	}
	data := contractorPersist{
		Allowance:            c.allowance,
		BlockHeight:          c.blockHeight,
		CurrentPeriod:        c.currentPeriod,
		LastChange:           c.lastChange,
		RecentRecoveryChange: c.recentRecoveryChange,
		RenewedFrom:          make(map[string]types.FileContractID),
		RenewedTo:            make(map[string]types.FileContractID),
		DoubleSpentContracts: make(map[string]types.BlockHeight),
		Synced:               synced,
	}
	for k, v := range c.renewedFrom {
		data.RenewedFrom[k.String()] = v
	}
	for k, v := range c.renewedTo {
		data.RenewedTo[k.String()] = v
	}
	for _, contract := range c.oldContracts {
		data.OldContracts = append(data.OldContracts, contract)
	}
	for fcID, height := range c.doubleSpentContracts {
		data.DoubleSpentContracts[fcID.String()] = height
	}
	for _, contract := range c.recoverableContracts {
		data.RecoverableContracts = append(data.RecoverableContracts, contract)
	}
	data.ChurnLimiter = c.staticChurnLimiter.callPersistData()
	data.WatchdogData = c.staticWatchdog.callPersistData()
	return data
}

// load loads the Contractor persistence data from disk.
func (c *Contractor) load() error {
	var data contractorPersist
	err := persist.LoadJSON(persistMeta, &data, filepath.Join(c.persistDir, PersistFilename))
	if err != nil {
		return err
	}

	// Compatibility code for allowance definition changes.
	if !reflect.DeepEqual(data.Allowance, modules.Allowance{}) {
		// COMPATv136 if the allowance is not the empty allowance and "Expected"
		// fields are not set, set them to the default values.
		if data.Allowance.ExpectedStorage == 0 && data.Allowance.ExpectedUpload == 0 &&
			data.Allowance.ExpectedDownload == 0 && data.Allowance.ExpectedRedundancy == 0 &&
			data.Allowance.MaxPeriodChurn == 0 {
			// Set the fields to the defaults.
			data.Allowance.ExpectedStorage = modules.DefaultAllowance.ExpectedStorage
			data.Allowance.ExpectedUpload = modules.DefaultAllowance.ExpectedUpload
			data.Allowance.ExpectedDownload = modules.DefaultAllowance.ExpectedDownload
			data.Allowance.ExpectedRedundancy = modules.DefaultAllowance.ExpectedRedundancy
			data.Allowance.MaxPeriodChurn = modules.DefaultAllowance.MaxPeriodChurn
		}

		// COMPATv1412 if the allowance is not the empty allowance and
		// MaxPeriodChurn is 0, set it to the default value.
		if data.Allowance.MaxPeriodChurn == 0 {
			data.Allowance.MaxPeriodChurn = modules.DefaultAllowance.MaxPeriodChurn
		}
	}

	c.allowance = data.Allowance
	c.blockHeight = data.BlockHeight
	c.currentPeriod = data.CurrentPeriod
	c.lastChange = data.LastChange
	c.synced = make(chan struct{})
	if data.Synced {
		close(c.synced)
	}
	c.recentRecoveryChange = data.RecentRecoveryChange
	var fcid types.FileContractID
	for k, v := range data.RenewedFrom {
		if err := fcid.LoadString(k); err != nil {
			return err
		}
		c.renewedFrom[fcid] = v
	}
	for k, v := range data.RenewedTo {
		if err := fcid.LoadString(k); err != nil {
			return err
		}
		c.renewedTo[fcid] = v
	}
	for _, contract := range data.OldContracts {
		c.oldContracts[contract.ID] = contract
	}
	for fcIDString, height := range data.DoubleSpentContracts {
		if err := fcid.LoadString(fcIDString); err != nil {
			return err
		}
		c.doubleSpentContracts[fcid] = height
	}
	for _, contract := range data.RecoverableContracts {
		c.recoverableContracts[contract.ID] = contract
	}

	c.staticChurnLimiter = newChurnLimiterFromPersist(c, data.ChurnLimiter)

	c.staticWatchdog, err = newWatchdogFromPersist(c, data.WatchdogData)
	if err != nil {
		return err
	}
	c.staticWatchdog.renewWindow = data.Allowance.RenewWindow
	c.staticWatchdog.blockHeight = data.BlockHeight
	return nil
}

// save saves the Contractor persistence data to disk.
func (c *Contractor) save() error {
	// c.persistData is broken out because stack traces will not include the
	// function call otherwise.
	persistData := c.persistData()
	filename := filepath.Join(c.persistDir, PersistFilename)
	return persist.SaveJSON(persistMeta, persistData, filename)
}

// convertPersist converts the pre-v1.3.1 contractor persist formats to the new
// formats.
func convertPersist(dir string, rl *ratelimit.RateLimit) (err error) {
	// Try loading v1.3.1 persist. If it has the correct version number, no
	// further action is necessary.
	persistPath := filepath.Join(dir, PersistFilename)
	err = persist.LoadJSON(persistMeta, nil, persistPath)
	if err == nil {
		return nil
	}

	// Try loading v1.3.0 persist (journal).
	journalPath := filepath.Join(dir, "contractor.journal")
	if _, err := os.Stat(journalPath); os.IsNotExist(err) {
		// no journal file found; assume this is a fresh install
		return nil
	}
	var p journalPersist
	j, err := openJournal(journalPath, &p)
	if err != nil {
		return err
	}
	j.Close()
	// convert to v1.3.1 format and save
	data := contractorPersist{
		Allowance:     p.Allowance,
		BlockHeight:   p.BlockHeight,
		CurrentPeriod: p.CurrentPeriod,
		LastChange:    p.LastChange,
	}
	for _, c := range p.OldContracts {
		data.OldContracts = append(data.OldContracts, modules.RenterContract{
			ID:               c.ID,
			HostPublicKey:    c.HostPublicKey,
			StartHeight:      c.StartHeight,
			EndHeight:        c.EndHeight(),
			RenterFunds:      c.RenterFunds(),
			DownloadSpending: c.DownloadSpending,
			StorageSpending:  c.StorageSpending,
			UploadSpending:   c.UploadSpending,
			TotalCost:        c.TotalCost,
			ContractFee:      c.ContractFee,
			TxnFee:           c.TxnFee,
			UplofundFee:       c.UplofundFee,
		})
	}
	err = persist.SaveJSON(persistMeta, data, persistPath)
	if err != nil {
		return err
	}

	// create the contracts directory if it does not yet exist
	cs, err := proto.NewContractSet(filepath.Join(dir, "contracts"), rl, modules.ProdDependencies)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, cs.Close())
	}()

	// convert contracts to contract files
	for _, c := range p.Contracts {
		cachedRev := p.CachedRevisions[c.ID.String()]
		if err := cs.ConvertV130Contract(c, cachedRev); err != nil {
			return err
		}
	}

	// delete the journal file
	return errors.AddContext(os.Remove(journalPath), "failed to remove journal file")
}
