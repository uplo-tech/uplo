package renter

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplodir"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplofile"
)

// bubbleStatus indicates the status of a bubble being executed on a
// directory
type bubbleStatus int

// bubbleduplodirMetadata is a wrapper for uplodir.Metadata that also contains the
// uplopath for convenience.
type bubbleduplodirMetadata struct {
	sp modules.UploPath
	uplodir.Metadata
}

// bubbledUploFileMetadata is a wrapper for uplofile.BubbledMetadata that also
// contains the uplopath for convenience.
type bubbledUploFileMetadata struct {
	sp modules.UploPath
	bm uplofile.BubbledMetadata
}

// bubbleError, bubbleInit, bubbleActive, and bubblePending are the constants
// used to determine the status of a bubble being executed on a directory
const (
	bubbleError bubbleStatus = iota
	bubbleActive
	bubblePending
)

// managedPrepareBubble will add a bubble to the bubble map. If 'true' is returned, the
// caller should proceed by calling bubble. If 'false' is returned, the caller
// should not bubble, another thread will handle running the bubble.
func (r *Renter) managedPrepareBubble(uploPath modules.UploPath) bool {
	r.bubbleUpdatesMu.Lock()
	defer r.bubbleUpdatesMu.Unlock()

	// Check for bubble in bubbleUpdate map
	uploPathStr := uploPath.String()
	status, ok := r.bubbleUpdates[uploPathStr]
	if !ok {
		r.bubbleUpdates[uploPathStr] = bubbleActive
		return true
	}
	if status != bubbleActive && status != bubblePending {
		build.Critical("bubble status set to bubbleError")
	}
	r.bubbleUpdates[uploPathStr] = bubblePending
	return false
}

