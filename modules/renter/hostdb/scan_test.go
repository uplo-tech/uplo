package hostdb

import (
	"errors"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// TestUpdateEntry checks that the various components of updateEntry are
// working correctly.
func TestUpdateEntry(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	hdbt, err := newHDBTesterDeps(t.Name(), &disableScanLoopDeps{})
	if err != nil {
		t.Fatal(err)
	}

	// Test 1: try calling updateEntry with a blank host. Result should be a
	// host with len 2 scan history.
	someErr := errors.New("testing err")
	entry1 := modules.HostDBEntry{
		PublicKey: types.UploPublicKey{
			Key: []byte{1},
		},
	}
	entry2 := modules.HostDBEntry{
		PublicKey: types.UploPublicKey{
			Key: []byte{2},
		},
	}

	// Try inserting the first entry. Result in the host tree should be a host
	// with a scan history length of two.
	hdbt.hdb.updateEntry(entry1, nil)
	updatedEntry, exists := hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	if len(updatedEntry.ScanHistory) != 2 {
		t.Fatal("new entry was not given two scanning history entries")
	}
	if !updatedEntry.ScanHistory[0].Timestamp.Before(updatedEntry.ScanHistory[1].Timestamp) {
		t.Error("new entry was not provided with a sorted scanning history")
	}
	if !updatedEntry.ScanHistory[0].Success || !updatedEntry.ScanHistory[1].Success {
		t.Error("new entry was not given success values despite a successful scan")
	}

	// Try inserting the second entry, but with an error. Results should largely
	// be the same.
	hdbt.hdb.updateEntry(entry2, someErr)
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry2.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	if len(updatedEntry.ScanHistory) != 2 {
		t.Fatal("new entry was not given two scanning history entries")
	}
	if !updatedEntry.ScanHistory[0].Timestamp.Before(updatedEntry.ScanHistory[1].Timestamp) {
		t.Error("new entry was not provided with a sorted scanning history")
	}
	if updatedEntry.ScanHistory[0].Success || updatedEntry.ScanHistory[1].Success {
		t.Error("new entry was not given success values despite a successful scan")
	}

	// Try inserting the entry twice rapidly, nothing should change in the scan
	// history length because it won't accept such a short turnaround.
	hdbt.hdb.updateEntry(entry1, nil)
	hdbt.hdb.updateEntry(entry1, nil)
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	if len(updatedEntry.ScanHistory) != 2 {
		t.Fatal("new updates should have been ignored", len(updatedEntry.ScanHistory))
	}

	// Insert the first entry twice more, with no error. There should be 4
	// entries, and the timestamps should be strictly increasing. Sleep for a
	// bit between each update, because the hostdb during testing will not count
	// scans if they are added too close together.
	time.Sleep(3 * scanTimeElapsedRequirement)
	hdbt.hdb.updateEntry(entry1, nil)
	time.Sleep(3 * scanTimeElapsedRequirement)
	hdbt.hdb.updateEntry(entry1, nil)
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	if len(updatedEntry.ScanHistory) != 4 {
		t.Fatal("new entry was not given two scanning history entries", len(updatedEntry.ScanHistory))
	}
	if !updatedEntry.ScanHistory[1].Timestamp.Before(updatedEntry.ScanHistory[2].Timestamp) {
		t.Error("new entry was not provided with a sorted scanning history")
	}
	if !updatedEntry.ScanHistory[2].Timestamp.Before(updatedEntry.ScanHistory[3].Timestamp) {
		t.Error("new entry was not provided with a sorted scanning history")
	}
	if !updatedEntry.ScanHistory[2].Success || !updatedEntry.ScanHistory[3].Success {
		t.Error("new entries did not get added with successful timestamps")
	}

	// Add a non-successful scan and verify that it is registered properly.
	time.Sleep(3 * scanTimeElapsedRequirement)
	hdbt.hdb.updateEntry(entry1, someErr)
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	if len(updatedEntry.ScanHistory) != 5 {
		t.Fatal("new entry was not given two scanning history entries")
	}
	if !updatedEntry.ScanHistory[3].Success || updatedEntry.ScanHistory[4].Success {
		t.Error("new entries did not get added with successful timestamps")
	}

	// Prefix an invalid entry to have a scan from more than maxHostDowntime
	// days ago. At less than minScans total, the host should not be deleted
	// upon update.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry2.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	updatedEntry.ScanHistory = append([]modules.HostDBScan{{}}, updatedEntry.ScanHistory...)
	err = hdbt.hdb.staticHostTree.Modify(updatedEntry)
	if err != nil {
		t.Fatal(err)
	}
	// Entry should still exist.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry2.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	// Add enough entries to get to minScans total length. When that length is
	// reached, the entry should be deleted.
	for i := len(updatedEntry.ScanHistory); i < minScans; i++ {
		time.Sleep(3 * scanTimeElapsedRequirement)
		hdbt.hdb.updateEntry(entry2, someErr)
	}
	// The entry should no longer exist in the hostdb, wiped for being offline.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry2.PublicKey)
	if exists {
		t.Fatal("entry should have been purged for being offline for too long")
	}

	// Trigger compression on entry1 by adding a past scan and then adding
	// unsuccessful scans until compression happens.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	updatedEntry.ScanHistory = append([]modules.HostDBScan{{Timestamp: time.Now().Add(maxHostDowntime * -1).Add(time.Hour * -1)}}, updatedEntry.ScanHistory...)
	err = hdbt.hdb.staticHostTree.Modify(updatedEntry)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(updatedEntry.ScanHistory); i <= minScans; i++ {
		time.Sleep(3 * scanTimeElapsedRequirement)
		hdbt.hdb.updateEntry(entry1, someErr)
	}
	// The result should be compression, and not the entry getting deleted.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("entry should not have been purged for being offline for too long")
	}
	if len(updatedEntry.ScanHistory) != minScans {
		t.Error("expecting a different number of scans", len(updatedEntry.ScanHistory))
	}
	if updatedEntry.HistoricDowntime == 0 {
		t.Error("host reporting historic downtime?")
	}
	if updatedEntry.HistoricUptime != 0 {
		t.Error("host not reporting historic uptime?")
	}

	// Repeat triggering compression, but with uptime this time.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	updatedEntry.ScanHistory = append([]modules.HostDBScan{{Success: true, Timestamp: time.Now().Add(time.Hour * 24 * (maxHostDownTimeInDays + 1) * -1)}}, updatedEntry.ScanHistory...)
	err = hdbt.hdb.staticHostTree.Modify(updatedEntry)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * scanTimeElapsedRequirement)
	hdbt.hdb.updateEntry(entry1, someErr)
	// The result should be compression, and not the entry getting deleted.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry1.PublicKey)
	if !exists {
		t.Fatal("entry should not have been purged for being offline for too long")
	}
	if len(updatedEntry.ScanHistory) != minScans+1 {
		t.Error("expecting a different number of scans")
	}
	if updatedEntry.HistoricUptime == 0 {
		t.Error("host not reporting historic uptime?")
	}
}

