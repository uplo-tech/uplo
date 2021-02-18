package uplodir

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// checkMetadataInit is a helper that verifies that the metadata was initialized
// properly
func checkMetadataInit(md Metadata) error {
	// Check that the modTimes are not Zero
	if md.AggregateModTime.IsZero() {
		return errors.New("AggregateModTime not initialized")
	}
	if md.ModTime.IsZero() {
		return errors.New("ModTime not initialized")
	}

	// All the rest of the metadata should be default values
	initMetadata := Metadata{
		AggregateHealth:        DefaultDirHealth,
		AggregateMinRedundancy: DefaultDirRedundancy,
		AggregateModTime:       md.AggregateModTime,
		AggregateRemoteHealth:  DefaultDirHealth,
		AggregateStuckHealth:   DefaultDirHealth,

		Health:        DefaultDirHealth,
		MinRedundancy: DefaultDirRedundancy,
		ModTime:       md.ModTime,
		RemoteHealth:  DefaultDirHealth,
		StuckHealth:   DefaultDirHealth,
	}

	return equalMetadatas(md, initMetadata)
}

// newRootDir creates a root directory for the test and removes old test files
func newRootDir(t *testing.T) (string, error) {
	dir := filepath.Join(os.TempDir(), "uplodirs", t.Name())
	err := os.RemoveAll(dir)
	if err != nil {
		return "", err
	}
	return dir, nil
}

// randomMetadata returns a uplodir Metadata struct with random values set
func randomMetadata() Metadata {
	md := Metadata{
		AggregateHealth:              float64(fastrand.Intn(100)),
		AggregateLastHealthCheckTime: time.Now(),
		AggregateMinRedundancy:       float64(fastrand.Intn(100)),
		AggregateModTime:             time.Now(),
		AggregateNumFiles:            fastrand.Uint64n(100),
		AggregateNumStuckChunks:      fastrand.Uint64n(100),
		AggregateNumSubDirs:          fastrand.Uint64n(100),
		AggregateRemoteHealth:        float64(fastrand.Intn(100)),
		AggregateRepairSize:          fastrand.Uint64n(100),
		AggregateSize:                fastrand.Uint64n(100),
		AggregateStuckHealth:         float64(fastrand.Intn(100)),
		AggregateStuckSize:           fastrand.Uint64n(100),

		AggregateSkynetFiles: fastrand.Uint64n(100),
		AggregateSkynetSize:  fastrand.Uint64n(100),

		Health:              float64(fastrand.Intn(100)),
		LastHealthCheckTime: time.Now(),
		MinRedundancy:       float64(fastrand.Intn(100)),
		ModTime:             time.Now(),
		NumFiles:            fastrand.Uint64n(100),
		NumStuckChunks:      fastrand.Uint64n(100),
		NumSubDirs:          fastrand.Uint64n(100),
		RemoteHealth:        float64(fastrand.Intn(100)),
		RepairSize:          fastrand.Uint64n(100),
		Size:                fastrand.Uint64n(100),
		StuckHealth:         float64(fastrand.Intn(100)),
		StuckSize:           fastrand.Uint64n(100),

		SkynetFiles: fastrand.Uint64n(100),
		SkynetSize:  fastrand.Uint64n(100),
	}
	return md
}

// TestNewuplodir tests that uplodirs are created on disk properly. It uses
// Loaduplodir to read the metadata from disk
func TestNewuplodir(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Initialize the test directory
	testDir, err := newuplodirTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create New uplodir that is two levels deep
	wal, _ := newTestWAL()
	topDir := filepath.Join(testDir, "TestDir")
	subDir := "SubDir"
	path := filepath.Join(topDir, subDir)
	uplodir, err := New(path, testDir, persist.DefaultDiskPermissionsTest, wal)
	if err != nil {
		t.Fatal(err)
	}

	// Check Sub Dir
	//
	// Check that the metadata was initialized properly
	md := uplodir.metadata
	if err = checkMetadataInit(md); err != nil {
		t.Fatal(err)
	}
	// Check that the directory and .uplodir file were created on disk
	_, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(path, modules.uplodirExtension))
	if err != nil {
		t.Fatal(err)
	}

	// Check Top Directory
	//
	// Check that the directory and .uplodir file were created on disk
	_, err = os.Stat(topDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(topDir, modules.uplodirExtension))
	if err != nil {
		t.Fatal(err)
	}
	// Get uplodir
	topuplodir, err := Loaduplodir(topDir, modules.ProdDependencies, wal)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the metadata was initialized properly
	md = topuplodir.metadata
	if err = checkMetadataInit(md); err != nil {
		t.Fatal(err)
	}

	// Check Root Directory
	//
	// Get uplodir
	rootuplodir, err := Loaduplodir(testDir, modules.ProdDependencies, wal)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the metadata was initialized properly
	md = rootuplodir.metadata
	if err = checkMetadataInit(md); err != nil {
		t.Fatal(err)
	}

	// Check that the directory and the .uplodir file were created on disk
	_, err = os.Stat(testDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(testDir, modules.uplodirExtension))
	if err != nil {
		t.Fatal(err)
	}
}