// managedCalculateDirectoryMetadata calculates the new values for the
// directory's metadata and tracks the value, either worst or best, for each to
// be bubbled up
func (r *Renter) managedCalculateDirectoryMetadata(uploPath modules.UploPath) (uplodir.Metadata, error) {
	// Set default metadata values to start
	now := time.Now()
	metadata := uplodir.Metadata{
		AggregateHealth:              uplodir.DefaultDirHealth,
		AggregateLastHealthCheckTime: now,
		AggregateMinRedundancy:       math.MaxFloat64,
		AggregateModTime:             time.Time{},
		AggregateNumFiles:            uint64(0),
		AggregateNumStuckChunks:      uint64(0),
		AggregateNumSubDirs:          uint64(0),
		AggregateRemoteHealth:        uplodir.DefaultDirHealth,
		AggregateRepairSize:          uint64(0),
		AggregateSize:                uint64(0),
		AggregateStuckHealth:         uplodir.DefaultDirHealth,
		AggregateStuckSize:           uint64(0),

		AggregateSkynetFiles: uint64(0),
		AggregateSkynetSize:  uint64(0),

		Health:              uplodir.DefaultDirHealth,
		LastHealthCheckTime: now,
		MinRedundancy:       math.MaxFloat64,
		ModTime:             time.Time{},
		NumFiles:            uint64(0),
		NumStuckChunks:      uint64(0),
		NumSubDirs:          uint64(0),
		RemoteHealth:        uplodir.DefaultDirHealth,
		RepairSize:          uint64(0),
		Size:                uint64(0),
		StuckHealth:         uplodir.DefaultDirHealth,
		StuckSize:           uint64(0),

		SkynetFiles: uint64(0),
		SkynetSize:  uint64(0),
	}
	// Read directory
	fileinfos, err := r.staticFileSystem.ReadDir(uploPath)
	if err != nil {
		r.log.Printf("WARN: Error in reading files in directory %v : %v\n", uploPath.String(), err)
		return uplodir.Metadata{}, err
	}

	// Iterate over directory and collect the file and dir uplopaths.
	var fileUploPaths, dirUploPaths []modules.UploPath
	for _, fi := range fileinfos {
		// Check to make sure renter hasn't been shutdown
		select {
		case <-r.tg.StopChan():
			return uplodir.Metadata{}, err
		default:
		}
		// Sort by file and dirs.
		ext := filepath.Ext(fi.Name())
		if ext == modules.UploFileExtension {
			// UploFile found.
			fName := strings.TrimSuffix(fi.Name(), modules.UploFileExtension)
			fileUploPath, err := uploPath.Join(fName)
			if err != nil {
				r.log.Println("unable to join uplopath with dirpath while calculating directory metadata:", err)
				continue
			}
			fileUploPaths = append(fileUploPaths, fileUploPath)
		} else if fi.IsDir() {
			// Directory is found, read the directory metadata file
			dirUploPath, err := uploPath.Join(fi.Name())
			if err != nil {
				r.log.Println("unable to join uplopath with dirpath while calculating directory metadata:", err)
				continue
			}
			dirUploPaths = append(dirUploPaths, dirUploPath)
		}
	}

	// Calculate the Files' bubbleMetadata first.
	// Note: We don't need to abort on error. It's likely that only one or a few
	// files failed and that the remaining metadatas are good to use.
	bubbledMetadatas, err := r.managedCalculateFileMetadatas(fileUploPaths)
	if err != nil {
		r.log.Printf("failed to calculate file metadata: %v", err)
	}

	// Get all the Directory Metadata
	// Note: We don't need to abort on error. It's likely that only one or a few
	// directories failed and that the remaining metadatas are good to use.
	dirMetadatas, err := r.managedDirectoryMetadatas(dirUploPaths)
	if err != nil {
		r.log.Printf("failed to calculate file metadata: %v", err)
	}

	for len(bubbledMetadatas)+len(dirMetadatas) > 0 {
		// Aggregate Fields
		var aggregateHealth, aggregateRemoteHealth, aggregateStuckHealth, aggregateMinRedundancy float64
		var aggregateLastHealthCheckTime, aggregateModTime time.Time
		if len(bubbledMetadatas) > 0 {
			// Get next file's metadata.
			bubbledMetadata := bubbledMetadatas[0]
			bubbledMetadatas = bubbledMetadatas[1:]
			fileUploPath := bubbledMetadata.sp
			fileMetadata := bubbledMetadata.bm
			// If 75% or more of the redundancy is missing, register an alert
			// for the file.
			uid := string(fileMetadata.UID)
			if maxHealth := math.Max(fileMetadata.Health, fileMetadata.StuckHealth); maxHealth >= AlertUplofileLowRedundancyThreshold {
				r.staticAlerter.RegisterAlert(modules.AlertIDUplofileLowRedundancy(uid), AlertMSGUplofileLowRedundancy,
					AlertCauseUplofileLowRedundancy(fileUploPath, maxHealth, fileMetadata.Redundancy),
					modules.SeverityWarning)
			} else {
				r.staticAlerter.UnregisterAlert(modules.AlertIDUplofileLowRedundancy(uid))
			}

			// If the file's LastHealthCheckTime is still zero, set it as now since it
			// it currently being checked.
			//
			// The LastHealthCheckTime is not a field that is initialized when a file
			// is created, so we can reach this point by one of two ways. If a file is
			// created in the directory after the health loop has decided it needs to
			// be bubbled, or a file is created in a directory that gets a bubble
			// called on it outside of the health loop before the health loop as been
			// able to set the LastHealthCheckTime.
			if fileMetadata.LastHealthCheckTime.IsZero() {
				fileMetadata.LastHealthCheckTime = time.Now()
			}

			// Update repair fields
			metadata.AggregateRepairSize += fileMetadata.RepairBytes
			metadata.AggregateStuckSize += fileMetadata.StuckBytes
			metadata.RepairSize += fileMetadata.RepairBytes
			metadata.StuckSize += fileMetadata.StuckBytes

			// Record Values that compare against sub directories
			aggregateHealth = fileMetadata.Health
			aggregateStuckHealth = fileMetadata.StuckHealth
			aggregateMinRedundancy = fileMetadata.Redundancy
			aggregateLastHealthCheckTime = fileMetadata.LastHealthCheckTime
			aggregateModTime = fileMetadata.ModTime
			if !fileMetadata.OnDisk {
				aggregateRemoteHealth = fileMetadata.Health
			}

			// Update aggregate fields.
			metadata.AggregateNumFiles++
			metadata.AggregateNumStuckChunks += fileMetadata.NumStuckChunks
			metadata.AggregateSize += fileMetadata.Size

			// Update uplodir fields.
			metadata.Health = math.Max(metadata.Health, fileMetadata.Health)
			if fileMetadata.LastHealthCheckTime.Before(metadata.LastHealthCheckTime) {
				metadata.LastHealthCheckTime = fileMetadata.LastHealthCheckTime
			}
			if fileMetadata.Redundancy != -1 {
				metadata.MinRedundancy = math.Min(metadata.MinRedundancy, fileMetadata.Redundancy)
			}
			if fileMetadata.ModTime.After(metadata.ModTime) {
				metadata.ModTime = fileMetadata.ModTime
			}
			metadata.NumFiles++
			metadata.NumStuckChunks += fileMetadata.NumStuckChunks
			if !fileMetadata.OnDisk {
				metadata.RemoteHealth = math.Max(metadata.RemoteHealth, fileMetadata.Health)
			}
			metadata.Size += fileMetadata.Size
			metadata.StuckHealth = math.Max(metadata.StuckHealth, fileMetadata.StuckHealth)

			// Update Skynet Fields
			//
			// If the current directory is under the Skynet Folder, or the uplofile
			// contains a skylink in the metadata, then we count the file towards the
			// Skynet Stats.
			//
			// For all cases we count the size.
			//
			// We only count the file towards the number of files if it is in the
			// skynet folder and is not extended. We do not count files outside of the
			// skynet folder because they should be treated as an extended file.
			isSkynetDir := strings.Contains(uploPath.String(), modules.SkynetFolder.String())
			isExtended := strings.Contains(fileUploPath.String(), modules.ExtendedSuffix)
			hasSkylinks := fileMetadata.NumSkylinks > 0
			if isSkynetDir || hasSkylinks {
				metadata.AggregateSkynetSize += fileMetadata.Size
				metadata.SkynetSize += fileMetadata.Size
			}
			if isSkynetDir && !isExtended {
				metadata.AggregateSkynetFiles++
				metadata.SkynetFiles++
			}
		} else if len(dirMetadatas) > 0 {
			// Get next dir's metadata.
			dirMetadata := dirMetadatas[0]
			dirMetadatas = dirMetadatas[1:]

			// Check if the directory's AggregateLastHealthCheckTime is Zero. If so
			// set the time to now and call bubble on that directory to try and fix
			// the directories metadata.
			//
			// The LastHealthCheckTime is not a field that is initialized when
			// a directory is created, so we can reach this point if a directory is
			// created and gets a bubble called on it outside of the health loop
			// before the health loop has been able to set the LastHealthCheckTime.
			if dirMetadata.AggregateLastHealthCheckTime.IsZero() {
				dirMetadata.AggregateLastHealthCheckTime = time.Now()
				err = r.tg.Launch(func() {
					r.callThreadedBubbleMetadata(dirMetadata.sp)
				})
				if err != nil {
					r.log.Printf("WARN: unable to launch bubble for '%v'", dirMetadata.sp)
				}
			}

			// Record Values that compare against files
			aggregateHealth = dirMetadata.AggregateHealth
			aggregateStuckHealth = dirMetadata.AggregateStuckHealth
			aggregateMinRedundancy = dirMetadata.AggregateMinRedundancy
			aggregateLastHealthCheckTime = dirMetadata.AggregateLastHealthCheckTime
			aggregateModTime = dirMetadata.AggregateModTime
			aggregateRemoteHealth = dirMetadata.AggregateRemoteHealth

			// Update aggregate fields.
			metadata.AggregateNumFiles += dirMetadata.AggregateNumFiles
			metadata.AggregateNumStuckChunks += dirMetadata.AggregateNumStuckChunks
			metadata.AggregateNumSubDirs += dirMetadata.AggregateNumSubDirs
			metadata.AggregateRepairSize += dirMetadata.AggregateRepairSize
			metadata.AggregateSize += dirMetadata.AggregateSize
			metadata.AggregateStuckSize += dirMetadata.AggregateStuckSize

			// Update aggregate Skynet fields
			metadata.AggregateSkynetFiles += dirMetadata.AggregateSkynetFiles
			metadata.AggregateSkynetSize += dirMetadata.AggregateSkynetSize

			// Add 1 to the AggregateNumSubDirs to account for this subdirectory.
			metadata.AggregateNumSubDirs++

			// Update uplodir fields
			metadata.NumSubDirs++
		}
		// Track the max value of aggregate health values
		metadata.AggregateHealth = math.Max(metadata.AggregateHealth, aggregateHealth)
		metadata.AggregateRemoteHealth = math.Max(metadata.AggregateRemoteHealth, aggregateRemoteHealth)
		metadata.AggregateStuckHealth = math.Max(metadata.AggregateStuckHealth, aggregateStuckHealth)
		// Track the min value for AggregateMinRedundancy
		if aggregateMinRedundancy != -1 {
			metadata.AggregateMinRedundancy = math.Min(metadata.AggregateMinRedundancy, aggregateMinRedundancy)
		}
		// Update LastHealthCheckTime
		if aggregateLastHealthCheckTime.Before(metadata.AggregateLastHealthCheckTime) {
			metadata.AggregateLastHealthCheckTime = aggregateLastHealthCheckTime
		}
		// Update ModTime
		if aggregateModTime.After(metadata.AggregateModTime) {
			metadata.AggregateModTime = aggregateModTime
		}
	}

	// Sanity check on ModTime. If mod time is still zero it means there were no
	// files or subdirectories. Set ModTime to now since we just updated this
	// directory
	if metadata.AggregateModTime.IsZero() {
		metadata.AggregateModTime = time.Now()
	}
	if metadata.ModTime.IsZero() {
		metadata.ModTime = time.Now()
	}
	// Sanity check on Redundancy. If MinRedundancy is still math.MaxFloat64
	// then set it to -1 to indicate an empty directory
	if metadata.AggregateMinRedundancy == math.MaxFloat64 {
		metadata.AggregateMinRedundancy = -1
	}
	if metadata.MinRedundancy == math.MaxFloat64 {
		metadata.MinRedundancy = -1
	}

	return metadata, nil
}

