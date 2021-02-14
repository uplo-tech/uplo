package contractor

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/ratelimit"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/proto"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
)

// TestSaveLoad tests that the contractor can save and load itself.
func TestSaveLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	// create contractor with mocked persist dependency
	persistDir := build.TempDir("contractor", "mock")
	os.MkdirAll(persistDir, 0700)
	c := &Contractor{
		persistDir: persistDir,
		synced:     make(chan struct{}),
	}

	c.staticWatchdog = newWatchdog(c)
	expectedFileContractStatus := &fileContractStatus{
		formationSweepHeight: 543210,
		contractFound:        true,
		revisionFound:        400,
		storageProofFound:    987123,

		formationTxnSet: []types.Transaction{
			{
				ArbitraryData: [][]byte{{1, 2, 3, 4, 5}},
			},
		},
		parentOutputs: map[types.UplocoinOutputID]struct{}{
			{4}: {},
		},

		sweepTxn: types.Transaction{
			ArbitraryData: [][]byte{{1, 2, 3}},
		},

		sweepParents: []types.Transaction{{
			ArbitraryData: [][]byte{{4, 5, 63}},
		}},

		windowStart: 5,
		windowEnd:   10,
	}
	c.staticWatchdog.contracts = map[types.FileContractID]*fileContractStatus{
		{1}: expectedFileContractStatus,
	}

	expectedArchivedContract := modules.ContractWatchStatus{
		Archived:                  true,
		FormationSweepHeight:      11,
		ContractFound:             true,
		LatestRevisionFound:       3883889,
		StorageProofFoundAtHeight: 12312,
		DoubleSpendHeight:         12333333,
		WindowStart:               1111111231209,
		WindowEnd:                 123808900,
	}
	c.staticWatchdog.archivedContracts = map[types.FileContractID]modules.ContractWatchStatus{
		{2}: expectedArchivedContract,
	}

	c.oldContracts = map[types.FileContractID]modules.RenterContract{
		{0}: {ID: types.FileContractID{0}, HostPublicKey: types.UploPublicKey{Key: []byte("foo")}},
		{1}: {ID: types.FileContractID{1}, HostPublicKey: types.UploPublicKey{Key: []byte("bar")}},
		{2}: {ID: types.FileContractID{2}, HostPublicKey: types.UploPublicKey{Key: []byte("baz")}},
	}

	c.renewedFrom = map[types.FileContractID]types.FileContractID{
		{1}: {2},
	}
	c.renewedTo = map[types.FileContractID]types.FileContractID{
		{1}: {2},
	}
	close(c.synced)

	c.staticChurnLimiter = newChurnLimiter(c)
	c.staticChurnLimiter.aggregateCurrentPeriodChurn = 123456
	c.staticChurnLimiter.remainingChurnBudget = -789

	// save, clear, and reload
	err := c.save()
	if err != nil {
		t.Fatal(err)
	}
	c.oldContracts = make(map[types.FileContractID]modules.RenterContract)
	c.renewedFrom = make(map[types.FileContractID]types.FileContractID)
	c.renewedTo = make(map[types.FileContractID]types.FileContractID)
	err = c.load()
	if err != nil {
		t.Fatal(err)
	}
	// Check that all fields were restored
	_, ok0 := c.oldContracts[types.FileContractID{0}]
	_, ok1 := c.oldContracts[types.FileContractID{1}]
	_, ok2 := c.oldContracts[types.FileContractID{2}]
	if !ok0 || !ok1 || !ok2 {
		t.Fatal("oldContracts were not restored properly:", c.oldContracts)
	}
	id := types.FileContractID{2}
	if c.renewedFrom[types.FileContractID{1}] != id {
		t.Fatal("renewedFrom not restored properly:", c.renewedFrom)
	}
	if c.renewedTo[types.FileContractID{1}] != id {
		t.Fatal("renewedTo not restored properly:", c.renewedTo)
	}
	select {
	case <-c.synced:
	default:
		t.Fatal("contractor should be synced")
	}
	// use stdPersist instead of mock
	c.persistDir = build.TempDir("contractor", t.Name())
	os.MkdirAll(c.persistDir, 0700)

	// COMPATv136 save the allowance but make sure that the newly added fields
	// are 0. After loading them from disk they should be set to the default
	// values.
	c.allowance = modules.DefaultAllowance
	c.allowance.ExpectedStorage = 0
	c.allowance.ExpectedUpload = 0
	c.allowance.ExpectedDownload = 0
	c.allowance.ExpectedRedundancy = 0
	c.allowance.MaxPeriodChurn = 0

	// save, clear, and reload
	err = c.save()
	if err != nil {
		t.Fatal(err)
	}
	c.oldContracts = make(map[types.FileContractID]modules.RenterContract)
	c.renewedFrom = make(map[types.FileContractID]types.FileContractID)
	c.renewedTo = make(map[types.FileContractID]types.FileContractID)
	c.synced = make(chan struct{})
	err = c.load()
	if err != nil {
		t.Fatal(err)
	}
	// check that all fields were restored
	_, ok0 = c.oldContracts[types.FileContractID{0}]
	_, ok1 = c.oldContracts[types.FileContractID{1}]
	_, ok2 = c.oldContracts[types.FileContractID{2}]
	if !ok0 || !ok1 || !ok2 {
		t.Fatal("oldContracts were not restored properly:", c.oldContracts)
	}
	if c.renewedFrom[types.FileContractID{1}] != id {
		t.Fatal("renewedFrom not restored properly:", c.renewedFrom)
	}
	if c.renewedTo[types.FileContractID{1}] != id {
		t.Fatal("renewedTo not restored properly:", c.renewedTo)
	}
	select {
	case <-c.synced:
	default:
		t.Fatal("contractor should be synced")
	}
	if c.allowance.ExpectedStorage != modules.DefaultAllowance.ExpectedStorage {
		t.Errorf("ExpectedStorage was %v but should be %v",
			c.allowance.ExpectedStorage, modules.DefaultAllowance.ExpectedStorage)
	}
	if c.allowance.ExpectedUpload != modules.DefaultAllowance.ExpectedUpload {
		t.Errorf("ExpectedUpload was %v but should be %v",
			c.allowance.ExpectedUpload, modules.DefaultAllowance.ExpectedUpload)
	}
	if c.allowance.ExpectedDownload != modules.DefaultAllowance.ExpectedDownload {
		t.Errorf("ExpectedDownload was %v but should be %v",
			c.allowance.ExpectedDownload, modules.DefaultAllowance.ExpectedDownload)
	}
	if c.allowance.ExpectedRedundancy != modules.DefaultAllowance.ExpectedRedundancy {
		t.Errorf("ExpectedRedundancy was %v but should be %v",
			c.allowance.ExpectedRedundancy, modules.DefaultAllowance.ExpectedRedundancy)
	}

	// Change the expected* fields of the allowance again, save, clear and reload.
	c.allowance.ExpectedStorage = uint64(fastrand.Intn(100))
	c.allowance.ExpectedUpload = uint64(fastrand.Intn(100))
	c.allowance.ExpectedDownload = uint64(fastrand.Intn(100))
	c.allowance.ExpectedRedundancy = float64(fastrand.Intn(100))
	c.allowance.MaxPeriodChurn = 1357
	a := c.allowance
	// Save
	err = c.save()
	if err != nil {
		t.Fatal(err)
	}
	// Clear allowance.
	c.allowance = modules.Allowance{}
	// Load
	err = c.load()
	if err != nil {
		t.Fatal(err)
	}
	// Check if fields were restored.
	if c.allowance.ExpectedStorage != a.ExpectedStorage {
		t.Errorf("ExpectedStorage was %v but should be %v",
			c.allowance.ExpectedStorage, a.ExpectedStorage)
	}
	if c.allowance.ExpectedUpload != a.ExpectedUpload {
		t.Errorf("ExpectedUpload was %v but should be %v",
			c.allowance.ExpectedUpload, a.ExpectedUpload)
	}
	if c.allowance.ExpectedDownload != a.ExpectedDownload {
		t.Errorf("ExpectedDownload was %v but should be %v",
			c.allowance.ExpectedDownload, a.ExpectedDownload)
	}
	if c.allowance.ExpectedRedundancy != a.ExpectedRedundancy {
		t.Errorf("ExpectedRedundancy was %v but should be %v",
			c.allowance.ExpectedRedundancy, a.ExpectedRedundancy)
	}
	if c.allowance.MaxPeriodChurn != a.MaxPeriodChurn {
		t.Errorf("MaxPeriodChurn was %v but should be %v",
			c.allowance.MaxPeriodChurn, a.MaxPeriodChurn)
	}

	// Check the watchdog settings.
	if c.staticWatchdog == nil {
		t.Fatal("Watchdog not restored")
	}
	contract, ok := c.staticWatchdog.contracts[types.FileContractID{1}]
	if !ok {
		t.Fatal("Contract not found", len(c.staticWatchdog.contracts))
	}
	if contract.formationSweepHeight != expectedFileContractStatus.formationSweepHeight {
		t.Fatal("watchdog not restored properly", contract.formationSweepHeight)
	}
	if contract.contractFound != expectedFileContractStatus.contractFound {
		t.Fatal("watchdog not restored properly")
	}
	if contract.revisionFound != expectedFileContractStatus.revisionFound {
		t.Fatal("watchdog not restored properly", contract.revisionFound)
	}
	if contract.storageProofFound != expectedFileContractStatus.storageProofFound {
		t.Fatal("watchdog not restored properly", contract.storageProofFound)
	}
	if len(contract.formationTxnSet) != 1 {
		t.Fatal("watchdog not restored properly", contract)
	}
	if contract.formationTxnSet[0].ID() != expectedFileContractStatus.formationTxnSet[0].ID() {
		t.Fatal("watchdog not restored properly", contract.formationTxnSet)
	}
	if len(contract.parentOutputs) != 1 {
		t.Fatal("watchdog not restored properly", contract.parentOutputs)
	}
	if _, foundOutput := contract.parentOutputs[types.UplocoinOutputID{4}]; !foundOutput {
		t.Fatal("watchdog not restored properly", contract.parentOutputs)
	}
	if contract.sweepTxn.ID() != expectedFileContractStatus.sweepTxn.ID() {
		t.Fatal("watchdog not restored properly", contract)
	}
	if len(contract.sweepParents) != len(expectedFileContractStatus.sweepParents) {
		t.Fatal("watchdog not restored properly", contract)
	}
	if contract.sweepParents[0].ID() != expectedFileContractStatus.sweepParents[0].ID() {
		t.Fatal("watchdog not restored properly", contract)
	}
	if contract.windowStart != expectedFileContractStatus.windowStart {
		t.Fatal("watchdog not restored properly", contract)
	}
	if contract.windowEnd != expectedFileContractStatus.windowEnd {
		t.Fatal("watchdog not restored properly", contract)
	}
	if len(c.staticWatchdog.archivedContracts) != 1 {
		t.Fatal("watchdog not restored properly", c.staticWatchdog.archivedContracts)
	}
	archivedContract, ok := c.staticWatchdog.archivedContracts[types.FileContractID{2}]
	if !ok {
		t.Fatal("watchdog not restored properly", c.staticWatchdog.archivedContracts)
	}
	if !reflect.DeepEqual(archivedContract, expectedArchivedContract) {
		t.Fatal("Archived contract not restored properly", archivedContract)
	}

	// Check churnLimiter state.
	aggregateChurn, maxChurn := c.staticChurnLimiter.managedAggregateAndMaxChurn()
	if aggregateChurn != 123456 {
		t.Fatal("Expected 123456 aggregate churn", aggregateChurn)
	}
	if maxChurn != a.MaxPeriodChurn {
		t.Fatal("Expected 1357 max churn", maxChurn)
	}
	remainingChurnBudget, periodBudget := c.staticChurnLimiter.managedChurnBudget()
	if remainingChurnBudget != -789 {
		t.Fatal("Expected -789 remainingChurnBudget", remainingChurnBudget)
	}
	expectedPeriodBudget := 1357 - 123456
	if periodBudget != expectedPeriodBudget {
		t.Fatal("Expected remainingChurnBudget", periodBudget)
	}
}