// Test UpdatedMetadata probes the UpdateMetadata method
func TestUpdateMetadata(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create new uplodir
	rootDir, err := newRootDir(t)
	if err != nil {
		t.Fatal(err)
	}
	uploPath, err := modules.NewUploPath("TestDir")
	if err != nil {
		t.Fatal(err)
	}
	UplodirSysPath := uploPath.UplodirSysPath(rootDir)
	wal, _ := newTestWAL()
	uplodir, err := New(UplodirSysPath, rootDir, modules.DefaultDirPerm, wal)
	if err != nil {
		t.Fatal(err)
	}

	// Check metadata was initialized properly in memory and on disk
	md := uplodir.metadata
	if err = checkMetadataInit(md); err != nil {
		t.Fatal(err)
	}
	uplodir, err = Loaduplodir(UplodirSysPath, modules.ProdDependencies, wal)
	if err != nil {
		t.Fatal(err)
	}
	md = uplodir.metadata
	if err = checkMetadataInit(md); err != nil {
		t.Fatal(err)
	}

	// Set the metadata
	metadataUpdate := randomMetadata()

	err = uplodir.UpdateMetadata(metadataUpdate)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the metadata was updated properly in memory and on disk
	md = uplodir.metadata
	err = equalMetadatas(md, metadataUpdate)
	if err != nil {
		t.Fatal(err)
	}
	uplodir, err = Loaduplodir(UplodirSysPath, modules.ProdDependencies, wal)
	if err != nil {
		t.Fatal(err)
	}
	md = uplodir.metadata
	// Check Time separately due to how the time is persisted
	if !md.AggregateLastHealthCheckTime.Equal(metadataUpdate.AggregateLastHealthCheckTime) {
		t.Fatalf("AggregateLastHealthCheckTimes not equal, got %v expected %v", md.AggregateLastHealthCheckTime, metadataUpdate.AggregateLastHealthCheckTime)
	}
	metadataUpdate.AggregateLastHealthCheckTime = md.AggregateLastHealthCheckTime
	if !md.LastHealthCheckTime.Equal(metadataUpdate.LastHealthCheckTime) {
		t.Fatalf("LastHealthCheckTimes not equal, got %v expected %v", md.LastHealthCheckTime, metadataUpdate.LastHealthCheckTime)
	}
	metadataUpdate.LastHealthCheckTime = md.LastHealthCheckTime
	if !md.AggregateModTime.Equal(metadataUpdate.AggregateModTime) {
		t.Fatalf("AggregateModTimes not equal, got %v expected %v", md.AggregateModTime, metadataUpdate.AggregateModTime)
	}
	metadataUpdate.AggregateModTime = md.AggregateModTime
	if !md.ModTime.Equal(metadataUpdate.ModTime) {
		t.Fatalf("ModTimes not equal, got %v expected %v", md.ModTime, metadataUpdate.ModTime)
	}
	metadataUpdate.ModTime = md.ModTime
	// Check the rest of the metadata
	err = equalMetadatas(md, metadataUpdate)
	if err != nil {
		t.Fatal(err)
	}
}

// TestuplodirDelete verifies the uplodir performs as expected after a delete
func TestuplodirDelete(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create new uplodir
	rootDir, err := newRootDir(t)
	if err != nil {
		t.Fatal(err)
	}
	uploPath, err := modules.NewUploPath("deleteddir")
	if err != nil {
		t.Fatal(err)
	}
	uplodirSysPath := uploPath.UplodirSysPath(rootDir)
	wal, _ := newTestWAL()
	uplodir, err := New(uplodirSysPath, rootDir, modules.DefaultDirPerm, wal)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the uplodir and keep uplodir in memory
	err = uplodir.Delete()
	if err != nil {
		t.Fatal(err)
	}

	// Verify functions either return or error accordingly
	//
	// First set should not error or panic
	if !uplodir.Deleted() {
		t.Error("uplodir metadata should reflect the deletion")
	}
	_ = uplodir.MDPath()
	_ = uplodir.Metadata()
	_ = uplodir.Path()

	// Second Set should return an error
	err = uplodir.Rename("")
	if !errors.Contains(err, ErrDeleted) {
		t.Error("Rename should return with and error for uplodir deleted")
	}
	err = uplodir.SetPath("")
	if !errors.Contains(err, ErrDeleted) {
		t.Error("SetPath should return with and error for uplodir deleted")
	}
	_, err = uplodir.DirReader()
	if !errors.Contains(err, ErrDeleted) {
		t.Error("DirReader should return with and error for uplodir deleted")
	}
	uplodir.mu.Lock()
	err = uplodir.updateMetadata(Metadata{})
	if !errors.Contains(err, ErrDeleted) {
		t.Error("updateMetadata should return with and error for uplodir deleted")
	}
	uplodir.mu.Unlock()
}