// managedCalculateFileMetadata calculates and returns the necessary metadata
// information of a uplofiles that needs to be bubbled.
func (r *Renter) managedCalculateFileMetadata(uploPath modules.UploPath, hostOfflineMap, hostGoodForRenewMap map[string]bool) (bubbledUploFileMetadata, error) {
	// Open UploFile in a read only state so that it doesn't need to be
	// closed
	sf, err := r.staticFileSystem.OpenUploFile(uploPath)
	if err != nil {
		return bubbledUploFileMetadata{}, err
	}
	defer func() {
		err = errors.Compose(err, sf.Close())
	}()

	// First check if the fileNode is blocked. Blocking a file does not remove the
	// file so this is required to ensuring the node is purging blocked files.
	if r.isFileNodeBlocked(sf) {
		// Delete the file
		r.log.Println("Deleting blocked fileNode at:", uploPath)
		return bubbledUploFileMetadata{}, errors.Compose(r.staticFileSystem.DeleteFile(uploPath), ErrSkylinkBlocked)
	}

	// Calculate file health
	health, stuckHealth, _, _, numStuckChunks, repairBytes, stuckBytes := sf.Health(hostOfflineMap, hostGoodForRenewMap)

	// Calculate file Redundancy and check if local file is missing and
	// redundancy is less than one
	redundancy, _, err := sf.Redundancy(hostOfflineMap, hostGoodForRenewMap)
	if err != nil {
		return bubbledUploFileMetadata{}, err
	}
	_, err = os.Stat(sf.LocalPath())
	onDisk := err == nil
	if !onDisk && redundancy < 1 {
		r.log.Debugf("File not found on disk and possibly unrecoverable: LocalPath %v; UploPath %v", sf.LocalPath(), uploPath.String())
	}

	// Grab the number of skylinks
	numSkylinks := len(sf.Metadata().Skylinks)

	// Return the metadata
	return bubbledUploFileMetadata{
		sp: uploPath,
		bm: uplofile.BubbledMetadata{
			Health:              health,
			LastHealthCheckTime: sf.LastHealthCheckTime(),
			ModTime:             sf.ModTime(),
			NumSkylinks:         uint64(numSkylinks),
			NumStuckChunks:      numStuckChunks,
			OnDisk:              onDisk,
			Redundancy:          redundancy,
			RepairBytes:         repairBytes,
			Size:                sf.Size(),
			StuckHealth:         stuckHealth,
			StuckBytes:          stuckBytes,
			UID:                 sf.UID(),
		},
	}, nil
}

