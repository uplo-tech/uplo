package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

const (
	// The UploPath that will be used by the program to upload and store all of
	// the files when performing test downloads.
	testuplodirDL = "var/skynet-benchmark-dl"

	// A range of files of different sizes.
	dir64kb = "64kb"
	dir1mb  = "1mb"
	dir4mb  = "4mb"
	dir10mb = "10mb"

	// A range of different thread counts.
	threads1  = "1thread"
	threads4  = "4threads"
	threads16 = "16threads"
	threads64 = "64threads"

	// The exact sizes of each file. This size is chosen so that when the
	// metadata is added to the file, and then the filesize is converted to a
	// fetch size, the final fetch size is as close as possible to the filesize
	// of the dir without going over.
	exactSize64kb = 61e3
	exactSize1mb  = 982e3
	exactSize4mb  = 3931e3
	exactSize10mb = 10e6 // Once over 4 MB, fetch size doesn't matter, can use exact sizes.

	// Fetch size is the largest fetch size that can be set using the skylink
	// naming standard without exceeding the filesize.
	fetchSize64kb = 61440
	fetchSize1mb  = 983040
	fetchSize4mb  = 3932160
	fetchSize10mb = 10e6 // Once over 4 MB, fetch size doesn't matter, can use exact sizes.

	// The total number of files of each size that we download during testing.
	filesPerDir = 200
)

// dl is a command that downloads skyfiles from Skynet in various sizes, ranging
// from 64kb up until 10mb. Before it can download this function will upload
// those files in four separate directories.
func dl() {
	fmt.Println("Performing dl command")

	// Convenience variables
	dirs := []string{dir64kb, dir1mb, dir4mb, dir10mb}
	threadss := []string{threads1, threads4, threads16, threads64}
	threadssCount := map[string]uint64{threads1: 1, threads4: 4, threads16: 16, threads64: 64}

	// Establish the directories that we will be using for testing.
	dirBasePath, err := modules.NewUploPath(testuplodirDL)
	if err != nil {
		fmt.Println("Could not create uplopath for testing directory:", err)
		return
	}

	// Create a map that contains the exact file size and fetch size. The
	// filesize used is slightly smaller than the expected filesize to leave
	// room for metadata overhead. The expected filesize used is the largest
	// filesize that fits inside of the file limits for the metrics collector.
	sizes := [][]uint64{
		{exactSize64kb, fetchSize64kb},
		{exactSize1mb, fetchSize1mb},
		{exactSize4mb, fetchSize4mb},
		{exactSize10mb, fetchSize10mb},
	}

	fmt.Println("Beginning uploading test files.")

	// Keep track of the uplo paths to avoid having to recreate them later.
	paths := make(map[string]modules.UploPath)

	// Iterate over every size category and thread count we're interested in and
	// upload the file set to that corresponding uplo dir. We re-upload all files
	// to directories specific to both the size and thread count to avoid
	// serving a skyfile from (internal) cache structures at all costs.
	for i, dir := range dirs {
		for _, threads := range threadss {
			subPathStr := dir + threads
			dirPath, err := dirBasePath.Join(subPathStr)
			if err != nil {
				fmt.Printf("Could not create '%v' uplopath for testing directory, err:%v\n", subPathStr, err)
				continue
			}
			paths[subPathStr] = dirPath
			err = uploadFileSet(dirPath, sizes[i][0], sizes[i][1])
			if err != nil {
				fmt.Printf("Unable to upload %v files, err:%v", subPathStr, err)
				return
			}
			fmt.Printf("- %v files: OK\n", subPathStr)
		}
	}

	fmt.Printf("Beginning download testing.\nEach test is %v files\n\n", filesPerDir)

	for _, threads := range threadss {
		for i, dir := range dirs {
			// Create download parameters
			path := paths[dir+threads]
			numThreads := threadssCount[threads]

			// Reset timer
			start := time.Now()
			fmt.Printf("%v downloads on %v threads started\n", dir, numThreads)
			timings, err := downloadFileSet(path, int(sizes[i][0]), numThreads)
			if err != nil {
				fmt.Printf("Unable to download all %v files, err: %v\n", dir, err)
			}

			// Log result
			fmt.Printf("%v downloads on %v threads finished in %v\n", dir, numThreads, time.Since(start))
			fmt.Println(getPercentilesString(timings))
		}
	}
}

