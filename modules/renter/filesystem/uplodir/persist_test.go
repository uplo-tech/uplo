package uplodir

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
)

// equalMetadatas is a helper that compares two uplodirMetadatas. If using this
// function to check persistence the time fields should be checked in the test
// itself as well and reset due to how time is persisted
func equalMetadatas(md, md2 Metadata) error {
	// Check Aggregate Fields
	if md.AggregateHealth != md2.AggregateHealth {
		return fmt.Errorf("AggregateHealth not equal, %v and %v", md.AggregateHealth, md2.AggregateHealth)
	}
	if md.AggregateLastHealthCheckTime != md2.AggregateLastHealthCheckTime {
		return fmt.Errorf("AggregateLastHealthCheckTimes not equal, %v and %v", md.AggregateLastHealthCheckTime, md2.AggregateLastHealthCheckTime)
	}
	if md.AggregateMinRedundancy != md2.AggregateMinRedundancy {
		return fmt.Errorf("AggregateMinRedundancy not equal, %v and %v", md.AggregateMinRedundancy, md2.AggregateMinRedundancy)
	}
	if md.AggregateModTime != md2.AggregateModTime {
		return fmt.Errorf("AggregateModTimes not equal, %v and %v", md.AggregateModTime, md2.AggregateModTime)
	}
	if md.AggregateNumFiles != md2.AggregateNumFiles {
		return fmt.Errorf("AggregateNumFiles not equal, %v and %v", md.AggregateNumFiles, md2.AggregateNumFiles)
	}
	if md.AggregateNumStuckChunks != md2.AggregateNumStuckChunks {
		return fmt.Errorf("AggregateNumStuckChunks not equal, %v and %v", md.AggregateNumStuckChunks, md2.AggregateNumStuckChunks)
	}
	if md.AggregateNumSubDirs != md2.AggregateNumSubDirs {
		return fmt.Errorf("AggregateNumSubDirs not equal, %v and %v", md.AggregateNumSubDirs, md2.AggregateNumSubDirs)
	}
	if md.AggregateRemoteHealth != md2.AggregateRemoteHealth {
		return fmt.Errorf("AggregateRemoteHealth not equal, %v and %v", md.AggregateRemoteHealth, md2.AggregateRemoteHealth)
	}
	if md.AggregateRepairSize != md2.AggregateRepairSize {
		return fmt.Errorf("AggregateRepairSize not equal, %v and %v", md.AggregateRepairSize, md2.AggregateRepairSize)
	}
	if md.AggregateSize != md2.AggregateSize {
		return fmt.Errorf("AggregateSize not equal, %v and %v", md.AggregateSize, md2.AggregateSize)
	}
	if md.AggregateStuckHealth != md2.AggregateStuckHealth {
		return fmt.Errorf("AggregateStuckHealth not equal, %v and %v", md.AggregateStuckHealth, md2.AggregateStuckHealth)
	}
	if md.AggregateStuckSize != md2.AggregateStuckSize {
		return fmt.Errorf("AggregateStuckSize not equal, %v and %v", md.AggregateStuckSize, md2.AggregateStuckSize)
	}

	// Aggregate Skynet Fields
	if md.AggregateSkynetFiles != md2.AggregateSkynetFiles {
		return fmt.Errorf("AggregateSkynetFiles not equal, %v and %v", md.AggregateSkynetFiles, md2.AggregateSkynetFiles)
	}
	if md.AggregateSkynetSize != md2.AggregateSkynetSize {
		return fmt.Errorf("AggregateSkynetSize not equal, %v and %v", md.AggregateSkynetSize, md2.AggregateSkynetSize)
	}

	// Check uplodir Fields
	if md.Health != md2.Health {
		return fmt.Errorf("Healths not equal, %v and %v", md.Health, md2.Health)
	}
	if md.LastHealthCheckTime != md2.LastHealthCheckTime {
		return fmt.Errorf("LastHealthCheckTime not equal, %v and %v", md.LastHealthCheckTime, md2.LastHealthCheckTime)
	}
	if md.MinRedundancy != md2.MinRedundancy {
		return fmt.Errorf("MinRedundancy not equal, %v and %v", md.MinRedundancy, md2.MinRedundancy)
	}
	if md.ModTime != md2.ModTime {
		return fmt.Errorf("ModTime not equal, %v and %v", md.ModTime, md2.ModTime)
	}
	if md.NumFiles != md2.NumFiles {
		return fmt.Errorf("NumFiles not equal, %v and %v", md.NumFiles, md2.NumFiles)
	}
	if md.NumStuckChunks != md2.NumStuckChunks {
		return fmt.Errorf("NumStuckChunks not equal, %v and %v", md.NumStuckChunks, md2.NumStuckChunks)
	}
	if md.NumSubDirs != md2.NumSubDirs {
		return fmt.Errorf("NumSubDirs not equal, %v and %v", md.NumSubDirs, md2.NumSubDirs)
	}
	if md.RemoteHealth != md2.RemoteHealth {
		return fmt.Errorf("RemoteHealth not equal, %v and %v", md.RemoteHealth, md2.RemoteHealth)
	}
	if md.RepairSize != md2.RepairSize {
		return fmt.Errorf("RepairSize not equal, %v and %v", md.RepairSize, md2.RepairSize)
	}
	if md.Size != md2.Size {
		return fmt.Errorf("Sizes not equal, %v and %v", md.Size, md2.Size)
	}
	if md.StuckHealth != md2.StuckHealth {
		return fmt.Errorf("StuckHealth not equal, %v and %v", md.StuckHealth, md2.StuckHealth)
	}
	if md.StuckSize != md2.StuckSize {
		return fmt.Errorf("StuckSize not equal, %v and %v", md.StuckSize, md2.StuckSize)
	}

	// Skynet Fields
	if md.SkynetFiles != md2.SkynetFiles {
		return fmt.Errorf("SkynetFiles not equal, %v and %v", md.SkynetFiles, md2.SkynetFiles)
	}
	if md.SkynetSize != md2.SkynetSize {
		return fmt.Errorf("SkynetSize not equal, %v and %v", md.SkynetSize, md2.SkynetSize)
	}

	return nil
}