// managedCalculateFileMetadatas calculates and returns the necessary metadata
// information of multiple uplofiles that need to be bubbled. Usually the return
// value of a method is ignored when the returned error != nil. For
// managedCalculateFileMetadatas we make an exception. The caller can decide
// themselves whether to use the output in case of an error or not.
func (r *Renter) managedCalculateFileMetadatas(uploPaths []modules.UploPath) (_ []bubbledUploFileMetadata, err error) {
	/// Get cached offline and goodforrenew maps.
	hostOfflineMap, hostGoodForRenewMap, _, _ := r.managedRenterContractsAndUtilities()

	// Define components
	mds := make([]bubbledUploFileMetadata, 0, len(uploPaths))
	uploPathChan := make(chan modules.UploPath, numBubbleWorkerThreads)
	var errs error
	var errMu, mdMu sync.Mutex

	// Create function for loading UploFiles and calculating the metadata
	metadataWorker := func() {
		for uploPath := range uploPathChan {
			md, err := r.managedCalculateFileMetadata(uploPath, hostOfflineMap, hostGoodForRenewMap)
			if errors.Contains(err, ErrSkylinkBlocked) {
				// If the fileNode is blocked we ignore the error and continue.
				continue
			}
			if err != nil {
				errMu.Lock()
				errs = errors.Compose(errs, err)
				errMu.Unlock()
				continue
			}
			mdMu.Lock()
			mds = append(mds, md)
			mdMu.Unlock()
		}
	}

	// Launch Metadata workers
	var wg sync.WaitGroup
	for i := 0; i < numBubbleWorkerThreads; i++ {
		wg.Add(1)
		go func() {
			metadataWorker()
			wg.Done()
		}()
	}
	for _, uploPath := range uploPaths {
		uploPathChan <- uploPath
	}
	close(uploPathChan)
	wg.Wait()
	return mds, errs
}