// downloadFileSet will download all of the files of the expected fetch size in
// a dir.
func downloadFileSet(dir modules.UploPath, fileSize int, threads uint64) (stats.Float64Data, error) {
	now := time.Now()

	// Create a list of timings
	timings := make([]float64, filesPerDir)

	// Create a thread pool and fill it. Need to grab a struct from the pool
	// before launching a thread, need to drop the object back into the pool
	// when done.
	threadPool := make(chan struct{}, threads)
	for i := uint64(0); i < threads; i++ {
		threadPool <- struct{}{}
	}

	// Loop over every file. Block until there is an object ready in the thread
	// pool, then launch a thread.
	var atomicDownloadsFinished uint64
	var atomicDownloadErrors uint64
	var wg sync.WaitGroup
	for i := 0; i < filesPerDir; i++ {
		// Get permission to launch a thread.
		<-threadPool

		// Launch the downloading thread.
		wg.Add(1)
		go func(i int, launched time.Time) {
			// Make room for the next thread.
			defer func() {
				threadPool <- struct{}{}
			}()
			// Clear the wait group.
			defer wg.Done()

			// Figure out the uplopath of the dir.
			uploPath, err := dir.Join(strconv.Itoa(i))
			if err != nil {
				fmt.Printf("Dir error on %v: %v\n", i, err)
				atomic.AddUint64(&atomicDownloadErrors, 1)
				return
			}
			// Figure out the skylink for the file.
			rf, err := c.RenterFileRootGet(uploPath)
			if err != nil {
				fmt.Printf("Error getting file info on %v: %v\n", i, err)
				atomic.AddUint64(&atomicDownloadErrors, 1)
				return
			}

			// Keep track of the elapsed time
			start := time.Now()

			// Get a reader / stream for the download.
			reader, err := c.SkynetSkylinkReaderGet(rf.File.Skylinks[0])
			if err != nil {
				fmt.Printf("Error getting skylink reader on %v: %v\n", i, err)
				atomic.AddUint64(&atomicDownloadErrors, 1)
				return
			}

			// Download and discard the result, we only care about the speeds,
			// not the data.
			data, err := ioutil.ReadAll(reader)
			if err != nil {
				fmt.Printf("Error performing download on %v, only got %v bytes: %v\n", i, len(data), err)
				atomic.AddUint64(&atomicDownloadErrors, 1)
				return
			}
			if len(data) != fileSize {
				fmt.Printf("Error performing download on %v, got %v bytes when expecting %v\n", i, len(data), fileSize)
				atomic.AddUint64(&atomicDownloadErrors, 1)
				return
			}

			elapsed := time.Since(start)
			timings[i] = float64(elapsed.Milliseconds())

			numFinished := atomic.AddUint64(&atomicDownloadsFinished, 1)
			numTwentyPct := uint64(filesPerDir / 5)
			if numFinished%numTwentyPct == 0 {
				fmt.Printf("%v%% finished after %vms\n", (numFinished/numTwentyPct)*20, time.Since(launched).Milliseconds())
			}
		}(i, now)
	}
	wg.Wait()

	// Don't need to use atomics, all threads have returned.
	if atomicDownloadErrors != 0 {
		return timings, fmt.Errorf("there were %v errors while downloading", atomicDownloadErrors)
	}
	return timings, nil
}