// TestConvertPersist tests that contracts previously stored in the
// .journal format can be converted to the .contract format.
func TestConvertPersist(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	dir := build.TempDir(filepath.Join("contractor", t.Name()))
	os.MkdirAll(dir, 0700)
	// copy the test data into the temp folder
	testdata, err := ioutil.ReadFile(filepath.Join("testdata", "TestConvertPersist.journal"))
	if err != nil {
		t.Fatal(err)
	}
	err = ioutil.WriteFile(filepath.Join(dir, "contractor.journal"), testdata, 0600)
	if err != nil {
		t.Fatal(err)
	}

	// convert the journal
	err = convertPersist(dir, ratelimit.NewRateLimit(0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}

	// load the persist
	var p contractorPersist
	err = persist.LoadJSON(persistMeta, &p, filepath.Join(dir, PersistFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Allowance.Funds.Equals64(10) || p.Allowance.Hosts != 7 || p.Allowance.Period != 3 || p.Allowance.RenewWindow != 20 {
		t.Fatal("recovered allowance was wrong:", p.Allowance)
	}

	// load the contracts
	cs, err := proto.NewContractSet(filepath.Join(dir, "contracts"), ratelimit.NewRateLimit(0, 0, 0), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Len() != 1 {
		t.Fatal("expected 1 contract, got", cs.Len())
	}
	m := cs.ViewAll()[0]
	if m.ID.String() != "792b5eec683819d78416a9e80cba454ebcb5a52eeac4f17b443d177bd425fc5c" {
		t.Fatal("recovered contract has wrong ID", m.ID)
	}
}