// managedCompleteBubbleUpdate completes the bubble update and updates and/or
// removes it from the renter's bubbleUpdates.
//
// TODO: bubbleUpdatesMu is in violation of conventions, needs to be moved to
// its own object to have its own mu.
func (r *Renter) managedCompleteBubbleUpdate(uploPath modules.UploPath) {
	r.bubbleUpdatesMu.Lock()
	defer r.bubbleUpdatesMu.Unlock()

	// Check current status
	uploPathStr := uploPath.String()
	status, exists := r.bubbleUpdates[uploPathStr]

	// If the status is 'bubbleActive', delete the status and return.
	if status == bubbleActive {
		delete(r.bubbleUpdates, uploPathStr)
		return
	}
	// If the status is not 'bubbleActive', and the status is also not
	// 'bubblePending', this is an error. There should be a status, and it
	// should either be active or pending.
	if status != bubblePending {
		build.Critical("invalid bubble status", status, exists)
		delete(r.bubbleUpdates, uploPathStr) // Attempt to reset the corrupted state.
		return
	}
	// The status is bubblePending, switch the status to bubbleActive.
	r.bubbleUpdates[uploPathStr] = bubbleActive

	// Launch a thread to do another bubble on this directory, as there was a
	// bubble pending waiting for the current bubble to complete.
	err := r.tg.Add()
	if err != nil {
		return
	}
	go func() {
		defer r.tg.Done()
		r.managedPerformBubbleMetadata(uploPath)
	}()
}

