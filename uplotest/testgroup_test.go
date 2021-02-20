package uplotest

import (
	"path/filepath"
	"testing"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/node"
	"github.com/uplo-tech/errors"
)

// TestNewGroup tests the behavior of NewGroup.
func TestNewGroup(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Specify the parameters for the group
	groupParams := GroupParams{
		Hosts:   5,
		Portals: 1,
		Renters: 2,
		Miners:  2,
	}
	// Create the group
	tg, err := NewGroupFromTemplate(uplotestTestDir(t.Name()), groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Check if the correct number of nodes was created
	if len(tg.Hosts()) != groupParams.Hosts {
		t.Error("Wrong number of hosts")
	}
	expectedRenters := groupParams.Portals + groupParams.Renters
	if len(tg.Renters()) != expectedRenters {
		t.Error("Wrong number of renters")
	}
	if len(tg.Portals()) != groupParams.Portals {
		t.Error("Wrong number of portals")
	}
	if len(tg.Miners()) != groupParams.Miners {
		t.Error("Wrong number of miners")
	}
	expectedNumberNodes := groupParams.Hosts + groupParams.Portals + groupParams.Renters + groupParams.Miners
	if len(tg.Nodes()) != expectedNumberNodes {
		t.Error("Wrong number of nodes")
	}

	// Check that all hosts are announced and have a registry.
	for _, host := range tg.Hosts() {
		hg, err := host.HostGet()
		if err != nil {
			t.Fatal(err)
		}
		if !hg.InternalSettings.AcceptingContracts {
			t.Fatal("host not accepting contracts")
		}
		if hg.InternalSettings.RegistrySize == 0 {
			t.Fatal("registry not set")
		}
	}

	// Check if nodes are funded
	cg, err := tg.Nodes()[0].ConsensusGet()
	if err != nil {
		t.Fatal("Failed to get consensus: ", err)
	}
	for _, node := range tg.Nodes() {
		wtg, err := node.WalletTransactionsGet(0, cg.Height)
		if err != nil {
			t.Fatal(err)
		}
		if len(wtg.ConfirmedTransactions) == 0 {
			t.Errorf("Node has 0 confirmed funds")
		}
	}

	// Check the portals allowance
	p := tg.Portals()[0]
	rg, err := p.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	a := rg.Settings.Allowance
	if !a.PaymentContractInitialFunding.Equals(DefaultPaymentContractInitialFunding) {
		t.Fatal("Portals PaymentContractInitialFunding is not set as expected")
	}
}

// TestNewGroupNoMiner tests NewGroup without a miner
func TestNewGroupNoMiner(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Try to create a group without miners
	groupParams := GroupParams{
		Hosts:   5,
		Renters: 2,
		Miners:  0,
	}
	// Create the group
	_, err := NewGroupFromTemplate(uplotestTestDir(t.Name()), groupParams)
	if err == nil {
		t.Fatal("Creating a group without miners should fail: ", err)
	}
}

// TestNewGroupNoRenterHost tests NewGroup with no renter or host
func TestNewGroupNoRenterHost(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group with nothing but miners
	groupParams := GroupParams{
		Hosts:   0,
		Renters: 0,
		Miners:  5,
	}
	// Create the group
	tg, err := NewGroupFromTemplate(uplotestTestDir(t.Name()), groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}

// TestAddNewNode tests that the added node is returned when AddNodes is called
func TestAddNewNode(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group
	groupParams := GroupParams{
		Renters: 2,
		Miners:  1,
	}
	groupDir := uplotestTestDir(t.Name())
	tg, err := NewGroupFromTemplate(groupDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Record current nodes
	oldRenters := tg.Renters()

	// Test adding a node
	renterTemplate := node.Renter(filepath.Join(groupDir, "renter"))
	nodes, err := tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("More nodes returned than expected; expected 1 got %v", len(nodes))
	}
	renter := nodes[0]
	for _, oldRenter := range oldRenters {
		if oldRenter.primarySeed == renter.primarySeed {
			t.Fatal("Returned renter is not the new renter")
		}
	}
}

// TestNewGroupPortal tests NewGroup with a portal
func TestNewGroupPortal(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Initiate a group with hosts and a miner.
	groupParams := GroupParams{
		Hosts:  4,
		Miners: 1,
	}
	// Create the group
	groupDir := uplotestTestDir(t.Name())
	tg, err := NewGroupFromTemplate(groupDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add two portals with allowances that only require 1 host. This tests the
	// portals expecting more contracts than what is defined by the allowance.
	a := DefaultAllowance
	a.Hosts = 1
	portal1Params := node.Renter(filepath.Join(groupDir, "/portal1"))
	portal1Params.CreatePortal = true
	portal1Params.Allowance = a
	portal2Params := node.Renter(filepath.Join(groupDir, "/portal2"))
	portal2Params.CreatePortal = true
	portal2Params.Allowance = a
	_, err = tg.AddNodes(portal1Params, portal2Params)
	if err != nil {
		t.Fatal("Failed to add portals to group: ", err)
	}

	// The Test Group should have the portal listed as a renter as well
	portals := tg.Portals()
	if len(tg.Renters()) != len(portals) {
		t.Fatal("Expected same number of renters and portals")
	}

	// Grab portals
	p1 := portals[0]
	p2 := portals[1]

	// Check that neither portal is in a renew window
	err1 := RenterContractsStable(p1, tg)
	err2 := RenterContractsStable(p2, tg)
	err = errors.Compose(err1, err2)
	if err != nil {
		t.Fatal(err)
	}

	// They should have the same number of contracts
	rc1, err1 := p1.RenterAllContractsGet()
	rc2, err2 := p2.RenterAllContractsGet()
	err = errors.Compose(err1, err2)
	if err != nil {
		t.Fatal(err)
	}
	p1Contracts := len(rc1.ActiveContracts) + len(rc1.PassiveContracts)
	p2Contracts := len(rc2.ActiveContracts) + len(rc2.PassiveContracts)
	if p1Contracts != p2Contracts || p1Contracts != groupParams.Hosts {
		t.Log("Portal 1 # of Contracts:", p1Contracts)
		t.Log("Portal 2 # of Contracts:", p2Contracts)
		t.Log("# of Hosts:", groupParams.Hosts)
		t.Fatal("Not enough contracts have formed")
	}

	// Have 1 portal upload a skyfile and have both portals download it
	skylink, _, _, err := p1.UploadNewSkyfileBlocking("file", 100, false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err1 = p2.SkynetSkylinkGet(skylink)
	err1 = errors.AddContext(err1, "p1 download failed")
	_, _, err2 = p2.SkynetSkylinkGet(skylink)
	err2 = errors.AddContext(err2, "p2 download failed")
	err = errors.Compose(err1, err2)
	if err != nil {
		t.Fatal(err)
	}
}
