package renter

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/host/contractmanager"
	"github.com/uplo-tech/uplo/modules/renter"
	"github.com/uplo-tech/uplo/modules/renter/contractor"
	"github.com/uplo-tech/uplo/node"
	"github.com/uplo-tech/uplo/node/api"
	"github.com/uplo-tech/uplo/node/api/client"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/uplotest"
	"github.com/uplo-tech/uplo/uplotest/dependencies"
	"github.com/uplo-tech/uplo/types"
)

// TestRenterOne executes a number of subtests using the same TestGroup to save
// time on initialization
func TestRenterOne(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for the subtests
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify subtests to run
	subTests := []uplotest.SubTest{
		{Name: "TestDownloadMultipleLargeSectors", Test: testDownloadMultipleLargeSectors},
		{Name: "TestLocalRepair", Test: testLocalRepair},
		{Name: "TestClearDownloadHistory", Test: testClearDownloadHistory},
		{Name: "TestDownloadAfterRenew", Test: testDownloadAfterRenew},
		{Name: "TestDirectories", Test: testDirectories},
		{Name: "TestAlertsSorted", Test: testAlertsSorted},
		{Name: "TestPriceTablesUpdated", Test: testPriceTablesUpdated},
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// TestRenterTwo executes a number of subtests using the same TestGroup to
// save time on initialization
func TestRenterTwo(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for the subtests
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify subtests to run
	subTests := []uplotest.SubTest{
		{Name: "TestReceivedFieldEqualsFileSize", Test: testReceivedFieldEqualsFileSize},
		{Name: "TestRemoteRepair", Test: testRemoteRepair},
		{Name: "TestSingleFileGet", Test: testSingleFileGet},
		{Name: "TestUploFileTimestamps", Test: testUplofileTimestamps},
		{Name: "TestZeroByteFile", Test: testZeroByteFile},
		{Name: "TestUploadWithAndWithoutForceParameter", Test: testUploadWithAndWithoutForceParameter},
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// testUplofileTimestamps tests if timestamps are set correctly when creating,
// uploading, downloading and modifying a file.
func testUplofileTimestamps(t *testing.T, tg *uplotest.TestGroup) {
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}
	// Grab the renter.
	r := tg.Renters()[0]

	// Get the current time.
	beforeUploadTime := time.Now()

	// Upload a new file.
	_, rf, err := r.UploadNewFileBlocking(100+uplotest.Fuzz(), 1, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	// Get the time again.
	afterUploadTime := time.Now()

	// Get the timestamps using the API.
	fi, err := r.File(rf)
	if err != nil {
		t.Fatal(err)
	}

	// The timestamps should all be between beforeUploadTime and
	// afterUploadTime.
	if fi.CreateTime.Before(beforeUploadTime) || fi.CreateTime.After(afterUploadTime) {
		t.Fatal("CreateTime was not within the correct interval")
	}
	if fi.AccessTime.Before(beforeUploadTime) || fi.AccessTime.After(afterUploadTime) {
		t.Fatal("AccessTime was not within the correct interval")
	}
	if fi.ChangeTime.Before(beforeUploadTime) || fi.ChangeTime.After(afterUploadTime) {
		t.Fatal("ChangeTime was not within the correct interval")
	}
	if fi.ModificationTime.Before(beforeUploadTime) || fi.ModificationTime.After(afterUploadTime) {
		t.Fatal("ModificationTime was not within the correct interval")
	}

	// After uploading a file the AccessTime, ChangeTime and ModificationTime should be
	// the same.
	if fi.AccessTime != fi.ChangeTime || fi.ChangeTime != fi.ModificationTime {
		t.Fatal("AccessTime, ChangeTime and ModificationTime are not the same")
	}

	// The CreateTime should precede the other timestamps.
	if fi.CreateTime.After(fi.AccessTime) {
		t.Fatal("CreateTime should before other timestamps")
	}

	// Get the time before starting the download.
	beforeDownloadTime := time.Now()

	// Download the file.
	_, _, err = r.DownloadByStream(rf)
	if err != nil {
		t.Fatal(err)
	}

	// Get the time after the download is done.
	afterDownloadTime := time.Now()

	// Get the timestamps using the API.
	fi2, err := r.File(rf)
	if err != nil {
		t.Fatal(err)
	}

	// Only the AccessTime should have changed.
	if fi2.AccessTime.Before(beforeDownloadTime) || fi2.AccessTime.After(afterDownloadTime) {
		t.Fatal("AccessTime was not within the correct interval")
	}
	if fi.CreateTime != fi2.CreateTime {
		t.Fatal("CreateTime changed after download")
	}
	if fi.ChangeTime != fi2.ChangeTime {
		t.Fatal("ChangeTime changed after download")
	}
	if fi.ModificationTime != fi2.ModificationTime {
		t.Fatal("ModificationTime changed after download")
	}

	// TODO Once we can change the localPath using the API, check that it only
	// changes the ChangeTime to do so.

	// Get the time before renaming.
	beforeRenameTime := time.Now()

	newUploPath, err := modules.NewUploPath("newuplopath")
	if err != nil {
		t.Fatal(err)
	}
	// Rename the file and check that only the ChangeTime changed.
	rf, err = r.Rename(rf, newUploPath)
	if err != nil {
		t.Fatal(err)
	}

	// Get the time after renaming.
	afterRenameTime := time.Now()

	// Get the timestamps using the API.
	fi3, err := r.File(rf)
	if err != nil {
		t.Fatal(err)
	}

	// Only the ChangeTime should have changed.
	if fi3.ChangeTime.Before(beforeRenameTime) || fi3.ChangeTime.After(afterRenameTime) {
		t.Fatal("ChangeTime was not within the correct interval")
	}
	if fi2.CreateTime != fi3.CreateTime {
		t.Fatal("CreateTime changed after download")
	}
	if fi2.AccessTime != fi3.AccessTime {
		t.Fatal("AccessTime changed after download")
	}
	if fi2.ModificationTime != fi3.ModificationTime {
		t.Fatal("ModificationTime changed after download")
	}
}

// TestRenterThree executes a number of subtests using the same TestGroup to
// save time on initialization
func TestRenterThree(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for the subtests
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify subtests to run
	subTests := []uplotest.SubTest{
		{Name: "TestAllowanceDefaultSet", Test: testAllowanceDefaultSet},
		{Name: "TestFileAvailableAndRecoverable", Test: testFileAvailableAndRecoverable},
		{Name: "TestSetFileStuck", Test: testSetFileStuck},
		{Name: "TestCancelAsyncDownload", Test: testCancelAsyncDownload},
		{Name: "TestUploadDownload", Test: testUploadDownload}, // Needs to be last as it impacts hosts
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// TestRenterFour executes a number of subtests using the same TestGroup to
// save time on initialization
func TestRenterFour(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for the subtests
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify subtests to run
	subTests := []uplotest.SubTest{
		{Name: "TestValidateUploPath", Test: testValidateUploPath},
		{Name: "TestNextPeriod", Test: testNextPeriod},
		{Name: "TestPauseAndResumeRepairAndUploads", Test: testPauseAndResumeRepairAndUploads},
		{Name: "TestDownloadServedFromDisk", Test: testDownloadServedFromDisk},
		{Name: "TestDirMode", Test: testDirMode},
		{Name: "TestEscapeUploPath", Test: testEscapeUploPath}, // Runs last because it uploads many files
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// testAllowanceDefaultSet tests that a renter's allowance is correctly set to
// the defaults after creating it and therefore confirming that the API
// endpoint and uplotest package both work.
func testAllowanceDefaultSet(t *testing.T, tg *uplotest.TestGroup) {
	if len(tg.Renters()) == 0 {
		t.Fatal("Test requires at least 1 renter")
	}
	// Get allowance.
	r := tg.Renters()[0]
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	// Make sure that the allowance was set correctly.
	if !reflect.DeepEqual(rg.Settings.Allowance, uplotest.DefaultAllowance) {
		expected, _ := json.Marshal(uplotest.DefaultAllowance)
		was, _ := json.Marshal(rg.Settings.Allowance)
		t.Log("Expected", string(expected))
		t.Log("Was", string(was))
		t.Fatal("Renter's allowance doesn't match uplotest.DefaultAllowance")
	}
}

// testReceivedFieldEqualsFileSize tests that the bug that caused finished
// downloads to stall in the UI and uploc is gone.
func testReceivedFieldEqualsFileSize(t *testing.T, tg *uplotest.TestGroup) {
	// Make sure the test has enough hosts.
	if len(tg.Hosts()) < 4 {
		t.Fatal("testReceivedFieldEqualsFileSize requires at least 4 hosts")
	}
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	// Clear the download history to make sure it's empty before we start the test.
	err := r.RenterClearAllDownloadsPost()
	if err != nil {
		t.Fatal(err)
	}

	// Upload a file.
	dataPieces := uint64(3)
	parityPieces := uint64(1)
	fileSize := int(modules.SectorSize)
	lf, rf, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}

	// This code sums up the 'received' variable in a similar way the renter
	// does it. We use it to find a fetchLen for which received != fetchLen due
	// to the implicit rounding of the unsigned integers.
	var fetchLen uint64
	for fetchLen = uint64(100); ; fetchLen++ {
		received := uint64(0)
		for piecesCompleted := uint64(1); piecesCompleted <= dataPieces; piecesCompleted++ {
			received += fetchLen / dataPieces
		}
		if received != fetchLen {
			break
		}
	}

	// Download fetchLen bytes of the file.
	_, _, err = r.DownloadToDiskPartial(rf, lf, false, 0, fetchLen)
	if err != nil {
		t.Fatal(err)
	}

	// Get the download.
	rdg, err := r.RenterDownloadsGet()
	if err != nil {
		t.Fatal(err)
	}
	d := rdg.Downloads[0]

	// Make sure that 'Received' matches the amount of data we fetched.
	if !d.Completed {
		t.Error("Download should be completed but wasn't")
	}
	if d.Received != fetchLen {
		t.Errorf("Received was %v but should be %v", d.Received, fetchLen)
	}
	// Compare uplopaths.
	rdgr, err := r.RenterDownloadsRootGet()
	if err != nil {
		t.Fatal(err)
	}
	if !d.UploPath.Equals(rf.UploPath()) {
		t.Fatal(d.UploPath.String(), rf.UploPath().String())
	}
	sp, err := rf.UploPath().Rebase(modules.RootUploPath(), modules.UserFolder)
	if err != nil {
		t.Fatal(err)
	}
	if !rdgr.Downloads[0].UploPath.Equals(sp) {
		t.Fatal(d.UploPath.String(), rf.UploPath().String())
	}
}

// testClearDownloadHistory makes sure that the download history is
// properly cleared when called through the API
func testClearDownloadHistory(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	rdg, err := r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	numDownloads := 10
	if len(rdg.Downloads) < numDownloads {
		remainingDownloads := numDownloads - len(rdg.Downloads)
		rf, err := r.RenterFilesGet(false)
		if err != nil {
			t.Fatal(err)
		}
		// Check if the renter has any files
		// Upload a file if none
		if len(rf.Files) == 0 {
			dataPieces := uint64(1)
			parityPieces := uint64(1)
			fileSize := 100 + uplotest.Fuzz()
			_, _, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
			if err != nil {
				t.Fatal("Failed to upload a file for testing: ", err)
			}
			rf, err = r.RenterFilesGet(false)
			if err != nil {
				t.Fatal(err)
			}
		}
		// Download files to build download history
		dest := filepath.Join(uplotest.UploTestingDir, strconv.Itoa(fastrand.Intn(math.MaxInt32)))
		for i := 0; i < remainingDownloads; i++ {
			_, err = r.RenterDownloadGet(rf.Files[0].UploPath, dest, 0, rf.Files[0].Filesize, false, false, false)
			if err != nil {
				t.Fatal("Could not Download file:", err)
			}
		}
		rdg, err = r.RenterDownloadsGet()
		if err != nil {
			t.Fatal("Could not get download history:", err)
		}
		// Confirm download history is not empty
		if len(rdg.Downloads) != numDownloads {
			t.Fatalf("Not all downloads added to download history: only %v downloads added, expected %v", len(rdg.Downloads), numDownloads)
		}
	}
	numDownloads = len(rdg.Downloads)

	// Check removing one download from history
	// Remove First Download
	timestamp := rdg.Downloads[0].StartTime
	err = r.RenterClearDownloadsRangePost(timestamp, timestamp)
	if err != nil {
		t.Fatal("Error in API endpoint to remove download from history:", err)
	}
	numDownloads--
	rdg, err = r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	if len(rdg.Downloads) != numDownloads {
		t.Fatalf("Download history not reduced: history has %v downloads, expected %v", len(rdg.Downloads), numDownloads)
	}
	i := sort.Search(len(rdg.Downloads), func(i int) bool { return rdg.Downloads[i].StartTime.Equal(timestamp) })
	if i < len(rdg.Downloads) {
		t.Fatal("Specified download not removed from history")
	}
	// Remove Last Download
	timestamp = rdg.Downloads[len(rdg.Downloads)-1].StartTime
	err = r.RenterClearDownloadsRangePost(timestamp, timestamp)
	if err != nil {
		t.Fatal("Error in API endpoint to remove download from history:", err)
	}
	numDownloads--
	rdg, err = r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	if len(rdg.Downloads) != numDownloads {
		t.Fatalf("Download history not reduced: history has %v downloads, expected %v", len(rdg.Downloads), numDownloads)
	}
	i = sort.Search(len(rdg.Downloads), func(i int) bool { return rdg.Downloads[i].StartTime.Equal(timestamp) })
	if i < len(rdg.Downloads) {
		t.Fatal("Specified download not removed from history")
	}

	// Check Clear Before
	timestamp = rdg.Downloads[len(rdg.Downloads)-2].StartTime
	err = r.RenterClearDownloadsBeforePost(timestamp)
	if err != nil {
		t.Fatal("Error in API endpoint to clear download history before timestamp:", err)
	}
	rdg, err = r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	i = sort.Search(len(rdg.Downloads), func(i int) bool { return rdg.Downloads[i].StartTime.Before(timestamp) })
	if i < len(rdg.Downloads) {
		t.Fatal("Download found that was before given time")
	}

	// Check Clear After
	timestamp = rdg.Downloads[1].StartTime
	err = r.RenterClearDownloadsAfterPost(timestamp)
	if err != nil {
		t.Fatal("Error in API endpoint to clear download history after timestamp:", err)
	}
	rdg, err = r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	i = sort.Search(len(rdg.Downloads), func(i int) bool { return rdg.Downloads[i].StartTime.After(timestamp) })
	if i < len(rdg.Downloads) {
		t.Fatal("Download found that was after given time")
	}

	// Check clear range
	before := rdg.Downloads[1].StartTime
	after := rdg.Downloads[len(rdg.Downloads)-1].StartTime
	err = r.RenterClearDownloadsRangePost(after, before)
	if err != nil {
		t.Fatal("Error in API endpoint to remove range of downloads from history:", err)
	}
	rdg, err = r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	i = sort.Search(len(rdg.Downloads), func(i int) bool {
		return rdg.Downloads[i].StartTime.Before(before) && rdg.Downloads[i].StartTime.After(after)
	})
	if i < len(rdg.Downloads) {
		t.Fatal("Not all downloads from range removed from history")
	}

	// Check clearing download history
	err = r.RenterClearAllDownloadsPost()
	if err != nil {
		t.Fatal("Error in API endpoint to clear download history:", err)
	}
	rdg, err = r.RenterDownloadsGet()
	if err != nil {
		t.Fatal("Could not get download history:", err)
	}
	if len(rdg.Downloads) != 0 {
		t.Fatalf("Download history not cleared: history has %v downloads, expected 0", len(rdg.Downloads))
	}
}

// testDirectories checks the functionality of directories in the Renter
func testDirectories(t *testing.T, tg *uplotest.TestGroup) {
	// Grab Renter
	r := tg.Renters()[0]

	// Test Directory endpoint for creating empty directory
	rd, err := r.UploadNewDirectory()
	if err != nil {
		t.Fatal(err)
	}

	// Check directory
	rgd, err := r.RenterDirGet(rd.UploPath())
	if err != nil {
		t.Fatal(err)
	}
	// Directory should return 0 FileInfos and 1 DirectoryInfo with would belong
	// to the directory itself
	if len(rgd.Directories) != 1 {
		t.Fatal("Expected 1 DirectoryInfo to be returned but got:", len(rgd.Directories))
	}
	if rgd.Directories[0].UploPath != rd.UploPath() {
		t.Fatalf("UploPaths do not match %v and %v", rgd.Directories[0].UploPath, rd.UploPath())
	}
	if len(rgd.Files) != 0 {
		t.Fatal("Expected no files in directory but found:", len(rgd.Files))
	}

	// Check uploading file to new subdirectory
	// Create local file
	size := 100 + uplotest.Fuzz()
	fd := r.FilesDir()
	ld, err := fd.CreateDir("subDir1/subDir2/subDir3-" + persist.RandomSuffix())
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ld.NewFile(size)
	if err != nil {
		t.Fatal(err)
	}

	// Upload file
	dataPieces := uint64(1)
	parityPieces := uint64(1)
	rf, err := r.UploadBlocking(lf, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Check directory that file was uploaded to
	uploPath, err := rf.UploPath().Dir()
	if err != nil {
		t.Fatal(err)
	}
	rgd, err = r.RenterDirGet(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Directory should have 1 file and 0 sub directories
	if len(rgd.Directories) != 1 {
		t.Fatal("Expected 1 DirectoryInfo to be returned but got:", len(rgd.Directories))
	}
	if len(rgd.Files) != 1 {
		t.Fatal("Expected 1 file in directory but found:", len(rgd.Files))
	}

	// Check parent directory
	uploPath, err = uploPath.Dir()
	if err != nil {
		t.Fatal(err)
	}
	rgd, err = r.RenterDirGet(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Directory should have 0 files and 1 sub directory
	if len(rgd.Directories) != 2 {
		t.Fatal("Expected 2 DirectoryInfos to be returned but got:", len(rgd.Directories))
	}
	if len(rgd.Files) != 0 {
		t.Fatal("Expected 0 files in directory but found:", len(rgd.Files))
	}

	// Test renaming subdirectory
	subDir1, err := modules.NewUploPath("subDir1")
	if err != nil {
		t.Fatal(err)
	}
	newUploPath := modules.RandomUploPath()
	if err = r.RenterDirRenamePost(subDir1, newUploPath); err != nil {
		t.Fatal(err)
	}
	// Renamed directory should have 0 files and 1 sub directory.
	rgd, err = r.RenterDirGet(newUploPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rgd.Files) != 0 {
		t.Fatalf("Renamed dir should have 0 files but had %v", len(rgd.Files))
	}
	if len(rgd.Directories) != 2 {
		t.Fatalf("Renamed dir should have 1 sub directory but had %v",
			len(rgd.Directories)-1)
	}
	// Subdir of renamed dir should have 0 files and 1 sub directory.
	rgd, err = r.RenterDirGet(rgd.Directories[1].UploPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rgd.Files) != 0 {
		t.Fatalf("Renamed dir should have 0 files but had %v", len(rgd.Files))
	}
	if len(rgd.Directories) != 2 {
		t.Fatalf("Renamed dir should have 1 sub directory but had %v",
			len(rgd.Directories)-1)
	}
	// SubSubdir of renamed dir should have 1 file and 0 sub directories.
	rgd, err = r.RenterDirGet(rgd.Directories[1].UploPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rgd.Files) != 1 {
		t.Fatalf("Renamed dir should have 1 file but had %v", len(rgd.Files))
	}
	if len(rgd.Directories) != 1 {
		t.Fatalf("Renamed dir should have 0 sub directories but had %v",
			len(rgd.Directories)-1)
	}
	// Try downloading the renamed file.
	if _, _, err := r.RenterDownloadHTTPResponseGet(rgd.Files[0].UploPath, 0, uint64(size), true, false); err != nil {
		t.Fatal(err)
	}

	// Check that the old uplodir was deleted from disk
	_, err = os.Stat(subDir1.uplodirSysPath(r.RenterFilesDir()))
	if !os.IsNotExist(err) {
		t.Fatal("Expected IsNotExist err, but got err:", err)
	}

	// create a file to test file deletion
	lf1, err := ld.NewFile(size)
	if err != nil {
		t.Fatal(err)
	}
	rf1, err := r.UploadBlocking(lf1, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Test deleting a file by its relative path
	err = r.RenterFileDeletePost(rf1.UploPath())
	if err != nil {
		t.Fatal(err)
	}

	// Test deleting directory
	if err = r.RenterDirDeletePost(rd.UploPath()); err != nil {
		t.Fatal(err)
	}

	// Create a new set of remote files and dirs, so we can test deleting with a
	// root path
	rd2, err := r.UploadNewDirectory()
	if err != nil {
		t.Fatal(err)
	}
	ld2, err := fd.CreateDir("subDir1a/subDir2a/subDir3a-" + persist.RandomSuffix())
	if err != nil {
		t.Fatal(err)
	}
	lf2, err := ld2.NewFile(size)
	if err != nil {
		t.Fatal(err)
	}
	rf2, err := r.UploadBlocking(lf2, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Test deleting a file by its root path
	rf2RootPath, err := modules.NewUploPath("/home/user/" + rf2.UploPath().Path)
	err = r.RenterFileDeleteRootPost(rf2RootPath)
	if err != nil {
		t.Fatal(fmt.Errorf(err.Error() + " => " + rf2RootPath.Path))
	}

	// Test deleting directory by its root path
	rd2RootPath, err := modules.NewUploPath("/home/user/" + rd2.UploPath().Path)
	if err = r.RenterDirDeleteRootPost(rd2RootPath); err != nil {
		t.Fatal(fmt.Errorf(err.Error() + " => " + rd2RootPath.Path))
	}

	// Check that uplodir was deleted from disk
	_, err = os.Stat(rd.UploPath().uplodirSysPath(r.RenterFilesDir()))
	if !os.IsNotExist(err) {
		t.Fatal("Expected IsNotExist err, but got err:", err)
	}
	// Check that uplodir was deleted from disk by root path
	_, err = os.Stat(rd2.UploPath().uplodirSysPath(r.RenterFilesDir()))
	if !os.IsNotExist(err) {
		t.Fatal("Expected IsNotExist err, but got err:", err)
	}
}

// testAlertsSorted checks that the alerts returned by the /daemon/alerts
// endpoint are sorted by severity.
func testAlertsSorted(t *testing.T, tg *uplotest.TestGroup) {
	// Grab Renter
	r := tg.Renters()[0]
	dag, err := r.DaemonAlertsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(dag.Alerts) < 3 {
		t.Fatalf("renter should have at least %v alerts registered but was %v", 3, len(dag.Alerts))
	}
	sorted := sort.SliceIsSorted(dag.Alerts, func(i, j int) bool {
		return dag.Alerts[i].Severity > dag.Alerts[j].Severity
	})
	if !sorted {
		t.Log("alerts:", dag.Alerts)
		t.Fatal("alerts are not sorted by severity")
	}
}

// testDownloadAfterRenew makes sure that we can still download a file
// after the contract period has ended.
func testDownloadAfterRenew(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	renter := tg.Renters()[0]
	// Upload file, creating a piece for each host in the group
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := 100 + uplotest.Fuzz()
	_, remoteFile, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	// Mine enough blocks for the next period to start. This means the
	// contracts should be renewed and the data should still be available for
	// download.
	miner := tg.Miners()[0]
	for i := types.BlockHeight(0); i < uplotest.DefaultAllowance.Period; i++ {
		if err := miner.MineBlock(); err != nil {
			t.Fatal(err)
		}
	}
	// Download the file synchronously directly into memory.
	_, _, err = renter.DownloadByStream(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
}

// testDownloadMultipleLargeSectors downloads multiple large files (>5 Sectors)
// in parallel and makes sure that the downloads are blocking each other.
func testDownloadMultipleLargeSectors(t *testing.T, tg *uplotest.TestGroup) {
	// parallelDownloads is the number of downloads that are run in parallel.
	parallelDownloads := 10
	// fileSize is the size of the downloaded file.
	fileSize := uplotest.Fuzz()
	if build.VLONG {
		fileSize += int(50 * modules.SectorSize)
	} else {
		fileSize += int(10 * modules.SectorSize)
	}
	// set download limits and reset them after test.
	// uniqueRemoteFiles is the number of files that will be uploaded to the
	// network. Downloads will choose the remote file to download randomly.
	uniqueRemoteFiles := 5
	// Create a custom renter with a dependency and remove it again after the test
	// is done.
	renterParams := node.Renter(filepath.Join(renterTestDir(t.Name()), "renter"))
	renterParams.RenterDeps = &dependencies.DependencyPostponeWritePiecesRecovery{}
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]
	defer func() {
		if err := tg.RemoveNode(renter); err != nil {
			t.Fatal(err)
		}
	}()

	// Upload files
	dataPieces := uint64(len(tg.Hosts())) - 1
	parityPieces := uint64(1)
	remoteFiles := make([]*uplotest.RemoteFile, 0, uniqueRemoteFiles)
	for i := 0; i < uniqueRemoteFiles; i++ {
		_, remoteFile, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
		if err != nil {
			t.Fatal("Failed to upload a file for testing: ", err)
		}
		remoteFiles = append(remoteFiles, remoteFile)
	}

	// set download limits and reset them after test.
	if err := renter.RenterRateLimitPost(int64(fileSize)*2, 0); err != nil {
		t.Fatal("failed to set renter bandwidth limit", err)
	}
	defer func() {
		if err := renter.RenterRateLimitPost(0, 0); err != nil {
			t.Error("failed to reset renter bandwidth limit", err)
		}
	}()

	// Randomly download using download to file and download to stream methods.
	wg := new(sync.WaitGroup)
	for i := 0; i < parallelDownloads; i++ {
		wg.Add(1)
		go func() {
			var err error
			var rf = remoteFiles[fastrand.Intn(len(remoteFiles))]
			if fastrand.Intn(2) == 0 {
				_, _, err = renter.DownloadByStream(rf)
			} else {
				_, _, err = renter.DownloadToDisk(rf, false)
			}
			if err != nil {
				t.Error("Download failed:", err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

// testLocalRepair tests if a renter correctly repairs a file from disk
// after a host goes offline.
func testLocalRepair(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	renterNode := tg.Renters()[0]

	// Check that we have enough hosts for this test.
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}

	// Set fileSize and redundancy for upload
	fileSize := int(modules.SectorSize)
	dataPieces := uint64(2)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces

	// Upload file
	_, remoteFile, err := renterNode.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Take down hosts until enough are missing that the chunks get marked as
	// stuck after repairs.
	var hostsRemoved uint64
	for hostsRemoved = 0; float64(hostsRemoved)/float64(parityPieces) < renter.AlertUplofileLowRedundancyThreshold; hostsRemoved++ {
		if err := tg.RemoveNode(tg.Hosts()[0]); err != nil {
			t.Fatal("Failed to shutdown host", err)
		}
	}
	expectedRedundancy := float64(dataPieces+parityPieces-hostsRemoved) / float64(dataPieces)
	if err := renterNode.WaitForDecreasingRedundancy(remoteFile, expectedRedundancy); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}
	// We should still be able to download
	if _, _, err := renterNode.DownloadByStream(remoteFile); err != nil {
		t.Fatal("Failed to download file", err)
	}
	// Check that the alert for low redundancy was set.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		dag, err := renterNode.DaemonAlertsGet()
		if err != nil {
			return errors.AddContext(err, "Failed to get alerts")
		}
		f, err := renterNode.File(remoteFile)
		if err != nil {
			return err
		}
		var found bool
		for _, alert := range dag.Alerts {
			expectedCause := fmt.Sprintf("Uplofile 'home/user/%v' has a health of %v and redundancy of %v", remoteFile.UploPath().String(), f.MaxHealth, f.Redundancy)
			if alert.Msg == renter.AlertMSGUplofileLowRedundancy &&
				alert.Cause == expectedCause {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("Correct alert wasn't registered (#alerts: %v)", len(dag.Alerts))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Bring up hosts to replace the ones that went offline.
	for hostsRemoved > 0 {
		hostsRemoved--
		_, err = tg.AddNodes(node.HostTemplate)
		if err != nil {
			t.Fatal("Failed to create a new host", err)
		}
	}
	if err := renterNode.WaitForUploadHealth(remoteFile); err != nil {
		t.Fatal("File wasn't repaired", err)
	}
	// Check to see if a chunk got repaired and marked as unstuck
	err = renterNode.WaitForStuckChunksToRepair()
	if err != nil {
		t.Fatal(err)
	}
	// We should be able to download
	if _, _, err := renterNode.DownloadByStream(remoteFile); err != nil {
		t.Fatal("Failed to download file", err)
	}
}

// TestLocalRepairCorrupted tests if a renter repairs a file from disk after the
// file on disk got corrupted.
//
// The test has certain timing contraints, in particular we wait 20 seconds at
// the end to ensure that a file cannot be repaired. Because of these timing
// constraints, the test is run standalone and without t.Parallel().
func TestLocalRepairCorrupted(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for the subtests
	gp := uplotest.GroupParams{
		Hosts:   3,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterTestDir(t.Name()), gp)
	if err != nil {
		t.Fatal(err)
	}

	// Grab the first of the group's renters
	renterNode := tg.Renters()[0]

	// Set fileSize and redundancy for upload
	fileSize := int(modules.SectorSize) + uplotest.Fuzz()
	dataPieces := uint64(2)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces

	// Upload file
	localFile, remoteFile, err := renterNode.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Take down hosts until the file is unable to be repaired from remote. This
	// will check that the local repair process is working.
	var hostsRemoved uint64
	hostsToRemove := parityPieces + 1
	for hostsRemoved = 0; hostsRemoved < hostsToRemove; hostsRemoved++ {
		hostToRemove := tg.Hosts()[0]
		err := tg.RemoveNode(hostToRemove)
		if err != nil {
			t.Fatal("Failed to shutdown host", err)
		}
	}
	expectedRedundancy := float64(dataPieces-1) / float64(dataPieces)
	if err := renterNode.WaitForDecreasingRedundancy(remoteFile, expectedRedundancy); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}
	// Download should fail, there are not enough hosts online.
	if _, _, err := renterNode.DownloadByStream(remoteFile); err == nil {
		t.Fatal("download is succeeding even though there are not enough hosts to carry the file.")
		t.Log(err)
	}
	// Bring a host back up and see that the file completes a local repair.
	_, err = tg.AddNodes(node.HostTemplate)
	if err != nil {
		t.Fatal(err)
	}
	hostsRemoved--
	err = renterNode.WaitForFileAvailable(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := renterNode.DownloadByStream(remoteFile); err != nil {
		t.Fatal(err)
	}

	// Corrupt the local file, so that repairs will cause problems.
	b, err := localFile.Data()
	if err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(localFile.Path(), fastrand.Bytes(len(b)), 0600); err != nil {
		t.Fatal(err)
	}

	// Bring more hosts online, check that repair will failover to remote
	// repair.
	for hostsRemoved > 0 {
		hostsRemoved--
		_, err = tg.AddNodes(node.HostTemplate)
		if err != nil {
			t.Fatal("Failed to create a new host", err)
		}
	}
	// File should get back to full health.
	err = renterNode.WaitForUploadHealth(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	// Verify that a download works.
	if _, _, err := renterNode.DownloadByStream(remoteFile); err != nil {
		t.Fatal(err)
	}

	// Bring hosts offline again. Now that the local file is corrupted,
	// repairing should be impossible.
	for hostsRemoved = 0; hostsRemoved < hostsToRemove; hostsRemoved++ {
		if err := tg.RemoveNode(tg.Hosts()[0]); err != nil {
			t.Fatal("Failed to shutdown host", err)
		}
	}
	// Wait for the redundancy to drop.
	if err := renterNode.WaitForDecreasingRedundancy(remoteFile, expectedRedundancy); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}
	// Bring a host back online so that the file can be repaired to be
	// available. Because the local file is corrupt, the repair should be
	// blocked.
	_, err = tg.AddNodes(node.HostTemplate)
	if err != nil {
		t.Fatal(err)
	}
	hostsRemoved--

	// Give the renter some time to complete the repair. I'm not really sure if
	// there's a better way than waiting to ensure that the repair loop has had
	// a couple of iterations to attempt the repair.
	time.Sleep(time.Second * 20)
	file, err := renterNode.File(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	if file.Available {
		t.Fatal("file should not be available when its only source of repair data is corrupt")
	}
}

// testPriceTablesUpdated verfies the workers' price tables are updated and stay
// recent with the host
func testPriceTablesUpdated(t *testing.T, tg *uplotest.TestGroup) {
	r := tg.Renters()[0]

	// Get the worker status
	rwg, err := r.RenterWorkersGet()
	if err != nil {
		t.Fatal(err)
	}

	// Get a random worker
	var host types.UploPublicKey
	for _, worker := range rwg.Workers {
		host = worker.HostPubKey
		break
	}

	// Wait until that worker has been able to update its price table, when that
	// is the case we save its current update and expiry time.
	var ut, et time.Time
	err = build.Retry(100, 100*time.Millisecond, func() error {
		rwg, err := r.RenterWorkersGet()
		if err != nil {
			return err
		}

		var ws *modules.WorkerStatus
		for i := range rwg.Workers {
			worker := rwg.Workers[i]
			if worker.HostPubKey.Equals(host) {
				ws = &worker
				break
			}
		}
		if ws == nil {
			return errors.New("worker not found")
		}

		if !ws.PriceTableStatus.Active {
			return errors.New("worker has no valid price table")
		}

		ut = ws.PriceTableStatus.UpdateTime
		et = ws.PriceTableStatus.ExpiryTime
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait until after the price table is set to update, note we don't gain
	// anything by waiting for this inside the build.Retry as we know when it
	// won't trigger before the update time.
	time.Sleep(time.Until(ut))

	// Verify in a retry that the price table's updateTime and expiryTime have
	// been set to new dates in the future, indicating a successful price table
	// update.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		rwg, err := r.RenterWorkersGet()
		if err != nil {
			return err
		}

		var ws *modules.WorkerStatus
		for i := range rwg.Workers {
			worker := rwg.Workers[i]
			if worker.HostPubKey.Equals(host) {
				ws = &worker
				break
			}
		}
		if ws == nil {
			return errors.New("worker not found")
		}

		if !(ws.PriceTableStatus.UpdateTime.After(ut) && ws.PriceTableStatus.ExpiryTime.After(et)) {
			return errors.New("updatedTime and expiryTime have not been updated yet, indicating the price table has not been renewed")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// testRemoteRepair tests if a renter correctly repairs a file by
// downloading it after a host goes offline.
//
// This test was extended to also support testing the download cooldowns.
func testRemoteRepair(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	// Check that we have enough hosts for this test.
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}

	// Choose a filesize for the upload. To hit a wide range of cases,
	// uplotest.Fuzz is used.
	fuzz := uplotest.Fuzz()
	fileSize := int(modules.SectorSize) + fuzz
	// One out of three times, add an extra sector.
	if uplotest.Fuzz() == 0 {
		fileSize += int(modules.SectorSize)
	}
	// One out of three times, add a random amount of extra data.
	if uplotest.Fuzz() == 0 {
		fileSize += fastrand.Intn(int(modules.SectorSize))
	}
	t.Log("testRemoteRepair fileSize choice:", fileSize)

	// Set fileSize and redundancy for upload
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces

	// Upload file
	localFile, remoteFile, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	// Get the file info of the fully uploaded file. Tha way we can compare the
	// redundancies later.
	_, err = r.File(remoteFile)
	if err != nil {
		t.Fatal("failed to get file info", err)
	}

	// Delete the file locally.
	if err := localFile.Delete(); err != nil {
		t.Fatal("failed to delete local file", err)
	}

	// Take down all of the parity hosts and check if redundancy decreases.
	for i := uint64(0); i < parityPieces; i++ {
		if err := tg.RemoveNode(tg.Hosts()[0]); err != nil {
			t.Fatal("Failed to shutdown host", err)
		}
	}
	expectedRedundancy := float64(dataPieces+parityPieces-1) / float64(dataPieces)
	if err := r.WaitForDecreasingRedundancy(remoteFile, expectedRedundancy); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}
	// We should still be able to download
	if _, _, err := r.DownloadByStream(remoteFile); err != nil {
		t.Error("Failed to download file", err)
	}
	// Bring up new parity hosts and check if redundancy increments again.
	_, err = tg.AddNodeN(node.HostTemplate, int(parityPieces))
	if err != nil {
		t.Fatal("Failed to create a new host", err)
	}
	// Wait for the file to be healthy.
	if err := r.WaitForUploadHealth(remoteFile); err != nil {
		t.Fatal("File wasn't repaired", err)
	}
	// Check to see if a chunk got repaired and marked as unstuck
	err = r.WaitForStuckChunksToRepair()
	if err != nil {
		t.Fatal(err)
	}
	// We should be able to download
	_, _, err = r.DownloadByStream(remoteFile)
	if err != nil {
		t.Error("Failed to download file", err)
	}

	// The worker shouldn't be on a cooldown since the last download was successful
	// and cleared the consecutive failures.
	err = build.Retry(500, 100*time.Millisecond, func() error {
		rwg, err := r.RenterWorkersGet()
		if err != nil {
			t.Fatal(err)
		}
		if rwg.TotalDownloadCoolDown != 0 {
			return errors.New("worker still on cooldown")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

// testSingleFileGet is a subtest that uses an existing TestGroup to test if
// using the single file API endpoint works
func testSingleFileGet(t *testing.T, tg *uplotest.TestGroup) {
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}
	// Grab the first of the group's renters
	renter := tg.Renters()[0]
	// Upload file, creating a piece for each host in the group
	dataPieces := uint64(2)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := 100 + uplotest.Fuzz()
	_, _, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}

	// Get all files from Renter
	files, err := renter.Files(false)
	if err != nil {
		t.Fatal("Failed to get renter files: ", err)
	}

	// Loop over files and compare against single file endpoint
	for i := range files {
		// Get Single File
		rf, err := renter.RenterFileGet(files[i].UploPath)
		if err != nil {
			t.Fatal(err)
		}
		// Compare File result and Files Results, check the fields which are
		// expected to be stable between accesses of the file.
		if files[i].Available != rf.File.Available {
			t.Error("mismatch")
		}
		if files[i].CipherType != rf.File.CipherType {
			t.Error("mismatch")
		}
		if files[i].CreateTime != rf.File.CreateTime {
			t.Error("mismatch")
		}
		if files[i].Filesize != rf.File.Filesize {
			t.Error("mismatch")
		}
		if files[i].LocalPath != rf.File.LocalPath {
			t.Error("mismatch")
		}
		if files[i].FileMode != rf.File.FileMode {
			t.Error("mismatch")
		}
		if files[i].NumStuckChunks != rf.File.NumStuckChunks {
			t.Error("mismatch")
		}
		if files[i].OnDisk != rf.File.OnDisk {
			t.Error("mismatch")
		}
		if files[i].Recoverable != rf.File.Recoverable {
			t.Error("mismatch")
		}
		if files[i].Renewing != rf.File.Renewing {
			t.Error("mismatch")
		}
		if files[i].UploPath != rf.File.UploPath {
			t.Error("mismatch")
		}
		if files[i].Stuck != rf.File.Stuck {
			t.Error("mismatch")
		}
	}
}

// testCancelAsyncDownload tests that cancelling an async download aborts the
// download and sets the correct fields.
func testCancelAsyncDownload(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	renter := tg.Renters()[0]
	// Upload file, creating a piece for each host in the group
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := 10 * modules.SectorSize
	_, remoteFile, err := renter.UploadNewFileBlocking(int(fileSize), dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	// Set a ratelimit that only allows for downloading a sector every second.
	if err := renter.RenterRateLimitPost(int64(modules.SectorSize), 0); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := renter.RenterRateLimitPost(0, 0); err != nil {
			t.Fatal(err)
		}
	}()
	// Download the file asynchronously.
	dst := filepath.Join(renter.FilesDir().Path(), "canceled_download.dat")
	cancelID, err := renter.RenterDownloadGet(remoteFile.UploPath(), dst, 0, fileSize, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	// Sometimes wait a second to not always cancel the download right
	// away.
	time.Sleep(time.Second * time.Duration(fastrand.Intn(2)))
	// Cancel the download.
	if err := renter.RenterCancelDownloadPost(cancelID); err != nil {
		t.Fatal(err)
	}
	// Get the download info.
	rdg, err := renter.RenterDownloadsGet()
	if err != nil {
		t.Fatal(err)
	}
	var di *api.DownloadInfo
	for i := range rdg.Downloads {
		d := rdg.Downloads[i]
		if remoteFile.UploPath() == d.UploPath && dst == d.Destination {
			di = &d
			break
		}
	}
	if di == nil {
		t.Fatal("couldn't find download")
	}
	// Make sure the download was cancelled.
	if !di.Completed {
		t.Fatal("download is not marked as completed")
	}
	if di.Received >= fileSize {
		t.Fatal("the download finished successfully")
	}
	if di.Error != modules.ErrDownloadCancelled.Error() {
		t.Fatal("error message doesn't match ErrDownloadCancelled")
	}
}

// testUploadDownload is a subtest that uses an existing TestGroup to test if
// uploading and downloading a file works
func testUploadDownload(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	renter := tg.Renters()[0]
	// Upload file, creating a piece for each host in the group
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := fastrand.Intn(2*int(modules.SectorSize)) + uplotest.Fuzz() + 2 // between 1 and 2*SectorSize + 3 bytes
	localFile, remoteFile, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	// Download the file synchronously directly into memory
	_, _, err = renter.DownloadByStream(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	// Download the file synchronously to a file on disk
	_, _, err = renter.DownloadToDisk(remoteFile, false)
	if err != nil {
		t.Fatal(err)
	}
	// Download the file asynchronously and wait for the download to finish.
	_, localFile, err = renter.DownloadToDisk(remoteFile, true)
	if err != nil {
		t.Error(err)
	}
	if err := renter.WaitForDownload(localFile, remoteFile); err != nil {
		t.Error(err)
	}
	// Stream the file.
	_, err = renter.Stream(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	// Stream the file partially a few times. At least 1 byte is streamed.
	for i := 0; i < 5; i++ {
		from := fastrand.Intn(fileSize - 1)             // [0..fileSize-2]
		to := from + 1 + fastrand.Intn(fileSize-from-1) // [from+1..fileSize-1]
		_, err = renter.StreamPartial(remoteFile, localFile, uint64(from), uint64(to))
		if err != nil {
			t.Fatal(err)
		}
	}
	// Download the file again with root set.
	rootPath, err := remoteFile.UploPath().Rebase(modules.RootUploPath(), modules.UserFolder)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(renter.FilesDir().Path(), "root.dat")
	_, err = renter.RenterDownloadGet(rootPath, dst, 0, uint64(fileSize), false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	dst = filepath.Join(renter.FilesDir().Path(), "root2.dat")
	_, err = renter.RenterDownloadFullGet(rootPath, dst, false, true)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = renter.RenterDownloadHTTPResponseGet(rootPath, 0, uint64(fileSize), true, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = renter.RenterStreamGet(rootPath, false, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = renter.RenterStreamPartialGet(rootPath, 0, uint64(fileSize), false, true)
	if err != nil {
		t.Fatal(err)
	}
}

// testUploadWithAndWithoutForceParameter is a subtest that uses an existing TestGroup to test if
// uploading an existing file is successful when setting 'force' to 'true' and 'force' set to 'false'
func testUploadWithAndWithoutForceParameter(t *testing.T, tg *uplotest.TestGroup) {
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}
	// Grab the first of the group's renters
	renter := tg.Renters()[0]

	// Upload a file, then try to overwrite the file with the force flag set.
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := 100 + uplotest.Fuzz()
	localFile, _, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	_, err = renter.UploadBlocking(localFile, dataPieces, parityPieces, true)
	if err != nil {
		t.Fatal("Failed to force overwrite a file when specifying 'force=true': ", err)
	}

	// Upload file, then try to overwrite the file without the force flag set.
	dataPieces = uint64(1)
	parityPieces = uint64(len(tg.Hosts())) - dataPieces
	fileSize = 100 + uplotest.Fuzz()
	localFile, _, err = renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	_, err = renter.UploadBlocking(localFile, dataPieces, parityPieces, false)
	if err == nil {
		t.Fatal("File overwritten without specifying 'force=true'")
	}

	// Try to upload a file with the force flag set.
	dataPieces = uint64(1)
	parityPieces = uint64(len(tg.Hosts())) - dataPieces
	fileSize = 100 + uplotest.Fuzz()
	localFile, _, err = renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, true)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	_, err = renter.UploadBlocking(localFile, dataPieces, parityPieces, false)
	if err == nil {
		t.Fatal("File overwritten without specifying 'force=true'")
	}

	// Try to upload a file with the force flag set.
	dataPieces = uint64(1)
	parityPieces = uint64(len(tg.Hosts())) - dataPieces
	fileSize = 100 + uplotest.Fuzz()
	localFile, _, err = renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, true)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	_, err = renter.UploadBlocking(localFile, dataPieces, parityPieces, true)
	if err != nil {
		t.Fatal("Failed to force overwrite a file when specifying 'force=true': ", err)
	}
}

// TestRenterInterrupt executes a number of subtests using the same TestGroup to
// save time on initialization
func TestRenterInterrupt(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for the subtests
	groupParams := uplotest.GroupParams{
		Hosts:  5,
		Miners: 1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify sub tests
	subTests := []uplotest.SubTest{
		{Name: "TestContractInterruptedSaveToDiskAfterDeletion", Test: testContractInterruptedSaveToDiskAfterDeletion},
		{Name: "TestDownloadInterruptedAfterSendingRevision", Test: testDownloadInterruptedAfterSendingRevision},
		{Name: "TestDownloadInterruptedBeforeSendingRevision", Test: testDownloadInterruptedBeforeSendingRevision},
		{Name: "TestUploadInterruptedAfterSendingRevision", Test: testUploadInterruptedAfterSendingRevision},
		{Name: "TestUploadInterruptedBeforeSendingRevision", Test: testUploadInterruptedBeforeSendingRevision},
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// testContractInterruptedSaveToDiskAfterDeletion runs testDownloadInterrupted with
// a dependency that interrupts the download after sending the signed revision
// to the host.
func testContractInterruptedSaveToDiskAfterDeletion(t *testing.T, tg *uplotest.TestGroup) {
	testContractInterrupted(t, tg, dependencies.NewDependencyInterruptContractSaveToDiskAfterDeletion())
}

// testDownloadInterruptedAfterSendingRevision runs testDownloadInterrupted with
// a dependency that interrupts the download after sending the signed revision
// to the host.
func testDownloadInterruptedAfterSendingRevision(t *testing.T, tg *uplotest.TestGroup) {
	testDownloadInterrupted(t, tg, dependencies.NewDependencyInterruptDownloadAfterSendingRevision())
}

// testDownloadInterruptedBeforeSendingRevision runs testDownloadInterrupted
// with a dependency that interrupts the download before sending the signed
// revision to the host.
func testDownloadInterruptedBeforeSendingRevision(t *testing.T, tg *uplotest.TestGroup) {
	testDownloadInterrupted(t, tg, dependencies.NewDependencyInterruptDownloadBeforeSendingRevision())
}

// testUploadInterruptedAfterSendingRevision runs testUploadInterrupted with a
// dependency that interrupts the upload after sending the signed revision to
// the host.
func testUploadInterruptedAfterSendingRevision(t *testing.T, tg *uplotest.TestGroup) {
	testUploadInterrupted(t, tg, dependencies.NewDependencyInterruptUploadAfterSendingRevision())
}

// testUploadInterruptedBeforeSendingRevision runs testUploadInterrupted with a
// dependency that interrupts the upload before sending the signed revision to
// the host.
func testUploadInterruptedBeforeSendingRevision(t *testing.T, tg *uplotest.TestGroup) {
	testUploadInterrupted(t, tg, dependencies.NewDependencyInterruptUploadBeforeSendingRevision())
}

// testContractInterrupted interrupts a download using the provided dependencies.
func testContractInterrupted(t *testing.T, tg *uplotest.TestGroup, deps *dependencies.DependencyInterruptOnceOnKeyword) {
	// Add Renter
	testDir := renterTestDir(t.Name())
	renterTemplate := node.Renter(testDir + "/renter")
	renterTemplate.ContractorDeps = deps
	renterTemplate.Allowance = uplotest.DefaultAllowance
	renterTemplate.Allowance.Period = 100
	renterTemplate.Allowance.RenewWindow = 75
	nodes, err := tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]
	numHosts := len(tg.Hosts())

	// Call fail on the dependency every 10 ms.
	cancel := make(chan struct{})
	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		for {
			// Cause the contract renewal to fail
			deps.Fail()
			select {
			case <-cancel:
				wg.Done()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	// Renew contracts.
	if err = uplotest.RenewContractsByRenewWindow(renter, tg); err != nil {
		t.Fatal(err)
	}

	// Disrupt statement should prevent contracts from being renewed properly.
	// This means that both old and new contracts will be staticContracts which
	// are exported through the API via RenterContracts.Contracts
	err = build.Retry(50, 100*time.Millisecond, func() error {
		rc, err := renter.RenterContractsGet()
		if err != nil {
			return err
		}
		// Need to use old contract endpoint field as it is pulling from the
		// Contractor's staticContracts field which is where the bug was seen
		if len(rc.Contracts) != numHosts*2 {
			return fmt.Errorf("Incorrect number of staticContracts: have %v expected %v", len(rc.Contracts), numHosts*2)
		}
		return nil
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, false, true)
		t.Fatal(err)
	}

	// By mining blocks to trigger threadContractMaintenance,
	// managedCheckForDuplicates should move renewed contracts from
	// staticContracts to oldContracts even though disrupt statement is still
	// interrupting renew code.
	m := tg.Miners()[0]
	if err = m.MineBlock(); err != nil {
		t.Fatal(err)
	}
	if err = tg.Sync(); err != nil {
		t.Fatal(err)
	}
	err = build.Retry(70, 100*time.Millisecond, func() error {
		// Check for older compatibility fields.
		// If we don't check this fields we are not checking the right conditions.
		rc, err := renter.RenterExpiredContractsGet()
		if err != nil {
			return err
		}
		if len(rc.InactiveContracts) != 0 {
			return fmt.Errorf("Incorrect number of inactive contracts: have %v expected %v", len(rc.InactiveContracts), 0)
		}
		if len(rc.ActiveContracts) != numHosts {
			return fmt.Errorf("Incorrect number of active contracts: have %v expected %v", len(rc.ActiveContracts), numHosts)
		}
		if len(rc.Contracts) != numHosts {
			return fmt.Errorf("Incorrect number of staticContracts: have %v expected %v", len(rc.Contracts), numHosts)
		}
		if len(rc.ExpiredContracts) != numHosts {
			return fmt.Errorf("Incorrect number of expired contracts: have %v expected %v", len(rc.ExpiredContracts), numHosts)
		}

		if err = m.MineBlock(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, false, true)
		t.Fatal(err)
	}

	// Stop calling fail on the dependency.
	close(cancel)
	wg.Wait()
	deps.Disable()
}

// testDownloadInterrupted interrupts a download using the provided dependencies.
func testDownloadInterrupted(t *testing.T, tg *uplotest.TestGroup, deps *dependencies.DependencyInterruptOnceOnKeyword) {
	// Add Renter
	testDir := renterTestDir(t.Name())
	renterTemplate := node.Renter(testDir + "/renter")
	renterTemplate.ContractSetDeps = deps
	nodes, err := tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// Set the bandwidth limit to 1 chunk per second.
	renter := nodes[0]
	ct := crypto.TypeDefaultRenter
	dataPieces := uint64(len(tg.Hosts())) - 1
	parityPieces := uint64(1)
	chunkSize := uplotest.ChunkSize(dataPieces, ct)
	_, remoteFile, err := renter.UploadNewFileBlocking(int(chunkSize), dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := renter.RenterRateLimitPost(int64(chunkSize), int64(chunkSize)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := renter.RenterRateLimitPost(0, 0); err != nil {
			t.Fatal(err)
		}
	}()

	// Call fail on the dependency every 10 ms.
	cancel := make(chan struct{})
	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		for {
			// Cause the next download to fail.
			deps.Fail()
			select {
			case <-cancel:
				wg.Done()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	// Try downloading the file 5 times.
	for i := 0; i < 5; i++ {
		if _, _, err := renter.DownloadByStream(remoteFile); err == nil {
			t.Fatal("Download shouldn't succeed since it was interrupted")
		}
	}
	// Stop calling fail on the dependency.
	close(cancel)
	wg.Wait()
	deps.Disable()
	// Download the file once more successfully
	if _, _, err := renter.DownloadByStream(remoteFile); err != nil {
		t.Fatal("Failed to download the file", err)
	}
}

// testUploadInterrupted let's the upload fail using the provided dependencies
// and makes sure that this doesn't corrupt the contract.
func testUploadInterrupted(t *testing.T, tg *uplotest.TestGroup, deps *dependencies.DependencyInterruptOnceOnKeyword) {
	// Add Renter
	testDir := renterTestDir(t.Name())
	renterTemplate := node.Renter(testDir + "/renter")
	renterTemplate.ContractSetDeps = deps
	nodes, err := tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// Set the bandwidth limit to 1 chunk per second.
	ct := crypto.TypeDefaultRenter
	renter := nodes[0]
	dataPieces := uint64(len(tg.Hosts())) - 1
	parityPieces := uint64(1)
	chunkSize := uplotest.ChunkSize(dataPieces, ct)
	if err := renter.RenterRateLimitPost(int64(chunkSize), int64(chunkSize)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := renter.RenterRateLimitPost(0, 0); err != nil {
			t.Fatal(err)
		}
	}()

	// Call fail on the dependency every two seconds to allow some uploads to
	// finish.
	cancel := make(chan struct{})
	done := make(chan struct{})
	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer close(done)
		// Loop until cancel was closed or we reach 5 iterations. Otherwise we
		// might end up blocking the upload for too long.
		for i := 0; i < 10; i++ {
			// Cause the next upload to fail.
			deps.Fail()
			select {
			case <-cancel:
				wg.Done()
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
		wg.Done()
	}()

	// Upload a file that's 1 chunk large.
	_, remoteFile, err := renter.UploadNewFileBlocking(int(chunkSize), dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	// Make sure that the upload does not finish before the interrupting go
	// routine is finished
	select {
	case <-done:
	default:
		t.Fatal("Upload finished before interrupt signal is done")
	}
	// Stop calling fail on the dependency.
	close(cancel)
	wg.Wait()
	deps.Disable()
	// Download the file.
	if _, _, err := renter.DownloadByStream(remoteFile); err != nil {
		t.Fatal("Failed to download the file", err)
	}
}

// TestRenterAddNodes runs a subset of tests that require adding their own renter
func TestRenterAddNodes(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for testing
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify subtests to run
	subTests := []uplotest.SubTest{
		{Name: "TestRedundancyReporting", Test: testRedundancyReporting}, // Put first because it pulls the original tg renter
		{Name: "TestUploadReady", Test: testUploadReady},
		{Name: "TestOverspendAllowance", Test: testOverspendAllowance},
		{Name: "TestRenterAllowanceCancel", Test: testRenterAllowanceCancel},
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// TestRenterAddNodes2 runs a subset of tests that require adding their own
// renter. TestRenterPostCancelAllowance was split into its own test to improve
// reliability - it was flaking previously.
func TestRenterAddNodes2(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for testing
	groupParams := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	groupDir := renterTestDir(t.Name())

	// Specify subtests to run
	subTests := []uplotest.SubTest{
		{Name: "TestRenterPostCancelAllowance", Test: testRenterPostCancelAllowance},
	}

	// Run tests
	if err := uplotest.RunSubTests(t, groupParams, groupDir, subTests); err != nil {
		t.Fatal(err)
	}
}

// testRedundancyReporting verifies that redundancy reporting is accurate if
// contracts become offline.
func testRedundancyReporting(t *testing.T, tg *uplotest.TestGroup) {
	// Upload a file.
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts()) - 1)

	renter := tg.Renters()[0]
	_, rf, err := renter.UploadNewFileBlocking(100, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Stop a host.
	host := tg.Hosts()[0]
	if err := tg.StopNode(host); err != nil {
		t.Fatal(err)
	}

	// Mine a block to trigger contract maintenance.
	miner := tg.Miners()[0]
	if err := miner.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Redundancy should decrease.
	expectedRedundancy := float64(dataPieces+parityPieces-1) / float64(dataPieces)
	if err := renter.WaitForDecreasingRedundancy(rf, expectedRedundancy); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}

	// Restart the host.
	if err := tg.StartNode(host); err != nil {
		t.Fatal(err)
	}

	// Wait until the host shows up as active again.
	pk, err := host.HostPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(60, time.Second, func() error {
		hdag, err := renter.HostDbActiveGet()
		if err != nil {
			return err
		}
		for _, h := range hdag.Hosts {
			if reflect.DeepEqual(h.PublicKey, pk) {
				return nil
			}
		}
		// If host is not active, announce it again and mine a block.
		if err := host.HostAnnouncePost(); err != nil {
			return err
		}
		miner := tg.Miners()[0]
		if err := miner.MineBlock(); err != nil {
			return err
		}
		if err := tg.Sync(); err != nil {
			return err
		}
		hg, err := host.HostGet()
		if err != nil {
			return err
		}
		return fmt.Errorf("host with address %v not active", hg.InternalSettings.NetAddress)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := miner.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// File should be repaired.
	if err := renter.WaitForUploadHealth(rf); err != nil {
		t.Fatal("File is not being repaired", err)
	}
}

// TestRenewFailing checks if a contract gets marked as !goodForRenew after
// failing multiple times in a row.
func TestRenewFailing(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for testing
	groupParams := uplotest.GroupParams{
		Hosts:  4,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add a host that can't renew.
	hostParams := node.HostTemplate
	hostParams.HostDeps = &dependencies.DependencyRenewFail{}
	nodes, err := tg.AddNodes(hostParams)
	if err != nil {
		t.Fatal(err)
	}
	failHost := nodes[0]
	lockedHostPK, err := failHost.HostPublicKey()
	if err != nil {
		t.Fatal(err)
	}

	// Add a regular renter.
	nodes, err = tg.AddNodes(node.RenterTemplate)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// All the contracts of the renter should be goodForRenew. So there should
	// be no inactive contracts, only active contracts
	err = uplotest.CheckExpectedNumberOfContracts(renter, len(tg.Hosts()), 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Create a map of the hosts in the group.
	hostMap := make(map[string]*uplotest.TestNode)
	for _, host := range tg.Hosts() {
		pk, err := host.HostPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		hostMap[pk.String()] = host
	}

	// Get the contracts
	rcg, err := renter.RenterAllContractsGet()
	if err != nil {
		t.Fatal(err)
	}

	// Wait until the contract is supposed to be renewed.
	cg, err := renter.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	rg, err := renter.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	miner := tg.Miners()[0]
	blockHeight := cg.Height
	renewWindow := rg.Settings.Allowance.RenewWindow
	for blockHeight+renewWindow+1 < rcg.ActiveContracts[0].EndHeight {
		if err := miner.MineBlock(); err != nil {
			t.Fatal(err)
		}
		blockHeight++
	}

	// There should be no inactive contracts, only active contracts since we are
	// 1 block before the renewWindow/s second half. Do this in a retry to give
	// the contractor some time to catch up.
	err = build.Retry(int(renewWindow/2), time.Second, func() error {
		return uplotest.CheckExpectedNumberOfContracts(renter, len(tg.Hosts()), 0, 0, 0, 0, 0)
	})
	if err != nil {
		t.Fatal(err)
	}

	// mine enough blocks to reach the second half of the renew window.
	for ; blockHeight+rg.Settings.Allowance.RenewWindow/2+1 < rcg.ActiveContracts[0].EndHeight; blockHeight++ {
		if err := miner.MineBlock(); err != nil {
			t.Fatal(err)
		}
		blockHeight++
	}

	// We should be within the second half of the renew window now. We keep
	// mining blocks until the host with the locked wallet has been replaced.
	// This should happen before we reach the endHeight of the contracts. This
	// means we should have number of hosts - 1 active contracts, number of
	// hosts - 1 renewed contracts, and one of the disabled contract which will
	// be the host that has the locked wallet
	err = build.Retry(int(rcg.ActiveContracts[0].EndHeight-blockHeight), time.Second, func() error {
		if err := miner.MineBlock(); err != nil {
			return err
		}
		// contract should be !goodForRenew now.
		// Assert number of contracts.
		err = uplotest.CheckExpectedNumberOfContracts(renter, len(tg.Hosts())-1, 0, 0, 1, len(tg.Hosts())-1, 0)
		if err != nil {
			return err
		}
		// If the host is the host in the disabled contract, then the test has
		// passed.
		rc, err := renter.RenterDisabledContractsGet()
		if err != nil {
			return err
		}
		if !rc.DisabledContracts[0].HostPublicKey.Equals(lockedHostPK) {
			return errors.New("Disbled contract host not the locked host")
		}
		return nil
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}
}

// testRenterAllowanceCancel tests that setting an empty allowance causes
// uploads, downloads, and renewals to cease as well as tests that resetting the
// allowance after the allowance was cancelled will trigger the correct contract
// formation.
func testRenterAllowanceCancel(t *testing.T, tg *uplotest.TestGroup) {
	renterParams := node.Renter(filepath.Join(renterTestDir(t.Name()), "renter"))
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Grab the number of hosts
	numHosts := len(tg.Hosts())

	// Test Resetting allowance
	// Cancel the allowance
	if err := renter.RenterAllowanceCancelPost(); err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Mark sure contracts have been updated
	err = build.Retry(200, 100*time.Millisecond, func() error {
		return uplotest.CheckExpectedNumberOfContracts(renter, 0, 0, 0, numHosts, 0, 0)
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Set the allowance again.
	if err := renter.RenterPostAllowance(uplotest.DefaultAllowance); err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Mine a block to start the threadedContractMaintenance.
	m := tg.Miners()[0]
	if err := m.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Give it some time to mark the contracts as goodForUpload and
	// goodForRenew again.
	tries := 0
	err = build.Retry(100, 100*time.Millisecond, func() error {
		if tries%20 == 0 {
			err := m.MineBlock()
			if err != nil {
				return err
			}
		}
		return uplotest.CheckExpectedNumberOfContracts(renter, numHosts, 0, 0, 0, 0, 0)
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Test Canceling allowance
	// Upload a file.
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts()) - 1)
	_, rf, err := renter.UploadNewFileBlocking(100, dataPieces, parityPieces, false)
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Cancel the allowance
	if err := renter.RenterAllowanceCancelPost(); err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Give it some time to mark the contracts as !goodForUpload and
	// !goodForRenew.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		return uplotest.CheckExpectedNumberOfContracts(renter, 0, 0, 0, numHosts, 0, 0)
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Try downloading the file; should succeed.
	if _, _, err := renter.DownloadByStream(rf); err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal("downloading file failed", err)
	}

	// Wait for a few seconds to make sure that the upload heap is rebuilt.
	// The rebuilt interval is 3 seconds. Sleep for 5 to be safe.
	time.Sleep(5 * time.Second)

	// Try to upload a file after the allowance was cancelled. Should succeed.
	_, rf2, err := renter.UploadNewFile(100, dataPieces, parityPieces, false)
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Give it some time to upload.
	time.Sleep(time.Second)

	// Redundancy should still be 0.
	renterFiles, err := renter.RenterFilesGet(false)
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal("Failed to get files")
	}
	if len(renterFiles.Files) != 2 {
		t.Fatal("There should be exactly 2 tracked files")
	}
	fileInfo, err := renter.File(rf2)
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}
	if fileInfo.UploadProgress > 0 || fileInfo.UploadedBytes > 0 || fileInfo.Redundancy > 0 {
		t.Fatal("Uploading a file after canceling the allowance should fail")
	}

	// Mine enough blocks for the period to pass and the contracts to expire.
	for i := types.BlockHeight(0); i < uplotest.DefaultAllowance.Period; i++ {
		if err := m.MineBlock(); err != nil {
			t.Fatal(err)
		}
	}

	// All contracts should be expired.
	tries = 0
	err = build.Retry(100, 100*time.Millisecond, func() error {
		if tries%20 == 0 {
			err := m.MineBlock()
			if err != nil {
				return err
			}
		}
		return uplotest.CheckExpectedNumberOfContracts(renter, 0, 0, 0, 0, numHosts, 0)
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Try downloading the file; should fail.
	if _, _, err := renter.DownloadByStream(rf2); err == nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal("downloading file succeeded even though it shouldnt", err)
	}

	// The uploaded files should have 0x redundancy now.
	err = build.Retry(200, 100*time.Millisecond, func() error {
		rf, err := renter.RenterFilesGet(false)
		if err != nil {
			return errors.New("Failed to get files")
		}
		if len(rf.Files) != 2 || rf.Files[0].Redundancy != 0 || rf.Files[1].Redundancy != 0 {
			return errors.New("file redundancy should be 0 now")
		}
		return nil
	})
	if err != nil {
		renter.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}
}

// testUploadReady tests that the RenterUploadReady endpoint returns as expected
func testUploadReady(t *testing.T, tg *uplotest.TestGroup) {
	// Add a renter that skips setting the allowance
	renterParams := node.Renter(filepath.Join(renterTestDir(t.Name()), "renter"))
	renterParams.SkipSetAllowance = true
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Renter should not be ready for upload
	rur, err := renter.RenterUploadReadyDefaultGet()
	if err != nil {
		t.Fatal(err)
	}
	if rur.Ready {
		t.Fatal("Renter should not be ready for upload")
	}

	// Check submitting only 1 variable set
	_, err = renter.RenterUploadReadyGet(1, 0)
	if err == nil {
		t.Fatal("Err should have been returned for only setting datapieces")
	}
	_, err = renter.RenterUploadReadyGet(0, 1)
	if err == nil {
		t.Fatal("Err should have been returned for only setting paritypieces")
	}

	// Set the allowance
	if err := renter.RenterPostAllowance(uplotest.DefaultAllowance); err != nil {
		t.Fatal(err)
	}

	// Mine a block to start the threadedContractMaintenance.
	if err := tg.Miners()[0].MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Confirm there are enough contracts
	err = build.Retry(100, 100*time.Millisecond, func() error {
		rc, err := renter.RenterContractsGet()
		if err != nil {
			return err
		}
		if len(rc.ActiveContracts) != len(tg.Hosts()) {
			return fmt.Errorf("Not enough contracts, have %v expected %v", len(rc.ActiveContracts), len(tg.Hosts()))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Renter should be ready for upload
	rur, err = renter.RenterUploadReadyDefaultGet()
	if err != nil {
		t.Fatal(err)
	}
	if !rur.Ready {
		t.Fatal("Renter is not ready for upload", rur)
	}

	// Renter should not be viewed as ready if data and parity pieces are larger
	// than defaults
	rur, err = renter.RenterUploadReadyGet(15, 35)
	if err != nil {
		t.Fatal(err)
	}
	if rur.Ready {
		t.Fatal("Expected renter to not be ready for upload", rur)
	}
}

// testOverspendAllowance tests that setting a small allowance and trying to
// form contracts will not result in overspending the allowance
func testOverspendAllowance(t *testing.T, tg *uplotest.TestGroup) {
	renterParams := node.Renter(filepath.Join(renterTestDir(t.Name()), "renter"))
	renterParams.SkipSetAllowance = true
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Set the allowance with only 4SC
	allowance := uplotest.DefaultAllowance
	allowance.Funds = types.UplocoinPrecision.Mul64(4)
	if err := renter.RenterPostAllowance(allowance); err != nil {
		t.Fatal(err)
	}

	// Mine a block to start the threadedContractMaintenance.
	if err := tg.Miners()[0].MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Try and form multiple sets of contracts by canceling any contracts that
	// form
	count := 0
	times := 0
	err = build.Retry(200, 100*time.Millisecond, func() error {
		// Mine Blocks every 5 iterations to ensure that contracts are
		// continually trying to be created
		count++
		if count%5 == 0 {
			if err := tg.Miners()[0].MineBlock(); err != nil {
				return err
			}
		}
		// Get contracts
		rc, err := renter.RenterContractsGet()
		if err != nil {
			return err
		}
		// Check if any contracts have formed
		if len(rc.ActiveContracts) == 0 {
			times++
			// Return if there have been 20 consecutive iterations with no new
			// contracts
			if times > 20 {
				return nil
			}
			return errors.New("no contracts to cancel")
		}
		times = 0
		// Cancel any active contracts
		for _, contract := range rc.ActiveContracts {
			err = renter.RenterContractCancelPost(contract.ID)
			if err != nil {
				return err
			}
		}
		return errors.New("contracts still forming")
	})
	if err != nil {
		t.Fatal(err)
	}
	// Confirm that contracts were formed
	rc, err := renter.RenterInactiveContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.ActiveContracts) == 0 && len(rc.InactiveContracts) == 0 {
		t.Fatal("No Contracts formed")
	}

	// Confirm that the total allocated did not exceed the allowance funds
	rg, err := renter.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	funds := rg.Settings.Allowance.Funds
	allocated := rg.FinancialMetrics.TotalAllocated
	if funds.Cmp(allocated) < 0 {
		t.Fatalf("%v allocated exceeds allowance of %v", allocated, funds)
	}
}

// TestRenterLosingHosts tests that hosts will be replaced if they go offline
// and downloads will succeed with hosts going offline until the redundancy
// drops below 1
func TestRenterLosingHosts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup without a renter so renter can be added with custom
	// allowance
	groupParams := uplotest.GroupParams{
		Hosts:  4,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add renter to the group
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	renterParams.Allowance = uplotest.DefaultAllowance
	renterParams.Allowance.Hosts = 3 // hosts-1
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal("Failed to add renter:", err)
	}
	r := nodes[0]

	// Remember hosts with whom there are contracts
	rc, err := r.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	contractHosts := make(map[string]struct{})
	for _, c := range rc.ActiveContracts {
		contractHosts[c.HostPublicKey.String()] = struct{}{}
	}

	// Upload a file
	_, rf, err := r.UploadNewFileBlocking(100, 2, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	// File should be at redundancy of 1.5
	file, err := r.RenterFileGet(rf.UploPath())
	if err != nil {
		t.Fatal(err)
	}
	if file.File.Redundancy != 1.5 {
		t.Fatal("Expected filed redundancy to be 1.5 but was", file.File.Redundancy)
	}

	// Verify we can download the file
	_, _, err = r.DownloadToDisk(rf, false)
	if err != nil {
		t.Fatal(err)
	}

	// Stop one of the hosts that the renter has a contract with
	var pk types.UploPublicKey
	for _, h := range tg.Hosts() {
		pk, err = h.HostPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := contractHosts[pk.String()]; !ok {
			continue
		}
		if err = tg.StopNode(h); err != nil {
			t.Fatal(err)
		}
		break
	}

	// Wait for contract to be replaced
	loop := 0
	m := tg.Miners()[0]
	err = build.Retry(600, 100*time.Millisecond, func() error {
		if loop%10 == 0 {
			if err := m.MineBlock(); err != nil {
				return err
			}
		}
		loop++
		rc, err = r.RenterContractsGet()
		if err != nil {
			return err
		}
		err = uplotest.CheckExpectedNumberOfContracts(r, int(renterParams.Allowance.Hosts), 0, 0, 1, 0, 0)
		if err != nil {
			return err
		}
		for _, c := range rc.ActiveContracts {
			if _, ok := contractHosts[c.HostPublicKey.String()]; !ok {
				contractHosts[c.HostPublicKey.String()] = struct{}{}
				return nil
			}
		}
		return errors.New("Contract not formed with new host")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Remove stopped host for map
	delete(contractHosts, pk.String())

	// Since there is another host, another contract should form and the
	// redundancy should stay at 1.5
	err = build.Retry(100, 200*time.Millisecond, func() error {
		file, err := r.RenterFileGet(rf.UploPath())
		if err != nil {
			return err
		}
		if file.File.Redundancy != 1.5 {
			return fmt.Errorf("Expected redundancy to be 1.5 but was %v", file.File.Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify that renter can still download file
	_, _, err = r.DownloadToDisk(rf, false)
	if err != nil {
		t.Fatal(err)
	}

	// Stop another one of the hosts that the renter has a contract with
	for _, h := range tg.Hosts() {
		pk, err = h.HostPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := contractHosts[pk.String()]; !ok {
			continue
		}
		if err = tg.StopNode(h); err != nil {
			t.Fatal(err)
		}
		break
	}
	// Remove stopped host for map
	delete(contractHosts, pk.String())

	// Now that the renter has fewer hosts online than needed the redundancy
	// should drop to 1
	err = build.Retry(100, 100*time.Millisecond, func() error {
		file, err := r.RenterFileGet(rf.UploPath())
		if err != nil {
			return err
		}
		if file.File.Redundancy != 1 {
			return fmt.Errorf("Expected redundancy to be 1 but was %v", file.File.Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify that renter can still download file
	if _, _, err = r.DownloadToDisk(rf, false); err != nil {
		r.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Stop another one of the hosts that the renter has a contract with
	for _, h := range tg.Hosts() {
		pk, err = h.HostPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := contractHosts[pk.String()]; !ok {
			continue
		}
		if err = tg.StopNode(h); err != nil {
			t.Fatal(err)
		}
		break
	}
	// Remove stopped host for map
	delete(contractHosts, pk.String())

	// Now that the renter only has one host online the redundancy should be 0.5
	err = build.Retry(100, 100*time.Millisecond, func() error {
		files, err := r.RenterFilesGet(false)
		if err != nil {
			return err
		}
		if files.Files[0].Redundancy != 0.5 {
			return fmt.Errorf("Expected redundancy to be 0.5 but was %v", files.Files[0].Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the download will now fail because the file is less than a
	// redundancy of 1
	_, _, err = r.DownloadToDisk(rf, false)
	if err == nil {
		t.Fatal("Expected download to fail")
	}
}

// TestRenterFailingStandbyDownload checks a very specific edge case regarding
// standby workers. It uploads a file with a 2/3 redundancy to 4 hosts, causes
// a single piece to be stored on 2 hosts. Then it will take 3 hosts offline,
// Since 4 hosts are in the worker pool but only 2 are needed, Uplo will put 2
// of them on standby and try to download from the other 2. Since only 1 worker
// can succeed, Uplo should wake up one worker after another until it finally
// realizes that it doesn't have enough workers and the download fails.
func TestRenterFailingStandbyDownload(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup without a renter so renter can be added with custom
	// allowance
	groupParams := uplotest.GroupParams{
		Hosts:  4,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add renter to the group
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	renterParams.Allowance = uplotest.DefaultAllowance
	renterParams.Allowance.Hosts = 3
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal("Failed to add renter:", err)
	}
	r := nodes[0]

	// Remember hosts with whom there are contracts
	rc, err := r.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	contractHosts := make(map[string]struct{})
	for _, c := range rc.ActiveContracts {
		if _, ok := contractHosts[c.HostPublicKey.String()]; ok {
			continue
		}
		contractHosts[c.HostPublicKey.String()] = struct{}{}
	}

	// Upload a file
	_, rf, err := r.UploadNewFileBlocking(100, 2, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	// File should be at redundancy of 1.5
	files, err := r.RenterFilesGet(false)
	if err != nil {
		t.Fatal(err)
	}
	if files.Files[0].Redundancy != 1.5 {
		t.Fatal("Expected filed redundancy to be 1.5 but was", files.Files[0].Redundancy)
	}

	// Stop one of the hosts that the renter has a contract with
	var pk types.UploPublicKey
	var stoppedHost *uplotest.TestNode
	for _, h := range tg.Hosts() {
		pk, err = h.HostPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := contractHosts[pk.String()]; !ok {
			continue
		}
		if err = tg.StopNode(h); err != nil {
			t.Fatal(err)
		}
		stoppedHost = h
		break
	}

	// Wait for contract to be replaced
	loop := 0
	m := tg.Miners()[0]
	err = build.Retry(100, 100*time.Millisecond, func() error {
		if loop%10 == 0 {
			if err := m.MineBlock(); err != nil {
				return err
			}
		}
		loop++
		rc, err = r.RenterContractsGet()
		if err != nil {
			return err
		}
		if len(rc.ActiveContracts) != int(renterParams.Allowance.Hosts) {
			return fmt.Errorf("Expected %v contracts but got %v", int(renterParams.Allowance.Hosts), len(rc.ActiveContracts))
		}
		for _, c := range rc.ActiveContracts {
			if _, ok := contractHosts[c.HostPublicKey.String()]; !ok {
				return nil
			}
		}
		return errors.New("Contract not formed with new host")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Since there is another host, another contract should form and the
	// redundancy should stay at 1.5
	err = build.Retry(100, 100*time.Millisecond, func() error {
		files, err := r.RenterFilesGet(false)
		if err != nil {
			return err
		}
		if files.Files[0].Redundancy != 1.5 {
			return fmt.Errorf("Expected redundancy to be 1.5 but was %v", files.Files[0].Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Bring the stopped host back up.
	pk, _ = stoppedHost.HostPublicKey()
	if err := tg.StartNode(stoppedHost); err != nil {
		t.Fatal(err)
	}

	// Announce it again to speed discovery up.
	if err := stoppedHost.HostAnnouncePost(); err != nil {
		t.Fatal(err)
	}

	// Wait until the contract is considered good again.
	loop = 0
	err = build.Retry(600, 500*time.Millisecond, func() error {
		if loop%10 == 0 {
			if err := m.MineBlock(); err != nil {
				return err
			}
		}
		loop++
		rc, err = r.RenterContractsGet()
		if err != nil {
			return err
		}
		if len(rc.ActiveContracts) != int(renterParams.Allowance.Hosts)+1 {
			return fmt.Errorf("Expected %v contracts but got %v", renterParams.Allowance.Hosts+1, len(rc.ActiveContracts))
		}
		return nil
	})
	if err != nil {
		r.PrintDebugInfo(t, true, false, true)
		t.Fatal(err)
	}

	// Stop 3 out of 4 hosts. We didn't add the replacement host to
	// contractHosts so it should contain the original 3 hosts.
	stoppedHosts := 0
	for _, h := range tg.Hosts() {
		pk, err = h.HostPublicKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := contractHosts[pk.String()]; !ok {
			continue
		}
		if err = tg.StopNode(h); err != nil {
			t.Fatal(err)
		}
		stoppedHosts++
	}

	// Check that we stopped the right amount of hosts.
	if stoppedHosts != len(tg.Hosts())-1 {
		t.Fatalf("Expected to stop %v hosts but was %v", stoppedHosts, len(tg.Hosts())-1)
	}

	// Verify that the download will now fail because the file is less than a
	// redundancy of 1
	_, _, err = r.DownloadToDisk(rf, false)
	if err == nil {
		t.Fatal("Expected download to fail")
	}
}

// TestRenterPersistData checks if the RenterSettings are persisted
func TestRenterPersistData(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get test directory
	testDir := renterTestDir(t.Name())

	// Copying legacy file to test directory
	source := "../../compatibility/renter_v04.json"
	destination := filepath.Join(testDir, "renter", "renter.json")
	if err := copyFile(source, destination); err != nil {
		t.Fatal(err)
	}

	// Create new node from legacy renter.json persistence file
	r, err := uplotest.NewNode(node.AllModules(testDir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err = r.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Set renter allowance to finish renter set up
	// Currently /renter POST endpoint errors if the allowance
	// is not previously set or passed in as an argument
	err = r.RenterPostAllowance(uplotest.DefaultAllowance)
	if err != nil {
		t.Fatal(err)
	}

	// Check Settings, should be defaults
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if rg.Settings.MaxDownloadSpeed != renter.DefaultMaxDownloadSpeed {
		t.Fatalf("MaxDownloadSpeed not set to default of %v, set to %v",
			renter.DefaultMaxDownloadSpeed, rg.Settings.MaxDownloadSpeed)
	}
	if rg.Settings.MaxUploadSpeed != renter.DefaultMaxUploadSpeed {
		t.Fatalf("MaxUploadSpeed not set to default of %v, set to %v",
			renter.DefaultMaxUploadSpeed, rg.Settings.MaxUploadSpeed)
	}

	// Set StreamCacheSize, MaxDownloadSpeed, and MaxUploadSpeed to new values
	cacheSize := uint64(4)
	ds := int64(20)
	us := int64(10)
	if err := r.RenterSetStreamCacheSizePost(cacheSize); err != nil {
		t.Fatalf("%v: Could not set StreamCacheSize to %v", err, cacheSize)
	}
	if err := r.RenterRateLimitPost(ds, us); err != nil {
		t.Fatalf("%v: Could not set RateLimits to %v and %v", err, ds, us)
	}
	defer func() {
		if err := r.RenterRateLimitPost(0, 0); err != nil {
			t.Fatal(err)
		}
	}()

	// Confirm Settings were updated
	rg, err = r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if rg.Settings.MaxDownloadSpeed != ds {
		t.Fatalf("MaxDownloadSpeed not set to %v, set to %v", ds, rg.Settings.MaxDownloadSpeed)
	}
	if rg.Settings.MaxUploadSpeed != us {
		t.Fatalf("MaxUploadSpeed not set to %v, set to %v", us, rg.Settings.MaxUploadSpeed)
	}

	// Restart node
	err = r.RestartNode()
	if err != nil {
		t.Fatal("Failed to restart node:", err)
	}

	// check Settings, settings should be values set through API endpoints
	rg, err = r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if rg.Settings.MaxDownloadSpeed != ds {
		t.Fatalf("MaxDownloadSpeed not persisted as %v, set to %v", ds, rg.Settings.MaxDownloadSpeed)
	}
	if rg.Settings.MaxUploadSpeed != us {
		t.Fatalf("MaxUploadSpeed not persisted as %v, set to %v", us, rg.Settings.MaxUploadSpeed)
	}
}

// testZeroByteFile tests uploading and downloading a 0 and 1 byte file
func testZeroByteFile(t *testing.T, tg *uplotest.TestGroup) {
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}
	// Grab renter
	r := tg.Renters()[0]

	// Create 0 and 1 byte file
	zeroByteFile := 0
	oneByteFile := 1

	// Test uploading 0 byte file
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	redundancy := float64((dataPieces + parityPieces) / dataPieces)
	_, zeroRF, err := r.UploadNewFile(zeroByteFile, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	// Get zerobyte file
	rf, err := r.File(zeroRF)
	if err != nil {
		t.Fatal(err)
	}
	// Check redundancy and upload progress
	if rf.Redundancy != redundancy {
		t.Fatalf("Expected redundancy to be %v, got %v", redundancy, rf.Redundancy)
	}
	if rf.UploadProgress != 100 {
		t.Fatalf("Expected upload progress to be 100, got %v", rf.UploadProgress)
	}
	// Check health information
	if rf.Health != 0 {
		t.Fatalf("Expected health to be 0, got %v", rf.Health)
	}
	if rf.MaxHealth != 0 {
		t.Fatalf("Expected max health to be 0, got %v", rf.MaxHealth)
	}
	if rf.MaxHealthPercent != 100 {
		t.Fatalf("Expected max health percentage to be 100, got %v", rf.MaxHealthPercent)
	}
	if rf.NumStuckChunks != 0 {
		t.Fatalf("Expected number of stuck chunks to be 0, got %v", rf.NumStuckChunks)
	}
	if rf.Stuck {
		t.Fatalf("Expected file not to be stuck")
	}
	if rf.StuckHealth != 0 {
		t.Fatalf("Expected stuck health to be 0, got %v", rf.StuckHealth)
	}
	// Get the same file using the /renter/files endpoint with 'cached' set to
	// true.
	rfs, err := r.Files(true)
	if err != nil {
		t.Fatal(err)
	}
	var rf2 modules.FileInfo
	var found bool
	for _, file := range rfs {
		if file.UploPath.Equals(rf.UploPath) {
			found = true
			rf2 = file
			break
		}
	}
	if !found {
		t.Fatal("couldn't find uploaded file using /renter/files endpoint")
	}
	// Compare the fields again.
	if rf.Redundancy != rf2.Redundancy {
		t.Fatalf("Expected redundancy to be %v, got %v", rf.Redundancy, rf2.Redundancy)
	}
	if rf.UploadProgress != rf2.UploadProgress {
		t.Fatalf("Expected upload progress to be %v, got %v", rf.UploadProgress, rf2.UploadProgress)
	}
	if rf.Health != rf2.Health {
		t.Fatalf("Expected health to be %v, got %v", rf.Health, rf2.Health)
	}
	if rf.MaxHealth != rf2.MaxHealth {
		t.Fatalf("Expected max health to be %v, got %v", rf.MaxHealth, rf2.MaxHealth)
	}
	if rf.MaxHealthPercent != rf2.MaxHealthPercent {
		t.Fatalf("Expected max health percentage to be %v, got %v", rf.MaxHealthPercent, rf2.MaxHealthPercent)
	}
	if rf.NumStuckChunks != rf2.NumStuckChunks {
		t.Fatalf("Expected number of stuck chunks to be %v, got %v", rf.NumStuckChunks, rf2.NumStuckChunks)
	}
	if rf.Stuck != rf2.Stuck {
		t.Fatalf("Expected stuck to be %v, got %v", rf.Stuck, rf2.Stuck)
	}
	if rf.StuckHealth != rf2.StuckHealth {
		t.Fatalf("Expected stuck health to be %v, got %v", rf.StuckHealth, rf2.StuckHealth)
	}

	// Test uploading 1 byte file
	_, oneRF, err := r.UploadNewFileBlocking(oneByteFile, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Test downloading 0 byte file
	_, _, err = r.DownloadToDisk(zeroRF, false)
	if err != nil {
		t.Fatal(err)
	}

	// Test downloading 1 byte file
	_, _, err = r.DownloadToDisk(oneRF, false)
	if err != nil {
		t.Fatal(err)
	}
}

// TestRenterFileChangeDuringDownload confirms that a download will continue and
// succeed if the file is renamed or deleted after the download has started
func TestRenterFileChangeDuringDownload(t *testing.T) {
	if !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup,
	groupParams := uplotest.GroupParams{
		Hosts:   2,
		Renters: 1,
		Miners:  1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Grab Renter and upload file
	r := tg.Renters()[0]
	dataPieces := uint64(1)
	parityPieces := uint64(1)
	chunkSize := int64(uplotest.ChunkSize(dataPieces, crypto.TypeDefaultRenter))
	fileSize := 3 * int(chunkSize)
	_, rf1, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	_, rf2, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	_, rf3, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	_, rf4, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	_, rf5, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Set the bandwidth limit to 1 chunk per second.
	if err := r.RenterRateLimitPost(chunkSize, chunkSize); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.RenterRateLimitPost(0, 0); err != nil {
			t.Fatal(err)
		}
	}()

	// Create Wait group
	wg := new(sync.WaitGroup)

	// Test Renaming while Downloading and Streaming on 5 files.
	wg.Add(1)
	go renameDuringDownloadAndStream(r, rf1, t, wg, time.Second)
	wg.Add(1)
	go renameDuringDownloadAndStream(r, rf2, t, wg, time.Second)
	wg.Add(1)
	go renameDuringDownloadAndStream(r, rf3, t, wg, time.Second)
	wg.Add(1)
	go renameDuringDownloadAndStream(r, rf4, t, wg, time.Second)
	wg.Add(1)
	go renameDuringDownloadAndStream(r, rf5, t, wg, time.Second)
	wg.Wait()

	// Test Deleting while Downloading and Streaming
	//
	// Download the file
	wg.Add(1)
	go deleteDuringDownloadAndStream(r, rf1, t, wg, time.Second)
	wg.Add(1)
	go deleteDuringDownloadAndStream(r, rf2, t, wg, time.Second)
	wg.Add(1)
	go deleteDuringDownloadAndStream(r, rf3, t, wg, time.Second)
	wg.Add(1)
	go deleteDuringDownloadAndStream(r, rf4, t, wg, time.Second)
	wg.Add(1)
	go deleteDuringDownloadAndStream(r, rf5, t, wg, time.Second)

	wg.Wait()
}

// TestSetFileTrackingPath tests if changing the repairPath of a file works.
func TestSetFileTrackingPath(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup.
	gp := uplotest.GroupParams{
		Hosts:   5,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterTestDir(t.Name()), gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Grab the first of the group's renters
	renter := tg.Renters()[0]
	// Check that we have enough hosts for this test.
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}
	// Set fileSize and redundancy for upload
	fileSize := int(modules.SectorSize)
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces

	// Upload file
	localFile, remoteFile, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	// Move the file to a new location.
	if err := localFile.Move(); err != nil {
		t.Fatal(err)
	}
	// Take down all the hosts.
	numHosts := len(tg.Hosts())
	for _, host := range tg.Hosts() {
		if err := tg.RemoveNode(host); err != nil {
			t.Fatal("Failed to shutdown host", err)
		}
	}
	// File should have 0 redundancy now.
	if err := renter.WaitForDecreasingRedundancy(remoteFile, 0); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}
	// Rename the repairPath to match the new location.
	if err := renter.SetFileRepairPath(remoteFile, localFile); err != nil {
		t.Fatal("Failed to change the repair path", err)
	}
	// Create new hosts.
	_, err = tg.AddNodeN(node.HostTemplate, numHosts)
	if err != nil {
		t.Fatal("Failed to create a new host", err)
	}
	// We should reach full health again.
	if err := renter.WaitForUploadHealth(remoteFile); err != nil {
		t.Logf("numHosts: %v", len(tg.Hosts()))
		t.Fatal("File wasn't repaired", err)
	}
	// We should be able to download
	if _, _, err := renter.DownloadByStream(remoteFile); err != nil {
		t.Fatal("Failed to download file", err)
	}
	// Create a new file that is smaller than the first one.
	smallFile, err := renter.FilesDir().NewFile(fileSize - 1)
	if err != nil {
		t.Fatal(err)
	}
	// Try to change the repairPath of the remote file again. This shouldn't
	// work.
	if err := renter.SetFileRepairPath(remoteFile, smallFile); err == nil {
		t.Fatal("Changing repair path to file of different size shouldn't work")
	}
	// Delete the small file and try again. This also shouldn't work.
	if err := smallFile.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := renter.SetFileRepairPath(remoteFile, smallFile); err == nil {
		t.Fatal("Changing repair path to a nonexistent file shouldn't work")
	}
}

// TestRenterFileContractIdentifier checks that the file contract's identifier
// is set correctly when forming a contract and after renewing it.
func TestRenterFileContractIdentifier(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup, creating without renter so the renter's
	// contract transactions can easily be obtained.
	groupParams := uplotest.GroupParams{
		Hosts:  2,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add a Renter node
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	r := nodes[0]

	rcg, err := r.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	// Get the heights of the contracts.
	sh := rcg.ActiveContracts[0].StartHeight
	eh := rcg.ActiveContracts[0].EndHeight

	// Get the blockheight.
	cg, err := r.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}
	bh := cg.Height

	// Get the allowance
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	allowance := rg.Settings.Allowance
	renewWindow := allowance.RenewWindow

	// Mine blocks until we reach the renew window.
	m := tg.Miners()[0]
	for ; bh < types.BlockHeight(eh-renewWindow); bh++ {
		if err := m.MineBlock(); err != nil {
			t.Fatal(err)
		}
	}

	// We have reached the renew window. Slowly mine through it and check the
	// contracts.
	err = build.Retry(int(renewWindow), time.Second, func() error {
		if err := m.MineBlock(); err != nil {
			return err
		}
		// Get the allowance. The period might have changed.
		rg, err = r.RenterGet()
		if err != nil {
			t.Fatal(err)
		}
		allowance = rg.Settings.Allowance

		if sh < rg.CurrentPeriod {
			// Contracts are expired right away
			return uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, 0, len(tg.Hosts()), 0)
		}
		// Contracts are just disabled
		return uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, len(tg.Hosts()), 0, 0)
	})
	if err != nil {
		r.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	// Mine blocks until after the renew window. The disabled contracts should
	// become expired.
	err = build.Retry(2*int(renewWindow), 100*time.Millisecond, func() error {
		if err := m.MineBlock(); err != nil {
			return err
		}
		return uplotest.CheckExpectedNumberOfContracts(r, len(tg.Hosts()), 0, 0, 0, len(tg.Hosts()), 0)
	})
	if err != nil {
		r.PrintDebugInfo(t, true, true, true)
		t.Fatal(err)
	}

	var fcTxns []modules.ProcessedTransaction
	tries := 0
	err = build.Retry(100, 100*time.Millisecond, func() error {
		if tries%10 == 0 {
			if err := m.MineBlock(); err != nil {
				return err
			}
		}
		tries++
		// Get the transaction which are related to the renter since we started
		// the renter.
		txns, err := r.WalletTransactionsGet(0, ^types.BlockHeight(0))
		if err != nil {
			return err
		}

		// Filter out transactions without file contracts.
		fcTxns = make([]modules.ProcessedTransaction, 0)
		for _, txn := range txns.ConfirmedTransactions {
			if len(txn.Transaction.FileContracts) > 0 {
				fcTxns = append(fcTxns, txn)
			}
		}

		// There should be twice as many transactions with contracts as there
		// are hosts.
		if len(fcTxns) != 2*len(tg.Hosts()) {
			return fmt.Errorf("Expected %v txns but got %v", 2*len(tg.Hosts()), len(fcTxns))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Get the wallet seed of the renter.
	wsg, err := r.WalletSeedsGet()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := modules.StringToSeed(wsg.PrimarySeed, "english")
	if err != nil {
		t.Fatal(err)
	}
	renterSeed := modules.DeriveRenterSeed(seed)
	defer fastrand.Read(renterSeed[:])

	// Check the arbitrary data of each transaction and contract.
	for _, fcTxn := range fcTxns {
		txn := fcTxn.Transaction
		for _, fc := range txn.FileContracts {
			// Check that the arbitrary data has correct length.
			if len(txn.ArbitraryData) != 1 {
				t.Fatal("arbitrary data has wrong length")
			}
			csi := modules.ContractSignedIdentifier{}
			n := copy(csi[:], txn.ArbitraryData[0])
			encryptedHostKey := txn.ArbitraryData[0][n:]
			// Calculate the renter seed given the WindowStart of the contract.
			rs := renterSeed.EphemeralRenterSeed(fc.WindowStart)
			// Check if the identifier is valid.
			spk, valid, err := csi.IsValid(rs, txn, encryptedHostKey)
			if err != nil {
				t.Fatal(err)
			}
			if !valid {
				t.Fatal("identifier is invalid")
			}
			// Check that the host's key is a valid key from the hostb.
			_, err = r.HostDbHostsGet(spk)
			if err != nil {
				t.Fatal("hostKey is invalid", err)
			}
		}
	}
}

// TestUploadAfterDelete tests that rapidly uploading a file to the same
// uplopath as a previously deleted file works.
func TestUploadAfterDelete(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup.
	groupParams := uplotest.GroupParams{
		Hosts:  2,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add a Renter node
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	renterParams.RenterDeps = &dependencies.DependencyDisableCloseUploadEntry{}
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Upload file, creating a piece for each host in the group
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := int(modules.SectorSize)
	localFile, remoteFile, err := renter.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	// Repeatedly upload and delete a file with the same UploPath without
	// closing the entry. That shouldn't cause issues.
	for i := 0; i < 5; i++ {
		// Delete the file.
		if err := renter.RenterFileDeletePost(remoteFile.UploPath()); err != nil {
			t.Fatal(err)
		}
		// Upload the file again right after deleting it.
		if _, err := renter.UploadBlocking(localFile, dataPieces, parityPieces, false); err != nil {
			t.Fatal(err)
		}
	}

	// Create an empty directory on the renter called 'dir.uplo'. This triggers
	// an edge case where calling /renter/delete on that directory in an old
	// version of the code would cause the directory to be deleted.
	sp, err := modules.NewUploPath("dir.uplo")
	if err != nil {
		t.Fatal(err)
	}
	err = renter.RenterDirCreatePost(sp)
	if err != nil {
		t.Fatal(err)
	}

	// Call delete file on that new dir.
	err = renter.RenterFileDeletePost(sp)
	if err == nil {
		t.Fatal("calling 'delete file' on empty dir should return an error")
	}
	// Check that the dir still exists.
	_, err = renter.RenterDirGet(sp)
	if err != nil {
		t.Fatal(err)
	}
}

// TestUplofileCompatCodeV137 checks that legacy renters can upgrade from the
// v137 uplofile format.
func TestUplofileCompatCodeV137(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get test directory
	testDir := renterTestDir(t.Name())

	// The uplopath stored in the legacy file.
	expectedUploPath, err := modules.NewUploPath("sub1/sub2/testfile")
	if err != nil {
		t.Fatal(err)
	}

	// Copying legacy file to test directory
	renterDir := filepath.Join(testDir, "renter")
	source := filepath.Join("..", "..", "compatibility", "uplofile_v1.3.7.uplo")
	destination := filepath.Join(renterDir, "sub1", "sub2", "testfile.uplo")
	if err := copyFile(source, destination); err != nil {
		t.Fatal(err)
	}
	// Copy the legacy settings file to the test directory.
	source2 := "../../compatibility/renter_v137.json"
	destination2 := filepath.Join(renterDir, "renter.json")
	if err := copyFile(source2, destination2); err != nil {
		t.Fatal(err)
	}
	// Copy the legacy contracts into the test directory.
	contractsSource := "../../compatibility/contracts_v137"
	contracts, err := ioutil.ReadDir(contractsSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, fi := range contracts {
		contractDst := filepath.Join(contractsSource, fi.Name())
		err := copyFile(contractDst, filepath.Join(renterDir, "contracts", fi.Name()))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create new node with legacy uplo file.
	r, err := uplotest.NewNode(node.AllModules(testDir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err = r.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Make sure the folder containing the legacy file was deleted.
	if _, err := os.Stat(filepath.Join(renterDir, "sub1")); !os.IsNotExist(err) {
		t.Fatal("Error should be ErrNotExist but was", err)
	}
	// Make sure the uplofile is exactly where we would expect it.
	expectedLocation := filepath.Join(renterDir, modules.FileSystemRoot, modules.UserFolder.String(), "sub1", "sub2", "testfile.uplo")
	if _, err := os.Stat(expectedLocation); err != nil {
		t.Fatal(err)
	}
	// Check that exactly 1 uplofile exists and that it's the correct one.
	fis, err := r.Files(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fis) != 1 {
		t.Fatal("Expected 1 file but got", len(fis))
	}
	if fis[0].UploPath != expectedUploPath {
		t.Fatalf("Uplopath should be '%v' but was '%v'",
			expectedUploPath, fis[0].UploPath)
	}
	// Check the other fields of the files in a loop since the cached fields might
	// need some time to update.
	err = build.Retry(100, time.Second, func() error {
		fis, err := r.Files(false)
		if err != nil {
			return err
		}
		sf := fis[0]
		if sf.AccessTime.IsZero() {
			return errors.New("AccessTime wasn't set correctly")
		}
		if sf.ChangeTime.IsZero() {
			return errors.New("ChangeTime wasn't set correctly")
		}
		if sf.CreateTime.IsZero() {
			return errors.New("CreateTime wasn't set correctly")
		}
		if sf.ModificationTime.IsZero() {
			return errors.New("ModificationTime wasn't set correctly")
		}
		if sf.Available {
			return errors.New("File shouldn't be available since we don't know the hosts")
		}
		if sf.CipherType != crypto.TypeTwofish.String() {
			return fmt.Errorf("CipherType should be twofish but was: %v", sf.CipherType)
		}
		if sf.Filesize != 4096 {
			return fmt.Errorf("Filesize should be 4096 but was: %v", sf.Filesize)
		}
		if sf.Expiration != 91 {
			return fmt.Errorf("Expiration should be 91 but was: %v", sf.Expiration)
		}
		if sf.LocalPath != "/tmp/UploTesting/uplotest/TestRenterTwo/gctwr-EKYAZSVOZ6U2T4HZYIAQ/files/4096bytes 16951a61" {
			return errors.New("LocalPath doesn't match")
		}
		if sf.Redundancy != 0 {
			return errors.New("Redundancy should be 0 since we don't know the hosts")
		}
		if sf.UploadProgress != 100 {
			return fmt.Errorf("File was uploaded before so the progress should be 100 but was %v", sf.UploadProgress)
		}
		if sf.UploadedBytes != 40960 {
			return errors.New("Redundancy should be 10/20 so 10x the Filesize = 40960 bytes should be uploaded")
		}
		if sf.OnDisk {
			return errors.New("OnDisk should be false but was true")
		}
		if sf.Recoverable {
			return errors.New("Recoverable should be false but was true")
		}
		if !sf.Renewing {
			return errors.New("Renewing should be true but wasn't")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestUplofileCompatCodeV140 checks that legacy renters can upgrade from the
// v140 uplofile format.
func TestUplofileCompatCodeV140(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get test directory
	testDir := renterTestDir(t.Name())

	// Copy the legacy settings file to the test directory.
	renterDir := filepath.Join(testDir, "renter")
	source := "../../compatibility/renter_v140.json"
	destination := filepath.Join(renterDir, "renter.json")
	if err := copyFile(source, destination); err != nil {
		t.Fatal(err)
	}

	// Prepare a legacy snapshots and uplofiles folder which should be moved by
	// the upgrade code.
	uplofilesDir := filepath.Join(renterDir, "uplofiles")
	snapshotsDir := filepath.Join(renterDir, "snapshots")
	if err := os.MkdirAll(uplofilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snapshotsDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Add a dummy snapshot and uplofile to their corresponding folder.
	dummyUplofile := "foo.uplo"
	dummySnapshot := "bar.uplo"
	var f *os.File
	var err error
	if f, err = os.Create(filepath.Join(uplofilesDir, dummyUplofile)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if f, err = os.Create(filepath.Join(snapshotsDir, dummySnapshot)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// Create new node with legacy uplo file.
	r, err := uplotest.NewNode(node.AllModules(testDir))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	// Make sure the folders don't exist anymore.
	if _, err := os.Stat(uplofilesDir); !os.IsNotExist(err) {
		t.Fatal("Error should be ErrNotExist but was", err)
	}
	if _, err := os.Stat(snapshotsDir); !os.IsNotExist(err) {
		t.Fatal("Error should be ErrNotExist but was", err)
	}
	// Make sure the files are where we would expect them.
	expectedLocation := filepath.Join(renterDir, modules.FileSystemRoot, modules.UserFolder.String(), dummyUplofile)
	if _, err := os.Stat(expectedLocation); err != nil {
		t.Fatal(err)
	}
	expectedLocation = filepath.Join(renterDir, modules.FileSystemRoot, modules.BackupFolder.String(), dummySnapshot)
	if _, err := os.Stat(expectedLocation); err != nil {
		t.Fatal(err)
	}
}

// testFileAvailableAndRecoverable checks to make sure that the API properly
// reports if a file is available and/or recoverable
func testFileAvailableAndRecoverable(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	// Check that we have 5 hosts for this test so that the redundancy
	// assumptions work for the test
	if len(tg.Hosts()) != 5 {
		t.Fatal("This test requires 5 hosts")
	}

	// Set fileSize and redundancy for upload
	fileSize := int(modules.SectorSize)
	dataPieces := uint64(4)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces

	// Upload file
	localFile, remoteFile, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}

	// Get the file info and check if it is available and recoverable. File
	// should be available, recoverable, redundancy >1, and the file should be
	// on disk
	fi, err := r.File(remoteFile)
	if err != nil {
		t.Fatal("failed to get file info", err)
	}
	if fi.Redundancy < 1 {
		t.Fatal("redundancy of file is less than 1:", fi.Redundancy)
	}
	if !fi.OnDisk {
		t.Fatal("file is not on disk")
	}
	if !fi.Available {
		t.Fatal("file is not available")
	}
	if !fi.Recoverable {
		t.Fatal("file is not recoverable")
	}

	// Take down two hosts so that the redundancy drops below 1
	for i := 0; i < 2; i++ {
		if err := tg.RemoveNode(tg.Hosts()[0]); err != nil {
			t.Fatal("Failed to shutdown host", err)
		}
	}
	expectedRedundancy := float64(dataPieces+parityPieces-2) / float64(dataPieces)
	if err := r.WaitForDecreasingRedundancy(remoteFile, expectedRedundancy); err != nil {
		t.Fatal("Redundancy isn't decreasing", err)
	}

	// Get file into, file should not be available because the redundancy is  <1
	// but it should be recoverable because the file is on disk
	fi, err = r.File(remoteFile)
	if err != nil {
		t.Fatal("failed to get file info", err)
	}
	if fi.Redundancy >= 1 {
		t.Fatal("redundancy of file should be less than 1:", fi.Redundancy)
	}
	if !fi.OnDisk {
		t.Fatal("file is not on disk")
	}
	if fi.Available {
		t.Fatal("file should not be available")
	}
	if !fi.Recoverable {
		t.Fatal("file should be recoverable")
	}

	// Delete the file locally.
	if err := localFile.Delete(); err != nil {
		t.Fatal("failed to delete local file", err)
	}

	// Get file into, file should now not be available or recoverable
	fi, err = r.File(remoteFile)
	if err != nil {
		t.Fatal("failed to get file info", err)
	}
	if fi.Redundancy >= 1 {
		t.Fatal("redundancy of file should be less than 1:", fi.Redundancy)
	}
	if fi.OnDisk {
		t.Fatal("file is still on disk")
	}
	if fi.Available {
		t.Fatal("file should not be available")
	}
	if fi.Recoverable {
		t.Fatal("file should not be recoverable")
	}
}

// testSetFileStuck tests that manually setting the 'stuck' field of a file
// works as expected.
func testSetFileStuck(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	// Check if there are already uploaded file we can use.
	rfg, err := r.RenterFilesGet(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rfg.Files) == 0 {
		// Set fileSize and redundancy for upload
		dataPieces := uint64(len(tg.Hosts()) - 1)
		parityPieces := uint64(len(tg.Hosts())) - dataPieces
		fileSize := int(dataPieces * modules.SectorSize)

		// Upload file
		_, _, err := r.UploadNewFileBlocking(fileSize, dataPieces, parityPieces, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Get a file.
	rfg, err = r.RenterFilesGet(false)
	if err != nil {
		t.Fatal(err)
	}
	f := rfg.Files[0]
	// Set stuck to the opposite value it had before.
	if err := r.RenterSetFileStuckPost(f.UploPath, false, !f.Stuck); err != nil {
		t.Fatal(err)
	}
	// Check if it was set correctly.
	fi, err := r.RenterFileGet(f.UploPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.File.Stuck == f.Stuck {
		t.Fatalf("Stuck field should be %v but was %v", !f.Stuck, fi.File.Stuck)
	}
	// Set stuck to the original value.
	if err := r.RenterSetFileStuckPost(f.UploPath, false, f.Stuck); err != nil {
		t.Fatal(err)
	}
	// Check if it was set correctly.
	fi, err = r.RenterFileGet(f.UploPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.File.Stuck != f.Stuck {
		t.Fatalf("Stuck field should be %v but was %v", f.Stuck, fi.File.Stuck)
	}
	// Set stuck back once more using the root flag.
	rebased, err := f.UploPath.Rebase(modules.RootUploPath(), modules.UserFolder)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RenterSetFileStuckPost(rebased, true, !f.Stuck); err != nil {
		t.Fatal(err)
	}
	// Check if it was set correctly.
	fi, err = r.RenterFileGet(f.UploPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.File.Stuck == f.Stuck {
		t.Fatalf("Stuck field should be %v but was %v", !f.Stuck, fi.File.Stuck)
	}
}

// testEscapeUploPath tests that UploPaths are escaped correctly to handle escape
// characters
func testEscapeUploPath(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	// Check that we have enough hosts for this test.
	if len(tg.Hosts()) < 2 {
		t.Fatal("This test requires at least 2 hosts")
	}

	// Set fileSize and redundancy for upload
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces

	// Create Local File
	lf, err := r.FilesDir().NewFile(100)
	if err != nil {
		t.Fatal(err)
	}

	// File names to tests
	names := []string{
		"dollar$sign",
		"and&sign",
		"single`quote",
		"full:colon",
		"semi;colon",
		"hash#tag",
		"percent%sign",
		"at@sign",
		"less<than",
		"greater>than",
		"equal=to",
		"question?mark",
		"open[bracket",
		"close]bracket",
		"open{bracket",
		"close}bracket",
		"carrot^top",
		"pipe|pipe",
		"tilda~tilda",
		"plus+sign",
		"minus-sign",
		"under_score",
		"comma,comma",
		"apostrophy's",
		`quotation"marks`,
	}
	for _, s := range names {
		// Create UploPath
		uploPath, err := modules.NewUploPath(s)
		if err != nil {
			t.Fatal(err)
		}

		// Upload file
		_, err = r.Upload(lf, uploPath, dataPieces, parityPieces, false)
		if err != nil {
			t.Fatal(err)
		}

		// Confirm we can get file
		_, err = r.RenterFileGet(uploPath)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// testValidateUploPath tests the validate uplopath endpoint
func testValidateUploPath(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	r := tg.Renters()[0]

	// Create uplopaths to test
	var pathTests = []struct {
		path  string
		valid bool
	}{
		{`\\some\\windows\\path`, true},
		{"valid/uplopath", true},
		{"../../../directory/traversal", false},
		{"testpath", true},
		{"valid/uplopath/../with/directory/traversal", false},
		{"validpath/test", true},
		{"..validpath/..test", true},
		{"./invalid/path", false},
		{".../path", true},
		{"valid./path", true},
		{"valid../path", true},
		{"valid/path./test", true},
		{"valid/path../test", true},
		{"test/path", true},
		{"/leading/slash", false}, // this is not valid through the api because a leading slash is added by the api call so this turns into 2 leading slashes
		{"foo/./bar", false},
		{"", false},
		{"blank/end/", true}, // clean will trim trailing slashes so this is a valid input
		{"double//dash", false},
		{"../", false},
		{"./", false},
		{".", false},
	}
	// Test all uplopaths
	for _, pathTest := range pathTests {
		err := r.RenterValidateUploPathPost(pathTest.path)
		// Verify expected Error
		if err != nil && pathTest.valid {
			t.Fatal("validateUplopath failed on valid path: ", pathTest.path)
		}
		if err == nil && !pathTest.valid {
			t.Fatal("validateUplopath succeeded on invalid path: ", pathTest.path)
		}
	}

	// Create UploPaths that contain escape characters
	var escapeCharTests = []struct {
		path  string
		valid bool
	}{
		{"dollar$sign", true},
		{"and&sign", true},
		{"single`quote", true},
		{"full:colon", true},
		{"semi;colon", true},
		{"hash#tag", true},
		{"percent%sign", true},
		{"at@sign", true},
		{"less<than", true},
		{"greater>than", true},
		{"equal=to", true},
		{"question?mark", true},
		{"open[bracket", true},
		{"close]bracket", true},
		{"open{bracket", true},
		{"close}bracket", true},
		{"carrot^top", true},
		{"pipe|pipe", true},
		{"tilda~tilda", true},
		{"plus+sign", true},
		{"minus-sign", true},
		{"under_score", true},
		{"comma,comma", true},
		{"apostrophy's", true},
		{`quotation"marks`, true},
	}
	// Test all escape charcter uplopaths
	for _, escapeCharTest := range escapeCharTests {
		path := url.PathEscape(escapeCharTest.path)
		err := r.RenterValidateUploPathPost(path)
		// Verify expected Error
		if err != nil && escapeCharTest.valid {
			t.Fatalf("validateUplopath failed on valid path %v, escaped %v ", escapeCharTest.path, path)
		}
		if err == nil && !escapeCharTest.valid {
			t.Fatalf("validateUplopath succeeded on invalid path %v, escaped %v ", escapeCharTest.path, path)
		}
	}
}

// TestOutOfStorageHandling makes sure that we form a new contract to replace a
// host that has run out of storage while still keeping it around as
// goodForRenew.
func TestOutOfStorageHandling(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group with 1 default host.
	gp := uplotest.GroupParams{
		Hosts:  1,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Prepare a host that offers the minimum storage possible.
	hostTemplate := node.Host(filepath.Join(testDir, "host1"))
	hostTemplate.HostStorage = modules.SectorSize * contractmanager.MinimumSectorsPerStorageFolder

	// Prepare a renter that expects to upload 1 Sector of data to 2 hosts at a
	// 2x redundancy. We set the ExpectedStorage lower than the available
	// storage on the host to make sure it's not penalized.
	renterTemplate := node.Renter(filepath.Join(testDir, "renter"))
	dataPieces := uint64(1)
	parityPieces := uint64(1)
	allowance := uplotest.DefaultAllowance
	allowance.ExpectedRedundancy = float64(dataPieces+parityPieces) / float64(dataPieces)
	allowance.ExpectedStorage = modules.SectorSize // 4 KiB
	allowance.Hosts = 3
	renterTemplate.Allowance = allowance

	// Add the host and renter to the group.
	nodes, err := tg.AddNodes(hostTemplate)
	if err != nil {
		t.Fatal(err)
	}
	host := nodes[0]
	nodes, err = tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Upload a file to fill up the host.
	_, _, err = renter.UploadNewFileBlocking(int(hostTemplate.HostStorage), dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	// Make sure the host is full.
	hg, err := host.HostGet()
	if hg.ExternalSettings.RemainingStorage != 0 {
		t.Fatal("Expected remaining storage to be 0 but was", hg.ExternalSettings.RemainingStorage)
	}
	// Start uploading another file in the background to trigger the OOS error.
	_, rf, err := renter.UploadNewFile(int(2*modules.SectorSize), dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal(err)
	}
	// Make sure the host's contract is no longer good for upload but still good
	// for renew.
	err = build.Retry(10, time.Second, func() error {
		if err := tg.Miners()[0].MineBlock(); err != nil {
			t.Fatal(err)
		}
		hpk, err := host.HostPublicKey()
		if err != nil {
			return err
		}
		rcg, err := renter.RenterContractsGet()
		if err != nil {
			return err
		}
		// One contract should be good for uploads and renewal and is therefore
		// active.
		if len(rcg.ActiveContracts) != 1 {
			return fmt.Errorf("Expected 1 active contract but got %v", len(rcg.ActiveContracts))
		}
		// One contract should be good for renewal but not uploading and is
		// therefore passive.
		if len(rcg.PassiveContracts) != 1 {
			return fmt.Errorf("Expected 1 passive contract but got %v", len(rcg.PassiveContracts))
		}
		hostContract := rcg.PassiveContracts[0]
		if !hostContract.HostPublicKey.Equals(hpk) {
			return errors.New("Passive contract doesn't belong to the host")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add a new host for the renter to replace the old one with.
	_, err = tg.AddNodes(node.Host(filepath.Join(testDir, "host2")))
	if err != nil {
		t.Fatal(err)
	}
	// The file should reach full health now.
	if err := renter.WaitForUploadHealth(rf); err != nil {
		t.Fatal(err)
	}
	// There should be 2 active contracts now and 1 passive one.
	rcg, err := renter.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(rcg.ActiveContracts) != 2 {
		t.Fatal("Expected 2 active contracts but got", len(rcg.ActiveContracts))
	}
	if len(rcg.PassiveContracts) != 1 {
		t.Fatal("Expected 1 passive contract but got", len(rcg.PassiveContracts))
	}
	// After a while we give the host a new chance and it should be active
	// again.
	i := 0
	err = build.Retry(100, 100*time.Millisecond, func() error {
		i++
		if i%10 == 0 {
			if err := tg.Miners()[0].MineBlock(); err != nil {
				t.Fatal(err)
			}
		}

		rcg, err = renter.RenterContractsGet()
		if err != nil {
			t.Fatal(err)
		}
		if len(rcg.ActiveContracts) != 3 {
			if err := tg.Miners()[0].MineBlock(); err != nil {
				t.Fatal(err)
			}

			return fmt.Errorf("Expected 3 active contracts but got %v", len(rcg.ActiveContracts))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAsyncStartupRace queries some of the modules endpoints during an async
// startup.
func TestAsyncStartupRace(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	testDir := renterTestDir(t.Name())
	np := node.AllModules(testDir)
	// Disable the async startup part of the modules.
	deps := &dependencies.DependencyDisableAsyncStartup{}
	np.ConsensusSetDeps = deps
	np.ContractorDeps = deps
	np.HostDBDeps = deps
	np.RenterDeps = deps
	// Disable the modules which aren't loaded async anyway.
	np.CreateExplorer = false
	np.CreateHost = false
	np.CreateMiner = false
	node, err := uplotest.NewCleanNodeAsync(np)
	if err != nil {
		t.Fatal(err)
	}
	// Call some endpoints a few times.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		// ConsensusSet
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := node.ConsensusGet()
			if err != nil {
				t.Fatal(err)
			}
		}()
		// Contractor
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := node.RenterContractsGet()
			if err != nil {
				t.Fatal(err)
			}
		}()
		// HostDB
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := node.HostDbAllGet()
			if err != nil {
				t.Fatal(err)
			}
			_, err = node.HostDbGet()
			if err != nil {
				t.Fatal(err)
			}
		}()
		// Renter
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := node.RenterGet()
			if err != nil {
				t.Fatal(err)
			}
		}()
		wg.Wait()
	}
}

// testRenterPostCancelAllowance tests setting and cancelling the allowance
// through the /renter POST endpoint
func testRenterPostCancelAllowance(t *testing.T, tg *uplotest.TestGroup) {
	// Create renter, skip setting the allowance so that we can properly test
	renterParams := node.Renter(filepath.Join(renterTestDir(t.Name()), "renter"))
	renterParams.SkipSetAllowance = true
	nodes, err := tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal(err)
	}
	renter := nodes[0]

	// Set the allowance, with the two required fields to 0, this should fail
	allowance := uplotest.DefaultAllowance
	allowance.Funds = types.ZeroCurrency
	err = renter.RenterPostAllowance(allowance)
	if err == nil {
		t.Fatal("Should have returned an error")
	}
	if !strings.Contains(err.Error(), api.ErrFundsNeedToBeSet.Error()) {
		t.Fatalf("Expected error to contain %v but got %v", api.ErrFundsNeedToBeSet, err)
	}
	allowance.Funds = uplotest.DefaultAllowance.Funds
	allowance.Period = types.BlockHeight(0)
	err = renter.RenterPostAllowance(allowance)
	if err == nil {
		t.Fatal("Should have returned an error")
	}
	if !strings.Contains(err.Error(), api.ErrPeriodNeedToBeSet.Error()) {
		t.Fatalf("Expected error to contain %v but got %v", api.ErrPeriodNeedToBeSet, err)
	}

	// Set the allowance with only the required fields, confirm all other fields
	// are set to defaults
	allowance = modules.DefaultAllowance
	values := url.Values{}
	values.Set("funds", allowance.Funds.String())
	values.Set("period", fmt.Sprint(allowance.Period))
	err = renter.RenterPost(values)
	if err != nil {
		t.Fatal(err)
	}
	rg, err := renter.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	// RenewWindow gets set to half the period if not set by the user, check
	// separately
	renewWindow := rg.Settings.Allowance.RenewWindow
	period := rg.Settings.Allowance.Period
	if renewWindow != period/2 {
		t.Fatalf("Renew window, not set as expected: got %v expected %v", renewWindow, period/2)
	}
	allowance.RenewWindow = renewWindow
	if !reflect.DeepEqual(allowance, rg.Settings.Allowance) {
		t.Log("allownace", allowance)
		t.Log("rg.Settings.Allowance", rg.Settings.Allowance)
		t.Fatal("expected allowances to match")
	}

	// Save for later
	startingAllowance := allowance

	// Confirm contracts form
	expectedContracts := int(allowance.Hosts)
	err = build.Retry(100, 100*time.Millisecond, func() error {
		return uplotest.CheckExpectedNumberOfContracts(renter, expectedContracts, 0, 0, 0, 0, 0)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test zeroing out individual fields of the allowance
	allowance = modules.Allowance{}
	var paramstests = []struct {
		key   string
		value string
		err   error
	}{
		{"period", fmt.Sprint(allowance.Period), api.ErrPeriodNeedToBeSet},
		{"funds", allowance.Funds.String(), api.ErrFundsNeedToBeSet},
		{"hosts", fmt.Sprint(allowance.Hosts), contractor.ErrAllowanceNoHosts},
		{"renewwindow", fmt.Sprint(allowance.RenewWindow), contractor.ErrAllowanceZeroWindow},
		{"expectedstorage", fmt.Sprint(allowance.ExpectedStorage), contractor.ErrAllowanceZeroExpectedStorage},
		{"expectedupload", fmt.Sprint(allowance.ExpectedUpload), contractor.ErrAllowanceZeroExpectedUpload},
		{"expecteddownload", fmt.Sprint(allowance.ExpectedDownload), contractor.ErrAllowanceZeroExpectedDownload},
		{"expectedredundancy", fmt.Sprint(allowance.ExpectedRedundancy), contractor.ErrAllowanceZeroExpectedRedundancy},
	}

	for _, test := range paramstests {
		values = url.Values{}
		values.Set(test.key, test.value)
		err = renter.RenterPost(values)

		if err == nil {
			t.Logf("testing key %v and value %v", test.key, test.value)
			t.Fatalf("Expected error to contain %v but got %v", test.err, err)
		}
		if test.err != nil && !strings.Contains(err.Error(), test.err.Error()) {
			t.Logf("testing key %v and value %v", test.key, test.value)
			t.Fatalf("Expected error to contain %v but got %v", test.err, err)
		}
	}

	// Test setting a non allowance field, this should have no affect on the
	// allowance.
	values = url.Values{}
	values.Set("checkforipviolation", "true")
	err = renter.RenterPost(values)
	if err != nil {
		t.Fatal(err)
	}
	rg, err = renter.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(startingAllowance, rg.Settings.Allowance) {
		t.Log("allownace", startingAllowance)
		t.Log("rg.Settings.Allowance", rg.Settings.Allowance)
		t.Fatal("expected allowances to match")
	}

	// Cancel allowance by setting funds and period to zero
	values = url.Values{}
	values.Set("period", fmt.Sprint(allowance.Period))
	values.Set("funds", allowance.Funds.String())
	err = renter.RenterPost(values)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm contracts are disabled
	err = build.Retry(100, 100*time.Millisecond, func() error {
		return uplotest.CheckExpectedNumberOfContracts(renter, 0, 0, 0, expectedContracts, 0, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// testNextPeriod confirms that the value for NextPeriod in RenterGET is valid
func testNextPeriod(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the renter
	r := tg.Renters()[0]

	// Request RenterGET
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(rg.Settings.Allowance, modules.Allowance{}) {
		t.Fatal("test only is valid if the allowance is set")
	}

	// Check Next Period
	currentPeriod, err := r.RenterCurrentPeriod()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := r.RenterSettings()
	if err != nil {
		t.Fatal(err)
	}
	period := settings.Allowance.Period
	nextPeriod := rg.NextPeriod
	if nextPeriod == 0 {
		t.Fatal("NextPeriod should not be zero for a renter with an allowance and contracts")
	}
	if nextPeriod != currentPeriod+period {
		t.Fatalf("expected next period to be %v but got %v", currentPeriod+period, nextPeriod)
	}
}

// testPauseAndResumeRepairAndUploads tests that the Renter's API endpoint to
// pause and resume the repair and uploads works as intended
func testPauseAndResumeRepairAndUploads(t *testing.T, tg *uplotest.TestGroup) {
	// Grab Renter
	r := tg.Renters()[0]
	numHost := len(tg.Hosts())
	hostToAdd := 2

	// Confirm that starting out the Renter's uploads are not paused
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if rg.Settings.UploadsStatus.Paused {
		t.Fatal("Renter's uploads are paused at the beginning of the test")
	}
	if !rg.Settings.UploadsStatus.PauseEndTime.Equal(time.Time{}) {
		t.Fatalf("Pause end time should be null if the uploads are not paused but was %v", rg.Settings.UploadsStatus.PauseEndTime)
	}

	// Pause Repairs And Uploads with a high duration to ensure that the uploads
	// and repairs don't start before we want them to
	err = r.RenterUploadsPausePost(time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the Renter's uploads are now paused
	rg, err = r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if !rg.Settings.UploadsStatus.Paused {
		t.Fatal("Renter's uploads are not paused but should be")
	}
	if rg.Settings.UploadsStatus.PauseEndTime.Equal(time.Time{}) {
		t.Fatal("Pause end time should not be null if the uploads are paused")
	}

	// Try and Upload a file, the upload post should succeed but the upload
	// progress of the file should never increase because the uploads are
	// paused
	_, rf, err := r.UploadNewFile(100, 1, uint64(numHost+hostToAdd-1), false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		file, err := r.File(rf)
		if err != nil {
			t.Fatal(err)
		}
		if file.UploadProgress != 0 {
			t.Fatal("UploadProgress is increasing, expected it to stay at 0:", file.UploadProgress)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Resume Repair
	err = r.RenterUploadsResumePost()
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the Renter's uploads are no longer paused
	rg, err = r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if rg.Settings.UploadsStatus.Paused {
		t.Fatal("Renter's uploads are still paused")
	}
	if !rg.Settings.UploadsStatus.PauseEndTime.Equal(time.Time{}) {
		t.Fatalf("Pause end time should be null if the uploads are not paused but was %v", rg.Settings.UploadsStatus.PauseEndTime)
	}

	// Confirm Upload resumes and gets to the expected redundancy. There aren't
	// enough hosts yet to get to the fullRedundancy
	fullRedundancy := float64(numHost + hostToAdd)
	expectedRedundancy := float64(numHost)
	err = build.Retry(100, 250*time.Millisecond, func() error {
		file, err := r.File(rf)
		if err != nil {
			return err
		}
		if file.Redundancy < expectedRedundancy {
			return fmt.Errorf("redundancy should be %v but was %v", expectedRedundancy, file.Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Pause the repairs and uploads again
	err = r.RenterUploadsPausePost(time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the Renter's uploads are now paused
	rg, err = r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if !rg.Settings.UploadsStatus.Paused {
		t.Fatal("Renter's uploads are not paused but should be")
	}
	if rg.Settings.UploadsStatus.PauseEndTime.Equal(time.Time{}) {
		t.Fatal("Pause end time should not be null if the uploads are paused")
	}

	// Update renter's allowance to require making contracts with the new hosts
	allowance := rg.Settings.Allowance
	allowance.Hosts = uint64(numHost + hostToAdd)
	err = r.RenterPostAllowance(allowance)
	if err != nil {
		t.Fatal(err)
	}

	// Add hosts so upload can get to full redundancy
	_, err = tg.AddNodeN(node.HostTemplate, hostToAdd)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm upload still hasn't reach full redundancy because repairs are
	// paused
	for i := 0; i < 5; i++ {
		file, err := r.File(rf)
		if err != nil {
			t.Fatal(err)
		}
		if file.Redundancy == fullRedundancy {
			t.Fatalf("File Redundancy %v has reached full redundancy %v but shouldn't have", file.Redundancy, fullRedundancy)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Resume Repair and Upload by calling pause again with a very should time
	// duration so the repairs and uploads restart on their own
	err = r.RenterUploadsPausePost(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm file gets to full Redundancy
	err = build.Retry(100, 100*time.Millisecond, func() error {
		file, err := r.File(rf)
		if err != nil {
			return err
		}
		if file.Redundancy < fullRedundancy {
			return fmt.Errorf("redundancy should be %v but was %v", fullRedundancy, file.Redundancy)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the Renter's uploads are no longer paused
	rg, err = r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	if rg.Settings.UploadsStatus.Paused {
		t.Fatal("Renter's uploads are still paused")
	}
	if !rg.Settings.UploadsStatus.PauseEndTime.Equal(time.Time{}) {
		t.Fatalf("Pause end time should be null if the uploads are not paused but was %v", rg.Settings.UploadsStatus.PauseEndTime)
	}
}

// testDownloadServedFromDisk tests whether downloads will actually be served
// from disk.
func testDownloadServedFromDisk(t *testing.T, tg *uplotest.TestGroup) {
	// Make sure a renter is available for testing.
	if len(tg.Renters()) == 0 {
		renterParams := node.Renter(filepath.Join(renterTestDir(t.Name()), "renter"))
		_, err := tg.AddNodes(renterParams)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := tg.Renters()[0]
	// Upload a file. Choose more datapieces than hosts available to prevent the
	// file from reaching 1x redundancy. That way it will only be downloadable
	// from disk.
	_, rf, err := r.UploadNewFile(int(1000), uint64(len(tg.Hosts())+1), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	// Download it in all ways possible. The download will only succeed if
	// served from disk.
	_, _, err = r.DownloadByStreamWithDiskFetch(rf, false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = r.DownloadToDiskWithDiskFetch(rf, false, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.StreamWithDiskFetch(rf, false)
	if err != nil {
		t.Fatal(err)
	}
}

// testDirMode is a subtest that makes sure that various ways of creating a dir
// all set the correct permissions.
func testDirMode(t *testing.T, tg *uplotest.TestGroup) {
	// Grab the first of the group's renters
	renter := tg.Renters()[0]
	// Upload file, creating a piece for each host in the group
	dataPieces := uint64(1)
	parityPieces := uint64(len(tg.Hosts())) - dataPieces
	fileSize := fastrand.Intn(2*int(modules.SectorSize)) + uplotest.Fuzz() + 2 // between 1 and 2*SectorSize + 3 bytes

	dirSP, err := modules.NewUploPath("dir")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := renter.FilesDir().CreateDir(dirSP.String())
	if err != nil {
		t.Fatal(err)
	}
	lf, err := dir.NewFile(fileSize)
	if err != nil {
		t.Fatal(err)
	}
	_, err = renter.UploadBlocking(lf, dataPieces, parityPieces, false)
	if err != nil {
		t.Fatal("Failed to upload a file for testing: ", err)
	}
	// The fileupload should have created a dir. That dir should have the same
	// permissions as the file.
	rd, err := renter.RenterDirGet(dirSP)
	if err != nil {
		t.Fatal(err)
	}
	di := rd.Directories[0]
	fi, err := lf.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if di.DirMode != fi.Mode() {
		t.Fatalf("Expected folder permissions to be %v but was %v", fi.Mode(), di.DirMode)
	}
	// Test creating dir using endpoint.
	dir2SP := modules.RandomUploPath()
	if err := renter.RenterDirCreatePost(dir2SP); err != nil {
		t.Fatal(err)
	}
	rd, err = renter.RenterDirGet(dir2SP)
	if err != nil {
		t.Fatal(err)
	}
	di = rd.Directories[0]
	// The created dir should have the default permissions.
	if di.DirMode != modules.DefaultDirPerm {
		t.Fatalf("Expected folder permissions to be %v but was %v", modules.DefaultDirPerm, di.DirMode)
	}
	dir3SP := modules.RandomUploPath()
	mode := os.FileMode(0777)
	if err := renter.RenterDirCreateWithModePost(dir3SP, mode); err != nil {
		t.Fatal(err)
	}
	rd, err = renter.RenterDirGet(dir3SP)
	if err != nil {
		t.Fatal(err)
	}
	di = rd.Directories[0]
	// The created dir should have the specified permissions.
	if di.DirMode != mode {
		t.Fatalf("Expected folder permissions to be %v but was %v", mode, di.DirMode)
	}
}

// TestWorkerStatus probes the WorkerPoolStatus
func TestWorkerStatus(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a testgroup.
	groupParams := uplotest.GroupParams{
		Hosts:   2,
		Miners:  1,
		Renters: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	r := tg.Renters()[0]
	numHosts := len(tg.Hosts())

	// Build Contract ID and PubKey maps
	rc, err := r.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	contracts := make(map[types.FileContractID]struct{})
	pks := make(map[string]struct{})
	for _, c := range rc.ActiveContracts {
		contracts[c.ID] = struct{}{}
		pks[c.HostPublicKey.String()] = struct{}{}
	}

	err = build.Retry(100, 100*time.Millisecond, func() error {
		// Get the worker status
		rwg, err := r.RenterWorkersGet()
		if err != nil {
			t.Fatal(err)
		}

		// There should be the same number of workers as Hosts
		if rwg.NumWorkers != numHosts {
			t.Fatalf("Expected NumWorkers to be %v but got %v", numHosts, rwg.NumWorkers)
		}
		if len(rwg.Workers) != numHosts {
			t.Fatalf("Expected %v Workers but got %v", numHosts, len(rwg.Workers))
		}

		// No workers should be on cooldown
		if rwg.TotalDownloadCoolDown != 0 {
			t.Fatal("Didn't expect any workers on download cool down but found", rwg.TotalDownloadCoolDown)
		}
		if rwg.TotalUploadCoolDown != 0 {
			t.Fatal("Didn't expect any workers on upload cool down but found", rwg.TotalUploadCoolDown)
		}

		// Check Worker information
		for _, worker := range rwg.Workers {
			// Contract Field checks
			if _, ok := contracts[worker.ContractID]; !ok {
				return fmt.Errorf("Worker Contract ID not found in Contract map %v", worker.ContractID)
			}
			cu := worker.ContractUtility
			if !cu.GoodForUpload {
				return errors.New("Worker contract should be GFR")
			}
			if !cu.GoodForRenew {
				return errors.New("Worker contract should be GFR")
			}
			if cu.BadContract {
				return errors.New("Worker contract should not be marked as Bad")
			}
			if cu.LastOOSErr != 0 {
				return errors.New("Worker contract LastOOSErr should be 0")
			}
			if cu.Locked {
				return errors.New("Worker contract should not be locked")
			}
			if _, ok := pks[worker.HostPubKey.String()]; !ok {
				return fmt.Errorf("Worker PubKey not found in PubKey map %v", worker.HostPubKey)
			}

			// Download Field checks
			if worker.DownloadOnCoolDown {
				return errors.New("Worker should not be on cool down")
			}
			if worker.DownloadQueueSize != 0 {
				return fmt.Errorf("Expected download queue to be empty but was %v", worker.DownloadQueueSize)
			}
			if worker.DownloadTerminated {
				return errors.New("Worker should not be marked as DownloadTerminated")
			}

			// Upload Field checks
			if worker.UploadCoolDownError != "" {
				return fmt.Errorf("Cool down error should be nil but was %v", worker.UploadCoolDownError)
			}
			if worker.UploadCoolDownTime.Nanoseconds() >= 0 {
				return fmt.Errorf("Cool down time should be negative but was %v", worker.UploadCoolDownTime)
			}
			if worker.UploadOnCoolDown {
				return errors.New("Worker should not be on cool down")
			}
			if worker.UploadQueueSize != 0 {
				return fmt.Errorf("Expected upload queue to be empty but was %v", worker.UploadQueueSize)
			}
			if worker.UploadTerminated {
				return errors.New("Worker should not be marked as UploadTerminated")
			}

			// Account checks
			if !worker.AccountBalanceTarget.Equals(types.UplocoinPrecision) {
				return fmt.Errorf("Expected balance target to be 1SC but was %v", worker.AccountBalanceTarget.HumanString())
			}

			// AccountStatus checks
			if worker.AccountStatus.AvailableBalance.IsZero() {
				return fmt.Errorf("Expected available balance to be greater zero but was %v", worker.AccountStatus.AvailableBalance.HumanString())
			}
			if !worker.AccountStatus.NegativeBalance.IsZero() {
				return fmt.Errorf("Expected negative balance to be zero but was %v", worker.AccountStatus.NegativeBalance.HumanString())
			}
			if worker.AccountStatus.RecentErr != "" {
				return fmt.Errorf("Expected recent err to be nil but was %v", worker.AccountStatus.RecentErr)
			}

			// PriceTableStatus checks
			if worker.PriceTableStatus.RecentErr != "" {
				return fmt.Errorf("Expected recent err to be nil but was %v", worker.PriceTableStatus.RecentErr)
			}

			// ReadJobsStatus checks
			if worker.ReadJobsStatus.RecentErr != "" {
				return fmt.Errorf("Expected recent err to be nil but was %v", worker.ReadJobsStatus.RecentErr)
			}
			if worker.ReadJobsStatus.JobQueueSize != 0 {
				return fmt.Errorf("Expected job queue size to be 0 but was %v", worker.ReadJobsStatus.JobQueueSize)
			}
			if worker.ReadJobsStatus.ConsecutiveFailures != 0 {
				return fmt.Errorf("Expected consecutive failures to be 0 but was %v", worker.ReadJobsStatus.ConsecutiveFailures)
			}

			// HasSectorJobStatus checks
			if worker.HasSectorJobsStatus.RecentErr != "" {
				return fmt.Errorf("Expected recent err to be nil but was %v", worker.HasSectorJobsStatus.RecentErr)
			}
			if worker.HasSectorJobsStatus.JobQueueSize != 0 {
				return fmt.Errorf("Expected job queue size to be 0 but was %v", worker.HasSectorJobsStatus.JobQueueSize)
			}
			if worker.HasSectorJobsStatus.ConsecutiveFailures != 0 {
				return fmt.Errorf("Expected consecutive failures to be 0 but was %v", worker.HasSectorJobsStatus.ConsecutiveFailures)
			}

			// ReadRegistryJobStatus checks
			if worker.ReadRegistryJobsStatus.RecentErr != "" {
				return fmt.Errorf("Expected recent err to be nil but was %v", worker.ReadRegistryJobsStatus.RecentErr)
			}
			if worker.ReadRegistryJobsStatus.JobQueueSize != 0 {
				return fmt.Errorf("Expected job queue size to be 0 but was %v", worker.ReadRegistryJobsStatus.JobQueueSize)
			}
			if worker.ReadRegistryJobsStatus.ConsecutiveFailures != 0 {
				return fmt.Errorf("Expected consecutive failures to be 0 but was %v", worker.ReadRegistryJobsStatus.ConsecutiveFailures)
			}

			// UpdateRegistryJobStatus checks
			if worker.UpdateRegistryJobsStatus.RecentErr != "" {
				return fmt.Errorf("Expected recent err to be nil but was %v", worker.UpdateRegistryJobsStatus.RecentErr)
			}
			if worker.UpdateRegistryJobsStatus.JobQueueSize != 0 {
				return fmt.Errorf("Expected job queue size to be 0 but was %v", worker.UpdateRegistryJobsStatus.JobQueueSize)
			}
			if worker.UpdateRegistryJobsStatus.ConsecutiveFailures != 0 {
				return fmt.Errorf("Expected consecutive failures to be 0 but was %v", worker.UpdateRegistryJobsStatus.ConsecutiveFailures)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestWorkerSyncBalanceWithHost verifies the renter will sync its
// account balance with the host's account balance after it experienced an
// unclean shutdown.
//
// Note: this test purposefully uses its own testgroup to avoid NDFs
func TestWorkerSyncBalanceWithHost(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create a testgroup without a renter and with only 3 hosts
	groupParams := uplotest.GroupParams{
		Hosts:  3,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// add a renter with a dependency that simulates an unclean shutdown by
	// preventing accounts to be saved and also prevents the snapshot syncing
	// thread from running. That way we won't experience unexpected withdrawals
	// or refunds.
	renterParams := node.Renter(filepath.Join(testDir, "renter"))
	renterParams.RenterDeps = &dependencies.DependencyNoSnapshotSyncInterruptAccountSaveOnShutdown{}

	// add a host with a dependency that alters the deposit amount, in a way not
	// noticeable to the renter until he asks for his balance, this is necessary
	// as only then we can ensure the unclean shutdown took place and we synced
	// to the host balance
	hostParams := node.Host(filepath.Join(testDir, "host"))
	hostParams.HostDeps = &dependencies.HostLowerDeposit{}
	nodes, err := tg.AddNodes(renterParams, hostParams)
	if err != nil {
		t.Fatal(err)
	}

	// grab the nodes we just added
	var r, h *uplotest.TestNode
	if strings.HasSuffix(nodes[0].Dir, "renter") {
		r = nodes[0]
		h = nodes[1]
	} else {
		r = nodes[1]
		h = nodes[0]
	}

	// grab the hostkey
	hpk, err := h.HostPublicKey()
	if err != nil {
		t.Fatal(err)
	}

	// create a function that filters worker statuses to return the status of
	// our custom host
	worker := func(w []modules.WorkerStatus) (modules.WorkerStatus, bool) {
		for _, worker := range w {
			if worker.HostPubKey.Equals(hpk) {
				return worker, true
			}
		}
		return modules.WorkerStatus{}, false
	}

	// allow some time for the worker to be added to the worker pool and fund
	// ephemeral account, remember this balance value as the renter's version of
	// the balance
	var renterBalance types.Currency
	err = build.Retry(300, 100*time.Millisecond, func() error {
		rwg, err := r.RenterWorkersGet()
		if err != nil {
			return err
		}
		w, found := worker(rwg.Workers)
		if !found {
			return errors.New("worker not in worker pool yet")
		}
		if w.AccountStatus.AvailableBalance.IsZero() {
			return errors.New("expected worker to have a funded account, instead its balance is still 0")
		}
		renterBalance = w.AccountStatus.AvailableBalance
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// restart the renter
	err = tg.RestartNode(r)
	if err != nil {
		t.Fatal(err)
	}

	// get the worker status
	rwg, err := r.RenterWorkersGet()
	if err != nil {
		t.Fatal(err)
	}

	// grab the balance of the worker, this should have been synced to use the
	// host's version of the balance
	w, found := worker(rwg.Workers)
	if !found {
		t.Fatal("Expected worker to be found")
	}
	if w.AccountStatus.AvailableBalance.IsZero() {
		t.Fatal("Expected the renter to have synced its balance to the host's version of the balance")
	}

	// safety check to avoid panic on sub later
	if w.AccountStatus.AvailableBalance.Cmp(renterBalance) >= 0 {
		t.Fatal("Expected the synced balance to be lower, as the 'lower deposit' dependency should have deposited less", w.AccountStatus.AvailableBalance, renterBalance)
	}
	delta := types.UplocoinPrecision.Div64(10)
	if renterBalance.Sub(w.AccountStatus.AvailableBalance).Cmp(delta) < 0 {
		t.Fatalf("Expected the synced balance to be at least %v lower than the renter balance, as thats the amount we subtracted from the deposit amount, instead synced balance was %v and renter balance was %v", delta, w.AccountStatus.AvailableBalance, renterBalance)
	}
}

// TestReadSectorOutputCorrupted verifies that the merkle proof check on the
// ReadSector MDM instruction works as expected.
func TestReadSectorOutputCorrupted(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create a testgroup with a renter and miner.
	groupParams := uplotest.GroupParams{
		Miners:  1,
		Renters: 1,
	}

	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// add a host that corrupts downloads.
	deps1 := dependencies.NewDependencyCorruptMDMOutput()
	deps2 := dependencies.NewDependencyCorruptMDMOutput()
	hostParams1 := node.Host(filepath.Join(testDir, "host1"))
	hostParams2 := node.Host(filepath.Join(testDir, "host2"))
	hostParams1.HostDeps = deps1
	hostParams2.HostDeps = deps2
	_, err = tg.AddNodes(hostParams1, hostParams2)
	if err != nil {
		t.Fatal(err)
	}

	// Upload a file.
	renter := tg.Renters()[0]
	skylink, _, _, err := renter.UploadNewSkyfileBlocking("test", 100, false)
	if err != nil {
		t.Fatal(err)
	}

	// Download the file.
	_, _, err = renter.SkynetSkylinkGet(skylink)
	if err != nil {
		t.Fatal(err)
	}

	// Enable the dependencies and download again.
	deps1.Fail()
	deps2.Fail()
	_, _, err = renter.SkynetSkylinkGet(skylink)
	if err == nil || !strings.Contains(err.Error(), "all workers failed") {
		t.Fatal(err)
	}

	// Download one more time. It should work again. Do it in a loop since the
	// workers might be on a cooldown.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		_, _, err = renter.SkynetSkylinkGet(skylink)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRenterPricesVolatility verifies that the renter caches its price
// estimation, and subsequent calls result in non-volatile results.
func TestRenterPricesVolatility(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create a testgroup with PriceEstimationScope hosts.
	groupParams := uplotest.GroupParams{
		Miners:  1,
		Hosts:   modules.PriceEstimationScope,
		Renters: 1,
	}

	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	renter := tg.Renters()[0]
	host := tg.Hosts()[0]

	// Get initial estimate.
	allowance := modules.Allowance{}
	rpg, err := renter.RenterPricesGet(allowance)
	if err != nil {
		t.Fatal(err)
	}
	initial := rpg.RenterPriceEstimation

	// Changing the contract price should be enough to trigger a change
	// if the hosts are not cached.
	hg, err := host.HostGet()
	if err != nil {
		t.Fatal(err)
	}
	mcp := hg.InternalSettings.MinContractPrice
	err = host.HostModifySettingPost(client.HostParamMinContractPrice, mcp.Mul64(2))
	if err != nil {
		t.Fatal(err)
	}

	// Get the estimate again.
	rpg, err = renter.RenterPricesGet(allowance)
	if err != nil {
		t.Fatal(err)
	}
	after := rpg.RenterPriceEstimation

	// Initial and After should be the same.
	if !reflect.DeepEqual(initial, after) {
		initialJSON, _ := json.MarshalIndent(initial, "", "\t")
		afterJSON, _ := json.MarshalIndent(after, "", "\t")
		t.Log("Initial:", string(initialJSON))
		t.Log("After:", string(afterJSON))
		t.Fatal("expected renter price estimation to be constant")
	}
}

// TestRenterPricesVolatility verifies that the renter caches its price
// estimation, and subsequent calls result in non-volatile results.
func TestRenterLimitGFUContracts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create a testgroup with PriceEstimationScope hosts.
	groupParams := uplotest.GroupParams{
		Miners:  1,
		Hosts:   int(uplotest.DefaultAllowance.Hosts),
		Renters: 1,
	}

	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	renter := tg.Renters()[0]

	// Helper to check the number of GFU contracts.
	test := func(hosts uint64, portalMode bool) error {
		// Update the allowance.
		allowance := uplotest.DefaultAllowance
		allowance.Hosts = hosts
		if portalMode {
			allowance.PaymentContractInitialFunding = types.UplocoinPrecision
		}
		err := renter.RenterPostAllowance(allowance)
		if err != nil {
			t.Fatal(err)
		}
		// Wait for the number of hosts to match allowance.
		retries := 0
		return build.Retry(100, 100*time.Millisecond, func() error {
			if retries%10 == 0 {
				err := tg.Miners()[0].MineBlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			retries++

			rcg, err := renter.RenterAllContractsGet()
			if err != nil {
				t.Fatal(err)
			}

			gfuContracts := uint64(0)
			for _, contract := range rcg.ActiveContracts {
				if contract.GoodForUpload {
					gfuContracts++
				}
			}
			if gfuContracts != hosts && !portalMode {
				return fmt.Errorf("expected %v contracts but got %v", hosts, gfuContracts)
			} else if gfuContracts != uint64(len(tg.Hosts())) && portalMode {
				return fmt.Errorf("expected %v contracts but got %v", hosts, gfuContracts)
			}
			return nil
		})
	}

	// Run for default allowance and then one less every time until we reach 0
	// hosts.
	for hosts := uplotest.DefaultAllowance.Hosts; hosts > 0; hosts-- {
		if err := test(hosts, false); err != nil {
			t.Fatal(err)
		}
	}

	// Run for portal.
	if err := test(1, true); err != nil {
		t.Fatal(err)
	}
}

// TestRenterClean tests the /renter/clean endpoint functionality.
func TestRenterClean(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create test group
	groupParams := uplotest.GroupParams{
		Miners:  1,
		Hosts:   1,
		Renters: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	r := tg.Renters()[0]
	numHosts := len(tg.Hosts())

	// Upload 2 UploFiles then delete one of the UploFile's localFile so that it
	// will appear as unrecoverable.
	//
	// We use datapieces > numHosts to ensure the redundancy for both files will
	// be < 1.
	dp := uint64(numHosts + 1)
	pp := dp + 1
	lf, _, err := r.UploadNewFile(100, dp, pp, false)
	if err != nil {
		t.Fatal(err)
	}
	err = lf.Delete()
	if err != nil {
		t.Fatal(err)
	}
	_, rf2, err := r.UploadNewFile(100, dp, pp, false)
	if err != nil {
		t.Fatal(err)
	}

	// Upload a SkyFile.
	//
	// Since it doesn't have a local file it will appear as unrecoverable if the
	// hosts are taken down.
	data := fastrand.Bytes(100)
	_, _, _, rf3, err := r.UploadSkyfileCustom("skyfile", data, "", 2, false)
	if err != nil {
		t.Fatal(err)
	}

	// Define test function
	cleanAndVerify := func(numUploFiles, numSkyFiles int) {
		// Clean renter
		err = r.RenterCleanPost()
		if err != nil {
			t.Fatal(err)
		}

		// Check for the expected UploFiles
		rds, err := r.RenterDirRootGet(modules.UserFolder)
		if err != nil {
			t.Fatal(err)
		}
		if len(rds.Files) != numUploFiles {
			t.Fatal("unexpected number of files in user folder:", len(rds.Files))
		}
		// The file should be the 2nd uplofile uploaded.
		uploPath := rds.Files[0].UploPath
		expected, err := modules.UserFolder.Join(rf2.UploPath().String())
		if err != nil {
			t.Fatal(err)
		}
		if !uploPath.Equals(expected) {
			t.Fatalf("unexpected uplopath; expected %v got %v", expected, uploPath)
		}

		// Check for the expected SkyFiles
		rds, err = r.RenterDirRootGet(modules.SkynetFolder)
		if err != nil {
			t.Fatal(err)
		}
		if len(rds.Files) != numSkyFiles {
			t.Fatal("unexpected number of files in skynet folder:", len(rds.Files))
		}
	}

	// First test should only remove the 1 unrecoverable Uplofile
	cleanAndVerify(1, 1)

	// Take down the hosts
	for _, h := range tg.Hosts() {
		err = tg.StopNode(h)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Make sure the redundancy of the Skyfile drops
	err = r.WaitForDecreasingRedundancy(rf3, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Second test should remove the now unrecoverable Skyfile
	cleanAndVerify(1, 0)
}

// TestRenterRepairSize test the RepairSize field of the metadata
func TestRenterRepairSize(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a group for testing
	groupParams := uplotest.GroupParams{
		Hosts:  6,
		Miners: 1,
	}
	testDir := renterTestDir(t.Name())
	tg, err := uplotest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group:", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Add renter with dependency
	renterParams := node.RenterTemplate
	renterParams.RenterDeps = &dependencies.DependencyIgnoreFailedRepairs{}
	_, err = tg.AddNodes(renterParams)
	if err != nil {
		t.Fatal("Failed to add renter", err)
	}

	// Grab renter
	r := tg.Renters()[0]

	// Define helper
	m := tg.Miners()[0]
	checkRepairSize := func(dirUploPath modules.UploPath, repairExpected, stuckExpected uint64) error {
		return build.Retry(15, time.Second, func() error {
			// Mine a block to make sure contracts are being updated for hosts.
			if err := m.MineBlock(); err != nil {
				return err
			}
			// Grab renter's root directory
			dis, err := r.RenterDirRootGet(modules.RootUploPath())
			if err != nil {
				return err
			}
			// Check repair totals for root. Since there is no file in root the
			// directory values should always be zero and the aggregate values should
			// match the input.
			dir := dis.Directories[0]
			var rootErrs error
			if dir.AggregateRepairSize != repairExpected {
				rootErrs = errors.Compose(rootErrs, fmt.Errorf("Root: AggregateRepairSize should be %v but was %v", repairExpected, dir.AggregateRepairSize))
			}
			if dir.AggregateStuckSize != stuckExpected {
				rootErrs = errors.Compose(rootErrs, fmt.Errorf("Root: AggregateStuckSize should be %v but was %v", stuckExpected, dir.AggregateStuckSize))
			}
			if dir.RepairSize != 0 {
				rootErrs = errors.Compose(rootErrs, fmt.Errorf("Root: RepairSize should be %v but was %v", 0, dir.RepairSize))
			}
			if dir.StuckSize != 0 {
				rootErrs = errors.Compose(rootErrs, fmt.Errorf("Root: StuckSize should be %v but was %v", 0, dir.StuckSize))
			}
			// If the passed in dirUploPath is also root then return
			if dirUploPath.IsRoot() {
				return rootErrs
			}

			// Grab renter's uplofile's directory
			dis, err = r.RenterDirRootGet(dirUploPath)
			if err != nil {
				return errors.Compose(err, rootErrs)
			}
			// Check repair totals. The Aggregate and Directory values should be the
			// same.
			dir = dis.Directories[0]
			var dirErrs error
			if dir.AggregateRepairSize != repairExpected {
				dirErrs = errors.Compose(dirErrs, fmt.Errorf("%v: AggregateRepairSize should be %v but was %v", dirUploPath, repairExpected, dir.AggregateRepairSize))
			}
			if dir.AggregateStuckSize != stuckExpected {
				dirErrs = errors.Compose(dirErrs, fmt.Errorf("%v: AggregateStuckSize should be %v but was %v", dirUploPath, stuckExpected, dir.AggregateStuckSize))
			}
			if dir.RepairSize != repairExpected {
				dirErrs = errors.Compose(dirErrs, fmt.Errorf("%v: RepairSize should be %v but was %v", dirUploPath, repairExpected, dir.RepairSize))
			}
			if dir.StuckSize != stuckExpected {
				dirErrs = errors.Compose(dirErrs, fmt.Errorf("%v: StuckSize should be %v but was %v", dirUploPath, stuckExpected, dir.StuckSize))
			}
			return errors.Compose(rootErrs, dirErrs)
		})
	}

	// Renter root directory should show 0 repair bytes needed
	if err := checkRepairSize(modules.RootUploPath(), 0, 0); err != nil {
		t.Log("Initial Check Failed")
		t.Error(err)
	}

	// Upload a file
	dp := 1
	pp := len(tg.Hosts()) - dp
	_, rf, err := r.UploadNewFileBlocking(100, uint64(dp), uint64(pp), false)
	if err != nil {
		t.Fatal(err)
	}
	dirUploPath, err := rf.UploPath().Dir()
	if err != nil {
		t.Fatal(err)
	}

	// Renter root directory should show 0 repair bytes needed
	if err := checkRepairSize(dirUploPath, 0, 0); err != nil {
		t.Log("After Upload Check Failed")
		t.Error(err)
	}

	// Take down one host
	hosts := tg.Hosts()
	host := hosts[0]
	if err := tg.StopNode(host); err != nil {
		t.Fatal(err)
	}

	// Mark as stuck
	err = r.RenterSetFileStuckPost(rf.UploPath(), false, true)
	if err != nil {
		t.Fatal(err)
	}

	// Since the file is marked as stuck it should register that stuck repair
	expected := modules.SectorSize
	if err := checkRepairSize(dirUploPath, 0, expected); err != nil {
		t.Log("First host stuck check failed")
		t.Error(err)
	}

	// Mark as not stuck
	err = r.RenterSetFileStuckPost(rf.UploPath(), false, false)
	if err != nil {
		t.Fatal(err)
	}

	// With only one host taken down there should be no repair needed since the
	// file won't be seen as needing repair
	if err := checkRepairSize(dirUploPath, 0, 0); err != nil {
		t.Log("First host check failed")
		t.Error(err)
	}

	// Take down the rest of the hosts one by one and verify the repair values are dropping.
	hosts = hosts[1:]
	for i, host := range hosts {
		// Stop the host
		if err := tg.StopNode(host); err != nil {
			t.Fatal(err)
		}

		// Check that the aggregate repair size increases.
		expected += modules.SectorSize
		if err := checkRepairSize(dirUploPath, expected, 0); err != nil {
			t.Log("Host loop failed", i)
			t.Error(err)
		}
	}

	// Mark as stuck again
	err = r.RenterSetFileStuckPost(rf.UploPath(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRepairSize(dirUploPath, 0, expected); err != nil {
		t.Log("Final stuck check failed")
		t.Error(err)
	}
}

// TestMemoryStatus checks the renter reported memory status against the
// expected defaults.
func TestMemoryStatus(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	testDir := renterTestDir(t.Name())
	r, err := uplotest.NewCleanNode(node.Renter(testDir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	ud := modules.MemoryManagerStatus{
		Available: 1 << 17, // 128 KiB
		Base:      1 << 17, // 128 KiB
		Requested: 0,

		PriorityAvailable: 1 << 17, // 128 KiB
		PriorityBase:      1 << 17, // 128 KiB
		PriorityRequested: 0,
		PriorityReserve:   0,
	}
	uu := modules.MemoryManagerStatus{
		Available: 1 << 17, // 128 KiB
		Base:      1 << 17, // 128 KiB
		Requested: 0,

		PriorityAvailable: 1 << 17, // 128 KiB
		PriorityBase:      1 << 17, // 128 KiB
		PriorityRequested: 0,
		PriorityReserve:   0,
	}
	reg := modules.MemoryManagerStatus{
		Available: 1 << 17, // 128 KiB
		Base:      1 << 17, // 128 KiB
		Requested: 0,

		PriorityAvailable: 1 << 17, // 128 KiB
		PriorityBase:      1 << 17, // 128 KiB
		PriorityRequested: 0,
		PriorityReserve:   0,
	}
	sys := modules.MemoryManagerStatus{
		Available: 3 << 15, // 96 KiB
		Base:      3 << 15, // 96 KiB
		Requested: 0,

		PriorityAvailable: 1 << 17, // 128 KiB
		PriorityBase:      1 << 17, // 128 KiB
		PriorityRequested: 0,
		PriorityReserve:   1 << 15, // 32 KiB
	}
	total := ud.Add(uu).Add(reg).Add(sys)

	// Check response.
	rg, err := r.RenterGet()
	if err != nil {
		t.Fatal(err)
	}
	ms := rg.MemoryStatus
	if !reflect.DeepEqual(ms.UserDownload, ud) {
		uplotest.PrintJSON(ms.UserDownload)
		uplotest.PrintJSON(ud)
		t.Fatal("ud")
	}
	if !reflect.DeepEqual(ms.UserUpload, uu) {
		uplotest.PrintJSON(ms.UserUpload)
		uplotest.PrintJSON(uu)
		t.Fatal("uu")
	}
	if !reflect.DeepEqual(ms.Registry, reg) {
		uplotest.PrintJSON(ms.Registry)
		uplotest.PrintJSON(reg)
		t.Fatal("reg")
	}
	if !reflect.DeepEqual(ms.System, sys) {
		uplotest.PrintJSON(ms.System)
		uplotest.PrintJSON(sys)
		t.Fatal("sys")
	}
	if !reflect.DeepEqual(ms.MemoryManagerStatus, total) {
		uplotest.PrintJSON(ms.MemoryManagerStatus)
		uplotest.PrintJSON(total)
		t.Fatal("total")
	}
}