// newuplodirTestDir creates a test directory for a uplodir test
func newuplodirTestDir(testDir string) (string, error) {
	rootPath := filepath.Join(os.TempDir(), "uplodirs", testDir)
	if err := os.RemoveAll(rootPath); err != nil {
		return "", err
	}
	return rootPath, os.MkdirAll(rootPath, persist.DefaultDiskPermissionsTest)
}

// newTestDir creates a new uplodir for testing, the test Name should be passed
// in as the rootDir
func newTestDir(rootDir string) (*uplodir, error) {
	rootPath, err := newuplodirTestDir(rootDir)
	if err != nil {
		return nil, err
	}
	wal, _ := newTestWAL()
	return New(modules.RandomUploPath().uplodirSysPath(rootPath), rootPath, modules.DefaultDirPerm, wal)
}

// newTestWal is a helper method to create a WAL for testing.
func newTestWAL() (*writeaheadlog.WAL, string) {
	// Create the wal.
	walsDir := filepath.Join(os.TempDir(), "wals")
	if err := os.MkdirAll(walsDir, 0700); err != nil {
		panic(err)
	}
	walFilePath := filepath.Join(walsDir, hex.EncodeToString(fastrand.Bytes(8)))
	_, wal, err := writeaheadlog.New(walFilePath)
	if err != nil {
		panic(err)
	}
	return wal, walFilePath
}

// TestIsuplodirUpdate tests the IsuplodirUpdate method.
func TestIsuplodirUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sd, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	metadataUpdate, err := createMetadataUpdate(sd.Path(), Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	deleteUpdate := sd.createDeleteUpdate()
	emptyUpdate := writeaheadlog.Update{}

	if !IsuplodirUpdate(metadataUpdate) {
		t.Error("metadataUpdate should be a uplodirUpdate but wasn't")
	}
	if !IsuplodirUpdate(deleteUpdate) {
		t.Error("deleteUpdate should be a uplodirUpdate but wasn't")
	}
	if IsuplodirUpdate(emptyUpdate) {
		t.Error("emptyUpdate shouldn't be a uplodirUpdate but was one")
	}
}

