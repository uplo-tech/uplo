package hostdb

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// quitAfterLoadDeps will quit startup in newHostDB
type quitAfterLoadDeps struct {
	modules.ProductionDependencies
}

// Send a disrupt signal to the quitAfterLoad codebreak.
func (*quitAfterLoadDeps) Disrupt(s string) bool {
	if s == "quitAfterLoad" {
		return true
	}
	return false
}

// TestSaveLoad tests that the hostdb can save and load itself.
func TestSaveLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdbt, err := newHDBTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Mine two blocks to put the hdb height at 2.
	for i := 0; i < 2; i++ {
		_, err := hdbt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Verify that the hdb height is 2.
	if hdbt.hdb.blockHeight != 2 {
		t.Fatal("test setup incorrect, hdb height needs to be 2 for remainder of test")
	}

	// Add fake hosts and a fake consensus change. The fake consensus change
	// would normally be detected and routed around, but we stunt the loading
	// process to only load the persistent fields.
	var host1, host2, host3 modules.HostDBEntry
	host1.FirstSeen = 1
	host2.FirstSeen = 2
	host3.FirstSeen = 3
	host1.PublicKey.Key = fastrand.Bytes(32)
	host2.PublicKey.Key = fastrand.Bytes(32)
	host3.PublicKey.Key = []byte("baz")
	hdbt.hdb.staticHostTree.Insert(host1)
	hdbt.hdb.staticHostTree.Insert(host2)
	hdbt.hdb.staticHostTree.Insert(host3)

	// Manually set listed Hosts and filterMode
	filteredHosts := make(map[string]types.UploPublicKey)
	filteredHosts[host1.PublicKey.String()] = host1.PublicKey
	filteredHosts[host2.PublicKey.String()] = host2.PublicKey
	filteredHosts[host3.PublicKey.String()] = host3.PublicKey
	filterMode := modules.HostDBActiveWhitelist

	// Save, close, and reload.
	hdbt.hdb.mu.Lock()
	hdbt.hdb.lastChange = modules.ConsensusChangeID{1, 2, 3}
	hdbt.hdb.disableIPViolationCheck = true
	stashedLC := hdbt.hdb.lastChange
	hdbt.hdb.filteredHosts = filteredHosts
	hdbt.hdb.filterMode = filterMode
	err = hdbt.hdb.saveSync()
	hdbt.hdb.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	err = hdbt.hdb.Close()
	if err != nil {
		t.Fatal(err)
	}
	var errChan <-chan error
	hdbt.hdb, errChan = NewCustomHostDB(hdbt.gateway, hdbt.cs, hdbt.tpool, hdbt.mux, filepath.Join(hdbt.persistDir, modules.RenterDir), &quitAfterLoadDeps{})
	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	// Last change and disableIPViolationCheck should have been reloaded.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		hdbt.hdb.mu.Lock()
		lastChange := hdbt.hdb.lastChange
		disableIPViolationCheck := hdbt.hdb.disableIPViolationCheck
		hdbt.hdb.mu.Unlock()
		if lastChange != stashedLC {
			return fmt.Errorf("wrong consensus change ID was loaded: %v", hdbt.hdb.lastChange)
		}
		if disableIPViolationCheck != true {
			return errors.New("disableIPViolationCheck should've been true but was false")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Check that AllHosts was loaded.
	h1, ok0 := hdbt.hdb.staticHostTree.Select(host1.PublicKey)
	h2, ok1 := hdbt.hdb.staticHostTree.Select(host2.PublicKey)
	h3, ok2 := hdbt.hdb.staticHostTree.Select(host3.PublicKey)
	if !ok0 || !ok1 || !ok2 || len(hdbt.hdb.staticHostTree.All()) != 3 {
		t.Error("allHosts was not restored properly", ok0, ok1, ok2, len(hdbt.hdb.staticHostTree.All()))
	}
	if h1.FirstSeen != 1 {
		t.Error("h1 block height loaded incorrectly")
	}
	if h2.FirstSeen != 2 {
		t.Error("h1 block height loaded incorrectly")
	}
	if h3.FirstSeen != 2 {
		t.Error("h1 block height loaded incorrectly")
	}

	// Check that FilterMode was saved
	if hdbt.hdb.filterMode != modules.HostDBActiveWhitelist {
		t.Error("filter mode should be whitelist")
	}
	if _, ok := hdbt.hdb.filteredHosts[host1.PublicKey.String()]; !ok {
		t.Error("host1 not found in filteredHosts")
	}
	if _, ok := hdbt.hdb.filteredHosts[host2.PublicKey.String()]; !ok {
		t.Error("host2 not found in filteredHosts")
	}
	if _, ok := hdbt.hdb.filteredHosts[host3.PublicKey.String()]; !ok {
		t.Error("host3 not found in filteredHosts")
	}
}

// TestRescan tests that the hostdb will rescan the blockchain properly, picking
// up new hosts which appear in an alternate past.
func TestRescan(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	_, err := newHDBTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	t.Skip("create two consensus sets with blocks + announcements")
}