// TestFeeChangeSignificant is a unit test for the feeChangeSignificant
// function.
func TestFeeChangeSignificant(t *testing.T) {
	// If the difference is exactly txnFeesUpdateRatio it is significant.
	n1 := uint64(100)
	n2 := uint64(100 * (1 + txnFeesUpdateRatio))
	s := feeChangeSignificant(types.NewCurrency64(n1), types.NewCurrency64(n2))
	if !s {
		t.Fatalf("should be significant but wasn't")
	}
	n1 = uint64(100)
	n2 = uint64(100 * (1 - txnFeesUpdateRatio))
	s = feeChangeSignificant(types.NewCurrency64(n1), types.NewCurrency64(n2))
	if !s {
		t.Fatalf("should be significant but wasn't")
	}
	// If the difference is bigger than txnFeesUpdateRatio it is significant.
	n1 = uint64(100)
	n2 = uint64(100*(1+txnFeesUpdateRatio) + 1)
	s = feeChangeSignificant(types.NewCurrency64(n1), types.NewCurrency64(n2))
	if !s {
		t.Fatalf("should be significant but wasn't")
	}
	n1 = uint64(100)
	n2 = uint64(100*(1-txnFeesUpdateRatio) - 1)
	s = feeChangeSignificant(types.NewCurrency64(n1), types.NewCurrency64(n2))
	if !s {
		t.Fatalf("should be significant but wasn't")
	}
	// If the difference is a bit less then it shouldn't be significant.
	n1 = uint64(100)
	n2 = uint64(100*(1+txnFeesUpdateRatio) - 1)
	s = feeChangeSignificant(types.NewCurrency64(n1), types.NewCurrency64(n2))
	if s {
		t.Fatalf("shouldn't be significant but was")
	}
	n1 = uint64(100)
	n2 = uint64(100*(1-txnFeesUpdateRatio) + 1)
	s = feeChangeSignificant(types.NewCurrency64(n1), types.NewCurrency64(n2))
	if s {
		t.Fatalf("shouldn't be significant but was")
	}
}

