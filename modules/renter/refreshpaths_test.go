package renter

import (
	"fmt"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest/dependencies"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// TestAddUniqueRefreshPaths probes the addUniqueRefreshPaths function
func TestAddUniqueRefreshPaths(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a Renter
	rt, err := newRenterTesterWithDependency(t.Name(), &dependencies.DependencyDisableRepairAndHealthLoops{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create some directory tree paths
	paths := []modules.UploPath{
		modules.RootUploPath(),
		{Path: ""},
		{Path: "root"},
		{Path: "root/SubDir1"},
		{Path: "root/SubDir1/SubDir1"},
		{Path: "root/SubDir1/SubDir1/SubDir1"},
		{Path: "root/SubDir1/SubDir2"},
		{Path: "root/SubDir2"},
		{Path: "root/SubDir2/SubDir1"},
		{Path: "root/SubDir2/SubDir2"},
		{Path: "root/SubDir2/SubDir2/SubDir2"},
	}

	// Create a map of directories to be refreshed
	dirsToRefresh := rt.renter.newUniqueRefreshPaths()

	// Add all paths to map
	for _, path := range paths {
		err = dirsToRefresh.callAdd(path)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Randomly add more paths
	for i := 0; i < 10; i++ {
		err = dirsToRefresh.callAdd(paths[fastrand.Intn(len(paths))])
		if err != nil {
			t.Fatal(err)
		}
	}

	// Verify the child and parent dir maps
	uniquePaths := []modules.UploPath{
		{Path: "root/SubDir1/SubDir1/SubDir1"},
		{Path: "root/SubDir1/SubDir2"},
		{Path: "root/SubDir2/SubDir1"},
		{Path: "root/SubDir2/SubDir2/SubDir2"},
	}
	childDirs := dirsToRefresh.callNumChildDirs()
	if childDirs != len(uniquePaths) {
		t.Fatalf("Expected %v paths in child dir map but got %v", len(uniquePaths), childDirs)
	}
	dirsToRefresh.mu.Lock()
	for _, path := range uniquePaths {
		if _, ok := dirsToRefresh.childDirs[path]; !ok {
			t.Fatal("Did not find path in map", path)
		}
	}
	dirsToRefresh.mu.Unlock()
	parentPaths := []modules.UploPath{
		{Path: ""},
		{Path: "root"},
		{Path: "root/SubDir1"},
		{Path: "root/SubDir1/SubDir1"},
		{Path: "root/SubDir2"},
		{Path: "root/SubDir2/SubDir2"},
	}
	parentDir := dirsToRefresh.callNumParentDirs()
	if parentDir != len(parentPaths) {
		t.Fatalf("Expected %v paths in parent dir map but got %v", len(parentPaths), parentDir)
	}
	dirsToRefresh.mu.Lock()
	for _, path := range parentPaths {
		if _, ok := dirsToRefresh.parentDirs[path]; !ok {
			t.Fatal("Did not find path in map", path)
		}
	}
	dirsToRefresh.mu.Unlock()

	// Make child directories and add a file to each
	rsc, _ := modules.NewRSCode(1, 1)
	up := modules.FileUploadParams{
		Source:      "",
		ErasureCode: rsc,
	}
	for _, sp := range uniquePaths {
		err = rt.renter.CreateDir(sp, modules.DefaultDirPerm)
		if err != nil {
			t.Fatal(err)
		}
		up.UploPath, err = sp.Join("testFile")
		if err != nil {
			t.Fatal(err)
		}
		err = rt.renter.staticFileSystem.NewUploFile(up.UploPath, up.Source, up.ErasureCode, crypto.GenerateUploKey(crypto.RandomCipherType()), 100, persist.DefaultDiskPermissionsTest, up.DisablePartialChunk)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Check the metadata of the root directory. Because we added files by
	// directly calling the staticFileSystem, a bubble should not have been
	// triggered and therefore the number of total files should be 0
	di, err := rt.renter.DirList(modules.RootUploPath())
	if err != nil {
		t.Fatal(err)
	}
	if di[0].AggregateNumFiles != 0 {
		t.Fatal("Expected AggregateNumFiles to be 0 but got", di[0].AggregateNumFiles)
	}
	if di[0].AggregateNumSubDirs != 0 {
		t.Fatal("Expected AggregateNumSubDirs to be 0 but got", di[0].AggregateNumSubDirs)
	}

	// Add the default folders to the uniqueRefreshPaths to have their information
	// bubbled as well
	err1 := dirsToRefresh.callAdd(modules.HomeFolder)
	err2 := dirsToRefresh.callAdd(modules.UserFolder)
	err3 := dirsToRefresh.callAdd(modules.VarFolder)
	err4 := dirsToRefresh.callAdd(modules.SkynetFolder)
	err5 := dirsToRefresh.callAdd(modules.BackupFolder)
	err = errors.Compose(err1, err2, err3, err4, err5)
	if err != nil {
		t.Fatal(err)
	}

	// Have uniqueBubblePaths call bubble
	err = dirsToRefresh.callRefreshAllBlocking()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for root directory to show proper number of files and subdirs.
	numSubDirs := len(dirsToRefresh.parentDirs) + len(dirsToRefresh.childDirs) - 1
	err = build.Retry(100, 100*time.Millisecond, func() error {
		di, err = rt.renter.DirList(modules.RootUploPath())
		if err != nil {
			return err
		}
		if int(di[0].AggregateNumFiles) != len(uniquePaths) {
			return fmt.Errorf("Expected AggregateNumFiles to be %v but got %v", len(uniquePaths), di[0].AggregateNumFiles)
		}
		if int(di[0].AggregateNumSubDirs) != numSubDirs {
			return fmt.Errorf("Expected AggregateNumSubDirs to be %v but got %v", numSubDirs, di[0].AggregateNumSubDirs)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
