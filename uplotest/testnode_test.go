package uplotest

import (
	"net"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uplo-tech/uplo/node"
)

// TestNextNodeAddress probes nextNodeAddress to verify that the addresses are
// indexing properly
func TestNextNodeAddress(t *testing.T) {
	if !testing.Short() || runtime.GOOS == "darwin" {
		t.SkipNow()
	}
	t.Parallel()

	// Confirm testNodeAddressCounter is initialized correctly
	ac := newNodeAddressCounter()
	if ac.address.String() != "127.1.0.0" {
		t.Fatalf("testNodeAddressCounter initial value incorrect; got %v expected %v", ac.address.String(), "127.1.0.0")
	}

	// Check address iteration
	nextIP, err := ac.managedNextNodeAddress()
	if err != nil {
		t.Fatal(err)
	}
	if nextIP != "127.1.0.1" {
		t.Fatalf("managedNextNodeAddress value incorrect; got %v expected %v", nextIP, "127.1.0.1")
	}

	// Test address iteration across range
	ac.address = net.ParseIP("127.0.0.255")
	nextIP, err = ac.managedNextNodeAddress()
	if err != nil {
		t.Fatal(err)
	}
	if nextIP != "127.0.1.0" {
		t.Fatalf("managedNextNodeAddress value incorrect; got %v expected %v", nextIP, "127.0.1.0")
	}

	// Test address iteration across multiple range
	ac.address = net.ParseIP("127.0.255.255")
	nextIP, err = ac.managedNextNodeAddress()
	if err != nil {
		t.Fatal(err)
	}
	if nextIP != "127.1.0.0" {
		t.Fatalf("managedNextNodeAddress value incorrect; got %v expected %v", nextIP, "127.1.0.0")
	}

	// Test last address iteration
	ac.address = net.ParseIP("127.255.255.255")
	nextIP, err = ac.managedNextNodeAddress()
	if err == nil {
		t.Fatal("Should have returned an error for reaching the last available address")
	}
}

// TestNodeBlocklistConnections probes the functionality of connecting nodes and
// blocklisting nodes to confirm nodes connect as intended
func TestNodeBlocklistConnections(t *testing.T) {
	// Skip if testing short or running on mac
	if testing.Short() || runtime.GOOS == "darwin" {
		t.SkipNow()
	}
	t.Parallel()

	// Create a host and a renter and connect them
	testDir := uplotestTestDir(t.Name())
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	renter, err := NewCleanNode(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	hostParams := node.Host(filepath.Join(testDir, "host"))
	host, err := NewCleanNode(hostParams)
	if err != nil {
		t.Fatal(err)
	}
	err = connectNodes(renter, host)
	if err != nil {
		t.Fatal(err)
	}

	// Have the host Blocklist the renter, confirm they are no longer peers
	err = host.GatewaySetBlocklistPost([]string{renter.GatewayAddress().Host()})
	if err != nil {
		t.Fatal(err)
	}
	isPeer, err := renter.hasPeer(host)
	if isPeer || err != nil {
		t.Fatalf("isPeer: %v, err: %v", isPeer, err)
	}
	isPeer, err = host.hasPeer(renter)
	if isPeer || err != nil {
		t.Fatalf("isPeer: %v, err: %v", isPeer, err)
	}

	// Create a miner and connect to the group
	minerParams := node.Miner(filepath.Join(testDir, "miner"))
	miner, err := NewCleanNode(minerParams)
	if err != nil {
		t.Fatal(err)
	}
	err = connectNodes(miner, host)
	if err != nil {
		t.Fatal(err)
	}
	err = connectNodes(miner, renter)
	if err != nil {
		t.Fatal(err)
	}

	// Add another renter to the group that has the same address as the original
	// renter. This renter should not connect to the host since the host had
	// disconnected and blocklisted the original renter
	renterParams = node.Renter(filepath.Join(testDir, "renterTwo"))
	renterParams.RPCAddress = renter.GatewayAddress().Host() + ":0"
	renterTwo, err := NewCleanNode(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	err = renterTwo.GatewayConnectPost(host.GatewayAddress())
	if err == nil {
		t.Fatal("expected to not be able to connect to host")
	}
	isPeer, err = renterTwo.hasPeer(host)
	if isPeer || err != nil {
		t.Fatalf("isPeer: %v, err: %v", isPeer, err)
	}
	isPeer, err = host.hasPeer(renterTwo)
	if isPeer || err != nil {
		t.Fatalf("isPeer: %v, err: %v", isPeer, err)
	}
	err = connectNodes(renterTwo, renter)
	if err != nil {
		t.Fatal(err)
	}
	err = connectNodes(renterTwo, miner)
	if err != nil {
		t.Fatal(err)
	}

	// Reset the Host blocklist, now renterTwo should be able to connect to the
	// host
	err = host.GatewaySetBlocklistPost([]string{})
	if err != nil {
		t.Fatal(err)
	}
	err = connectNodes(renterTwo, host)
	if err != nil {
		t.Fatal(err)
	}
}