// TestUpdateEntryWithKnown host checks that a host from a knownContract (i.e. a
// host we have a contract with currently) is never deleted from the host tree.
func TestUpdateEntryWithKnownHost(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	hdbt, err := newHDBTesterDeps(t.Name(), &disableScanLoopDeps{})
	if err != nil {
		t.Fatal(err)
	}

	entry := modules.HostDBEntry{
		PublicKey: types.UploPublicKey{
			Key: []byte{1},
		},
	}
	// we need an err in updateEntry to make all interactions unsuccessful.
	someErr := errors.New("testing err")

	// Add the host from that entry to the knownContracts map.
	hdbt.hdb.mu.Lock()
	hdbt.hdb.knownContracts[entry.PublicKey.String()] = contractInfo{HostPublicKey: entry.PublicKey}
	hdbt.hdb.mu.Unlock()

	time.Sleep(3 * scanTimeElapsedRequirement)
	hdbt.hdb.updateEntry(entry, someErr)
	updatedEntry, exists := hdbt.hdb.staticHostTree.Select(entry.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}
	if len(updatedEntry.ScanHistory) != 2 {
		t.Fatal("new entry was not given two scanning history entries")
	}

	// Prefix an invalid entry to have a scan from more than maxHostDowntime
	// days ago. At less than minScans total, the host should not be deleted
	// upon update.
	updatedEntry.ScanHistory = append([]modules.HostDBScan{{}}, updatedEntry.ScanHistory...)
	err = hdbt.hdb.staticHostTree.Modify(updatedEntry)
	if err != nil {
		t.Fatal(err)
	}
	// Entry should still exist.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry.PublicKey)
	if !exists {
		t.Fatal("Entry did not get inserted into the host tree")
	}

	// Add enough entries to get to minScans total length.
	for i := len(updatedEntry.ScanHistory); i < minScans; i++ {
		time.Sleep(3 * scanTimeElapsedRequirement)
		hdbt.hdb.updateEntry(entry, someErr)
	}
	// The entry should **still** exist in the hostdb, despite being offline.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry.PublicKey)
	if !exists {
		t.Fatal("entry should not have been purged for being offline for too long")
	}

	// Remove the host from the hostContracts map and update with the same entry. It should
	// now be deleted.
	hdbt.hdb.mu.Lock()
	delete(hdbt.hdb.knownContracts, updatedEntry.PublicKey.String())
	hdbt.hdb.mu.Unlock()
	time.Sleep(3 * scanTimeElapsedRequirement)
	hdbt.hdb.updateEntry(entry, someErr)

	// Entry should not exist.
	updatedEntry, exists = hdbt.hdb.staticHostTree.Select(entry.PublicKey)
	if exists {
		t.Fatal("Entry did not get removed from the host tree")
	}
}