// TestCreateReadMetadataUpdate tests if an update can be created using createMetadataUpdate
// and if the created update can be read using readMetadataUpdate.
func TestCreateReadMetadataUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sd, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	// Create metadata update
	path := filepath.Join(sd.path, modules.uplodirExtension)
	update, err := createMetadataUpdate(path, sd.metadata)
	if err != nil {
		t.Fatal(err)
	}

	// Read metadata update
	data, path, err := readMetadataUpdate(update)
	if err != nil {
		t.Fatal("Failed to read update", err)
	}

	// Check path
	path2 := filepath.Join(sd.path, modules.uplodirExtension)
	if path != path2 {
		t.Fatalf("Path not correct: expected %v got %v", path2, path)
	}

	// Check data
	var metadata Metadata
	err = json.Unmarshal(data, &metadata)
	if err != nil {
		t.Fatal(err)
	}
	// Check Time separately due to how the time is persisted
	if !metadata.AggregateLastHealthCheckTime.Equal(sd.metadata.AggregateLastHealthCheckTime) {
		t.Fatalf("AggregateLastHealthCheckTimes not equal, got %v expected %v", metadata.AggregateLastHealthCheckTime, sd.metadata.AggregateLastHealthCheckTime)
	}
	sd.metadata.AggregateLastHealthCheckTime = metadata.AggregateLastHealthCheckTime
	if !metadata.LastHealthCheckTime.Equal(sd.metadata.LastHealthCheckTime) {
		t.Fatalf("LastHealthCheckTimes not equal, got %v expected %v", metadata.LastHealthCheckTime, sd.metadata.LastHealthCheckTime)
	}
	sd.metadata.LastHealthCheckTime = metadata.LastHealthCheckTime
	if !metadata.AggregateModTime.Equal(sd.metadata.AggregateModTime) {
		t.Fatalf("AggregateModTimes not equal, got %v expected %v", metadata.AggregateModTime, sd.metadata.AggregateModTime)
	}
	sd.metadata.AggregateModTime = metadata.AggregateModTime
	if !metadata.ModTime.Equal(sd.metadata.ModTime) {
		t.Fatalf("ModTimes not equal, got %v expected %v", metadata.ModTime, sd.metadata.ModTime)
	}
	sd.metadata.ModTime = metadata.ModTime
	if err := equalMetadatas(metadata, sd.metadata); err != nil {
		t.Fatal(err)
	}
}

// TestCreateReadDeleteUpdate tests if an update can be created using
// createDeleteUpdate and if the created update can be read using
// readDeleteUpdate.
func TestCreateReadDeleteUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sd, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	update := sd.createDeleteUpdate()
	// Read update
	path := readDeleteUpdate(update)
	// Compare values
	uplodirPath := sd.path
	if path != uplodirPath {
		t.Error("paths don't match")
	}
}

// TestApplyUpdates tests a variety of functions that are used to apply
// updates.
func TestApplyUpdates(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	t.Run("TestApplyUpdates", func(t *testing.T) {
		uplodir, err := newTestDir(t.Name())
		if err != nil {
			t.Fatal(err)
		}
		testApply(t, uplodir, ApplyUpdates)
	})
	t.Run("TestuplodirApplyUpdates", func(t *testing.T) {
		uplodir, err := newTestDir(t.Name())
		if err != nil {
			t.Fatal(err)
		}
		testApply(t, uplodir, uplodir.applyUpdates)
	})
	t.Run("TestCreateAndApplyTransaction", func(t *testing.T) {
		uplodir, err := newTestDir(t.Name())
		if err != nil {
			t.Fatal(err)
		}
		testApply(t, uplodir, uplodir.createAndApplyTransaction)
	})
}

// testApply tests if a given method applies a set of updates correctly.
func testApply(t *testing.T, uplodir *uplodir, apply func(...writeaheadlog.Update) error) {
	// Create an update to the metadata
	metadata := uplodir.metadata
	metadata.Health = 1.0
	path := filepath.Join(uplodir.path, modules.uplodirExtension)
	update, err := createMetadataUpdate(path, metadata)
	if err != nil {
		t.Fatal(err)
	}

	// Apply update.
	if err := apply(update); err != nil {
		t.Fatal("Failed to apply update", err)
	}
	// Open file.
	sd, err := Loaduplodir(uplodir.path, modules.ProdDependencies, uplodir.wal)
	if err != nil {
		t.Fatal("Failed to load uplodir", err)
	}
	// Check Time separately due to how the time is persisted
	if !metadata.AggregateLastHealthCheckTime.Equal(sd.metadata.AggregateLastHealthCheckTime) {
		t.Fatalf("AggregateLastHealthCheckTimes not equal, got %v expected %v", metadata.AggregateLastHealthCheckTime, sd.metadata.AggregateLastHealthCheckTime)
	}
	sd.metadata.AggregateLastHealthCheckTime = metadata.AggregateLastHealthCheckTime
	if !metadata.LastHealthCheckTime.Equal(sd.metadata.LastHealthCheckTime) {
		t.Fatalf("LastHealthCheckTimes not equal, got %v expected %v", metadata.LastHealthCheckTime, sd.metadata.LastHealthCheckTime)
	}
	sd.metadata.LastHealthCheckTime = metadata.LastHealthCheckTime
	if !metadata.AggregateModTime.Equal(sd.metadata.AggregateModTime) {
		t.Fatalf("AggregateModTimes not equal, got %v expected %v", metadata.AggregateModTime, sd.metadata.AggregateModTime)
	}
	sd.metadata.AggregateModTime = metadata.AggregateModTime
	if !metadata.ModTime.Equal(sd.metadata.ModTime) {
		t.Fatalf("ModTimes not equal, got %v expected %v", metadata.ModTime, sd.metadata.ModTime)
	}
	sd.metadata.ModTime = metadata.ModTime
	// Check if correct data was written.
	if err := equalMetadatas(metadata, sd.metadata); err != nil {
		t.Fatal(err)
	}
}