// managedDirectoryMetadatas returns all the metadatas of the uplodirs for the
// provided uploPaths
func (r *Renter) managedDirectoryMetadatas(uploPaths []modules.UploPath) ([]bubbleduplodirMetadata, error) {
	// Define components
	mds := make([]bubbleduplodirMetadata, 0, len(uploPaths))
	uploPathChan := make(chan modules.UploPath, numBubbleWorkerThreads)
	var errs error
	var errMu, mdMu sync.Mutex

	// Create function for getting the directory metadata
	metadataWorker := func() {
		for uploPath := range uploPathChan {
			md, err := r.managedDirectoryMetadata(uploPath)
			if err != nil {
				errMu.Lock()
				errs = errors.Compose(errs, err)
				errMu.Unlock()
				continue
			}
			mdMu.Lock()
			mds = append(mds, bubbleduplodirMetadata{
				uploPath,
				md,
			})
			mdMu.Unlock()
		}
	}

	// Launch Metadata workers
	var wg sync.WaitGroup
	for i := 0; i < numBubbleWorkerThreads; i++ {
		wg.Add(1)
		go func() {
			metadataWorker()
			wg.Done()
		}()
	}
	for _, uploPath := range uploPaths {
		uploPathChan <- uploPath
	}
	close(uploPathChan)
	wg.Wait()
	return mds, errs
}

// managedDirectoryMetadata reads the directory metadata and returns the bubble
// metadata
func (r *Renter) managedDirectoryMetadata(uploPath modules.UploPath) (_ uplodir.Metadata, err error) {
	// Check for bad paths and files
	fi, err := r.staticFileSystem.Stat(uploPath)
	if err != nil {
		return uplodir.Metadata{}, err
	}
	if !fi.IsDir() {
		return uplodir.Metadata{}, fmt.Errorf("%v is not a directory", uploPath)
	}

	//  Open uplodir
	uploDir, err := r.staticFileSystem.OpenuplodirCustom(uploPath, true)
	if err != nil {
		return uplodir.Metadata{}, err
	}
	defer func() {
		err = errors.Compose(err, uploDir.Close())
	}()

	// Grab the metadata.
	return uploDir.Metadata()
}

// managedUpdateLastHealthCheckTime updates the LastHealthCheckTime and
// AggregateLastHealthCheckTime fields of the directory metadata by reading all
// the subdirs of the directory.
func (r *Renter) managedUpdateLastHealthCheckTime(uploPath modules.UploPath) error {
	// Read directory
	fileinfos, err := r.staticFileSystem.ReadDir(uploPath)
	if err != nil {
		r.log.Printf("WARN: Error in reading files in directory %v : %v\n", uploPath.String(), err)
		return err
	}

	// Iterate over directory and find the oldest AggregateLastHealthCheckTime
	aggregateLastHealthCheckTime := time.Now()
	for _, fi := range fileinfos {
		// Check to make sure renter hasn't been shutdown
		select {
		case <-r.tg.StopChan():
			return err
		default:
		}
		// Check for UploFiles and Directories
		if fi.IsDir() {
			// Directory is found, read the directory metadata file
			dirUploPath, err := uploPath.Join(fi.Name())
			if err != nil {
				return err
			}
			dirMetadata, err := r.managedDirectoryMetadata(dirUploPath)
			if err != nil {
				return err
			}
			// Update AggregateLastHealthCheckTime.
			if dirMetadata.AggregateLastHealthCheckTime.Before(aggregateLastHealthCheckTime) {
				aggregateLastHealthCheckTime = dirMetadata.AggregateLastHealthCheckTime
			}
		} else {
			// Ignore everything that is not a directory since files should be updated
			// already by the ongoing bubble.
			continue
		}
	}

	// Write changes to disk.
	entry, err := r.staticFileSystem.Openuplodir(uploPath)
	if err != nil {
		return err
	}
	err = entry.UpdateLastHealthCheckTime(aggregateLastHealthCheckTime, time.Now())
	return errors.Compose(err, entry.Close())
}