// getPercentilesString takes a set of timings and returns a bunch of percentile
// statistics.
func getPercentilesString(timings stats.Float64Data) string {
	p50, err := timings.Percentile(50)
	if err != nil {
		return err.Error()
	}
	p60, err := timings.Percentile(60)
	if err != nil {
		return err.Error()
	}
	p70, err := timings.Percentile(70)
	if err != nil {
		return err.Error()
	}
	p80, err := timings.Percentile(80)
	if err != nil {
		return err.Error()
	}
	p90, err := timings.Percentile(90)
	if err != nil {
		return err.Error()
	}
	p95, err := timings.Percentile(95)
	if err != nil {
		return err.Error()
	}
	p99, err := timings.Percentile(99)
	if err != nil {
		return err.Error()
	}
	p999, err := timings.Percentile(99.9)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("50p: %vms\n60p: %vms\n70p: %vms\n80p: %vms\n90p: %vms\n95p: %vms\n99p: %vms\n999p: %vms\n\n", p50, p60, p70, p80, p90, p95, p99, p999)
}

// getMissingFiles will fetch a map of all the files that are missing or don't
// have skylinks
func getMissingFiles(dir modules.UploPath, expectedFileSize uint64, expectedFetchSize uint64) (map[int]struct{}, error) {
	// Determine whether the dirs already exist and have files in them for
	// downloading.
	rdg, err := c.RenterDirRootGet(dir)
	if err != nil {
		// If the error is something other than a DNE, abort.
		if !strings.Contains(err.Error(), filesystem.ErrNotExist.Error()) {
			return nil, errors.AddContext(err, "could not fetch dir for missing files")
		}
	}

	missingFiles := make(map[int]struct{})
	for i := 0; i < filesPerDir; i++ {
		missingFiles[i] = struct{}{}
	}
	// Loop through the files we have.
	for _, file := range rdg.Files {
		// Check the files that are the right size.
		if !file.Available {
			continue
		}
		if len(file.Skylinks) != 1 {
			continue
		}
		var sl modules.Skylink
		err := sl.LoadString(file.Skylinks[0])
		if err != nil {
			return nil, errors.AddContext(err, "error parsing skylink in testing dir")
		}
		_, fetchSize, err := sl.OffsetAndFetchSize()
		if err != nil {
			return nil, errors.AddContext(err, "error parsing skylink offset and fetch size in testing dir")
		}
		if expectedFetchSize < 4100e3 && fetchSize != expectedFetchSize {
			continue
		} else if fetchSize >= 4100e3 && file.Filesize != expectedFetchSize {
			continue
		}
		cleanName := strings.TrimSuffix(file.UploPath.Name(), "-extended")
		num, err := strconv.Atoi(cleanName)
		if err != nil {
			continue
		}
		delete(missingFiles, num)
	}
	return missingFiles, nil
}

// uploadFileSet will upload a set of files for testing, skipping over any files
// that already exist.
func uploadFileSet(dir modules.UploPath, fileSize uint64, expectedFetchSize uint64) error {
	missingFiles, err := getMissingFiles(dir, fileSize, expectedFetchSize)
	if err != nil {
		return errors.AddContext(err, "error assembling set of missing files")
	}
	if len(missingFiles) != 0 {
		fmt.Printf("There are %v missing %v files, uploading now.\n", len(missingFiles), fileSize)
	}

	// Upload files until there are enough.
	for i := range missingFiles {
		// Get the uplopath for the file.
		sp, err := dir.Join(strconv.Itoa(i))
		if err != nil {
			return errors.AddContext(err, "error creating filename")
		}
		buf := bytes.NewReader(fastrand.Bytes(int(fileSize)))
		// Fill out the upload parameters.
		sup := modules.SkyfileUploadParameters{
			UploPath:  sp,
			Filename: strconv.Itoa(i) + ".rand",
			Mode:     modules.DefaultFilePerm,

			Root:  true,
			Force: true, // This will overwrite other files in the dir.

			Reader: buf,
		}
		// Upload the file.
		_, _, err = c.SkynetSkyfilePost(sup)
		if err != nil {
			return errors.AddContext(err, "error when attempting to upload new file")
		}
	}

	missingFiles, err = getMissingFiles(dir, fileSize, expectedFetchSize)
	if err != nil {
		return errors.AddContext(err, "error assembling set of missing files")
	}
	if len(missingFiles) > 0 {
		fmt.Println("Failed to upload all necessary files:", len(missingFiles), "did not complete")
		return errors.New("Upload appears unsuccessful")
	}
	return nil
}