// TestCreateAndApplyTransactions tests if CreateAndApplyTransactions applies a
// set of updates correctly.
func TestManagedCreateAndApplyTransactions(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	uplodir, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	// Create an update to the metadata
	metadata := uplodir.metadata
	metadata.Health = 1.0
	path := filepath.Join(uplodir.path, modules.uplodirExtension)
	update, err := createMetadataUpdate(path, metadata)
	if err != nil {
		t.Fatal(err)
	}

	// Apply update.
	if err := CreateAndApplyTransaction(uplodir.wal, update); err != nil {
		t.Fatal("Failed to apply update", err)
	}
	// Open file.
	sd, err := Loaduplodir(uplodir.path, modules.ProdDependencies, uplodir.wal)
	if err != nil {
		t.Fatal("Failed to load uplodir", err)
	}
	// Check Time separately due to how the time is persisted
	if !metadata.AggregateLastHealthCheckTime.Equal(sd.metadata.AggregateLastHealthCheckTime) {
		t.Fatalf("AggregateLastHealthCheckTimes not equal, got %v expected %v", metadata.AggregateLastHealthCheckTime, sd.metadata.AggregateLastHealthCheckTime)
	}
	sd.metadata.AggregateLastHealthCheckTime = metadata.AggregateLastHealthCheckTime
	if !metadata.LastHealthCheckTime.Equal(sd.metadata.LastHealthCheckTime) {
		t.Fatalf("LastHealthCheckTimes not equal, got %v expected %v", metadata.LastHealthCheckTime, sd.metadata.LastHealthCheckTime)
	}
	sd.metadata.LastHealthCheckTime = metadata.LastHealthCheckTime
	if !metadata.AggregateModTime.Equal(sd.metadata.AggregateModTime) {
		t.Fatalf("AggregateModTimes not equal, got %v expected %v", metadata.AggregateModTime, sd.metadata.AggregateModTime)
	}
	sd.metadata.AggregateModTime = metadata.AggregateModTime
	if !metadata.ModTime.Equal(sd.metadata.ModTime) {
		t.Fatalf("ModTimes not equal, got %v expected %v", metadata.ModTime, sd.metadata.ModTime)
	}
	sd.metadata.ModTime = metadata.ModTime
	// Check if correct data was written.
	if err := equalMetadatas(metadata, sd.metadata); err != nil {
		t.Fatal(err)
	}
}

// TestCreateAndApplyTransactionPanic verifies that the
// createAndApplyTransaction helpers panic when the updates can't be applied.
func TestCreateAndApplyTransactionPanic(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create invalid update that triggers a panic.
	update := writeaheadlog.Update{
		Name: "invalid name",
	}

	// Declare a helper to check for a panic.
	assertRecover := func() {
		if r := recover(); r == nil {
			t.Fatalf("Expected a panic")
		}
	}

	// Run the test for both the method and function
	uplodir, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer assertRecover()
		_ = uplodir.createAndApplyTransaction(update)
	}()
	func() {
		defer assertRecover()
		_ = CreateAndApplyTransaction(uplodir.wal, update)
	}()
}

// TestCreateDirMetadataAll probes the case of a potential infinite loop in
// createDirMetadataAll
func TestCreateDirMetadataAll(t *testing.T) {
	// Ignoring errors, only checking that the functions return
	createDirMetadataAll("path", "", persist.DefaultDiskPermissionsTest)
	createDirMetadataAll("path", ".", persist.DefaultDiskPermissionsTest)
	createDirMetadataAll("path", "/", persist.DefaultDiskPermissionsTest)
}