// callThreadedBubbleMetadata is the thread safe method used to call
// managedBubbleMetadata when the call does not need to be blocking
func (r *Renter) callThreadedBubbleMetadata(uploPath modules.UploPath) {
	if err := r.tg.Add(); err != nil {
		return
	}
	defer r.tg.Done()
	if err := r.managedBubbleMetadata(uploPath); err != nil {
		r.log.Debugln("WARN: error with bubbling metadata:", err)
	}
}

// managedPerformBubbleMetadata will bubble the metadata without checking the
// bubble preparation.
func (r *Renter) managedPerformBubbleMetadata(uploPath modules.UploPath) (err error) {
	// Make sure we call callThreadedBubbleMetadata on the parent once we are
	// done.
	defer func() error {
		// Complete bubble
		r.managedCompleteBubbleUpdate(uploPath)

		// Continue with parent dir if we aren't in the root dir already.
		if uploPath.IsRoot() {
			return nil
		}
		parentDir, err := uploPath.Dir()
		if err != nil {
			return errors.AddContext(err, "failed to defer callThreadedBubbleMetadata on parent dir")
		}
		go r.callThreadedBubbleMetadata(parentDir)
		return nil
	}()

	// Calculate the new metadata values of the directory
	metadata, err := r.managedCalculateDirectoryMetadata(uploPath)
	if err != nil {
		e := fmt.Sprintf("could not calculate the metadata of directory %v", uploPath.String())
		return errors.AddContext(err, e)
	}

	// Update directory metadata with the health information. Don't return here
	// to avoid skipping the repairNeeded and stuckChunkFound signals.
	uplodir, err := r.staticFileSystem.Openuplodir(uploPath)
	if err != nil {
		e := fmt.Sprintf("could not open directory %v", uploPath.String())
		err = errors.AddContext(err, e)
	} else {
		defer func() {
			err = errors.Compose(err, uplodir.Close())
		}()
		err = uplodir.UpdateBubbledMetadata(metadata)
		if err != nil {
			e := fmt.Sprintf("could not update the metadata of the directory %v", uploPath.String())
			err = errors.AddContext(err, e)
		}
	}

	// If we are at the root directory then check if any files were found in
	// need of repair or and stuck chunks and trigger the appropriate repair
	// loop. This is only done at the root directory as the repair and stuck
	// loops start at the root directory so there is no point triggering them
	// until the root directory is updated
	if uploPath.IsRoot() {
		if modules.NeedsRepair(metadata.AggregateHealth) {
			select {
			case r.uploadHeap.repairNeeded <- struct{}{}:
			default:
			}
		}
		if metadata.AggregateNumStuckChunks > 0 {
			select {
			case r.uploadHeap.stuckChunkFound <- struct{}{}:
			default:
			}
		}
	}
	return err
}

// managedBubbleMetadata calculates the updated values of a directory's metadata
// and updates the uplodir metadata on disk then calls callThreadedBubbleMetadata
// on the parent directory so that it is only blocking for the current directory
func (r *Renter) managedBubbleMetadata(uploPath modules.UploPath) error {
	// Check if bubble is needed
	proceedWithBubble := r.managedPrepareBubble(uploPath)
	if !proceedWithBubble {
		// Update the AggregateLastHealthCheckTime even if we weren't able to
		// bubble right away.
		return r.managedUpdateLastHealthCheckTime(uploPath)
	}
	return r.managedPerformBubbleMetadata(uploPath)
}
