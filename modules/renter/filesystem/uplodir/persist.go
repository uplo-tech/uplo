package uplodir

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
)

const (
	// uplodirExtension is the name of the metadata file for the uplo directory
	uplodirExtension = ".uplodir"

	// DefaultDirHealth is the default health for the directory and the fall
	// back value when there is an error. This is to protect against falsely
	// trying to repair directories that had a read error
	DefaultDirHealth = float64(0)

	// DefaultDirRedundancy is the default redundancy for the directory and the
	// fall back value when there is an error. This is to protect against
	// falsely trying to repair directories that had a read error
	DefaultDirRedundancy = float64(-1)

	// updateDeleteName is the name of a uplodir update that deletes the
	// specified metadata file.
	updateDeleteName = "uplodirDelete"

	// updateMetadataName is the name of a uplodir update that inserts new
	// information into the metadata file
	updateMetadataName = "uplodirMetadata"
)

var (
	// ErrDeleted is the error returned if the uplodir is deleted
	ErrDeleted = errors.New("uplodir is deleted")
)

// ApplyUpdates  applies a number of writeaheadlog updates to the corresponding
// uplodir. This method can apply updates from different uplodirs and should only
// be run before the uplodirs are loaded from disk right after the startup of
// uplod. Otherwise we might run into concurrency issues.
func ApplyUpdates(updates ...writeaheadlog.Update) error {
	// Apply updates.
	for _, u := range updates {
		err := applyUpdate(modules.ProdDependencies, u)
		if err != nil {
			return errors.AddContext(err, "failed to apply update")
		}
	}
	return nil
}

// CreateAndApplyTransaction is a helper method that creates a writeaheadlog
// transaction and applies it.
func CreateAndApplyTransaction(wal *writeaheadlog.WAL, updates ...writeaheadlog.Update) (err error) {
	// Create the writeaheadlog transaction.
	txn, err := wal.NewTransaction(updates)
	if err != nil {
		return errors.AddContext(err, "failed to create wal txn")
	}
	// No extra setup is required. Signal that it is done.
	if err := <-txn.SignalSetupComplete(); err != nil {
		return errors.AddContext(err, "failed to signal setup completion")
	}
	// Starting at this point the changes to be made are written to the WAL.
	// This means we need to panic in case applying the updates fails.
	defer func() {
		if err != nil {
			panic(err)
		}
	}()
	// Apply the updates.
	if err := ApplyUpdates(updates...); err != nil {
		return errors.AddContext(err, "failed to apply updates")
	}
	// Updates are applied. Let the writeaheadlog know.
	if err := txn.SignalUpdatesApplied(); err != nil {
		return errors.AddContext(err, "failed to signal that updates are applied")
	}
	return nil
}

// IsuplodirUpdate is a helper method that makes sure that a wal update belongs
// to the uplodir package.
func IsuplodirUpdate(update writeaheadlog.Update) bool {
	switch update.Name {
	case updateMetadataName, updateDeleteName:
		return true
	default:
		return false
	}
}

// New creates a new directory in the renter directory and makes sure there is a
// metadata file in the directory and creates one as needed. This method will
// also make sure that all the parent directories are created and have metadata
// files as well and will return the uplodir containing the information for the
// directory that matches the uploPath provided
//
// NOTE: the fullPath is expected to include the rootPath. The rootPath is used
// to determine when to stop recursively creating uplodir metadata.
func New(fullPath, rootPath string, mode os.FileMode, wal *writeaheadlog.WAL) (*Uplodir, error) {
	// Create path to directory and ensure path contains all metadata
	updates, err := createDirMetadataAll(fullPath, rootPath, mode)
	if err != nil {
		return nil, err
	}

	// Create metadata for directory
	md, update, err := createDirMetadata(fullPath, mode)
	if err != nil {
		return nil, err
	}

	// Create uplodir
	sd := &Uplodir{
		metadata: md,
		deps:     modules.ProdDependencies,
		path:     fullPath,
		wal:      wal,
	}

	return sd, CreateAndApplyTransaction(wal, append(updates, update)...)
}

// Loaduplodir loads the directory metadata from disk
func Loaduplodir(path string, deps modules.Dependencies, wal *writeaheadlog.WAL) (sd *Uplodir, err error) {
	sd = &Uplodir{
		deps: deps,
		path: path,
		wal:  wal,
	}
	sd.metadata, err = callLoaduplodirMetadata(filepath.Join(path, modules.UplodirExtension), modules.ProdDependencies)
	return sd, err
}

// createDirMetadata makes sure there is a metadata file in the directory and
// creates one as needed
func createDirMetadata(path string, mode os.FileMode) (Metadata, writeaheadlog.Update, error) {
	// Check if metadata file exists
	mdPath := filepath.Join(path, modules.UplodirExtension)
	_, err := os.Stat(mdPath)
	if err == nil {
		return Metadata{}, writeaheadlog.Update{}, os.ErrExist
	} else if !os.IsNotExist(err) {
		return Metadata{}, writeaheadlog.Update{}, err
	}

	// Initialize metadata, set Health and StuckHealth to DefaultDirHealth so
	// empty directories won't be viewed as being the most in need. Initialize
	// ModTimes.
	now := time.Now()
	md := Metadata{
		AggregateHealth:        DefaultDirHealth,
		AggregateMinRedundancy: DefaultDirRedundancy,
		AggregateModTime:       now,
		AggregateRemoteHealth:  DefaultDirHealth,
		AggregateStuckHealth:   DefaultDirHealth,

		Health:        DefaultDirHealth,
		MinRedundancy: DefaultDirRedundancy,
		Mode:          mode,
		ModTime:       now,
		RemoteHealth:  DefaultDirHealth,
		StuckHealth:   DefaultDirHealth,
	}
	update, err := createMetadataUpdate(mdPath, md)
	return md, update, err
}

// createDirMetadataAll creates a path on disk to the provided uploPath and make
// sure that all the parent directories have metadata files.
func createDirMetadataAll(dirPath, rootPath string, mode os.FileMode) ([]writeaheadlog.Update, error) {
	// Create path to directory
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return nil, err
	}
	if dirPath == rootPath {
		return []writeaheadlog.Update{}, nil
	}

	// Create metadata
	var updates []writeaheadlog.Update
	var err error
	for {
		dirPath = filepath.Dir(dirPath)
		if err != nil {
			return nil, err
		}
		if dirPath == string(filepath.Separator) || dirPath == "." {
			dirPath = rootPath
		}
		_, update, err := createDirMetadata(dirPath, mode)
		if err != nil && !os.IsExist(err) {
			return nil, err
		}
		if !os.IsExist(err) {
			updates = append(updates, update)
		}
		if dirPath == rootPath {
			break
		}
	}
	return updates, nil
}

// callLoaduplodirMetadata loads the directory metadata from disk.
func callLoaduplodirMetadata(path string, deps modules.Dependencies) (md Metadata, err error) {
	// Open the file.
	file, err := deps.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	defer func() {
		err = errors.Compose(err, file.Close())
	}()

	// Read the file
	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return Metadata{}, err
	}

	// Parse the json object.
	err = json.Unmarshal(bytes, &md)

	// CompatV1420 check if filemode is set. If not use the default. It's fine
	// not to persist it right away since it will either be persisted anyway or
	// we just set the values again the next time we load it and hope that it
	// gets persisted then.
	if md.Version == "" && md.Mode == 0 {
		md.Mode = modules.DefaultDirPerm
		md.Version = "1.0"
	}
	return
}

// Rename renames the uplodir to targetPath.
func (sd *Uplodir) rename(targetPath string) error {
	// TODO: os.Rename is not ACID
	err := os.Rename(sd.path, targetPath)
	if err != nil {
		return err
	}
	sd.path = targetPath
	return nil
}

// Delete removes the directory from disk and marks it as deleted. Once the
// directory is deleted, attempting to access the directory will return an
// error.
func (sd *Uplodir) Delete() error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Check if the uplodir is already deleted
	if sd.deleted {
		return nil
	}

	// Create and apply the delete update
	update := sd.createDeleteUpdate()
	err := sd.createAndApplyTransaction(update)
	sd.deleted = true
	return err
}

// saveDir saves the whole uplodir atomically.
func (sd *Uplodir) saveDir() error {
	// Check if Deleted
	if sd.deleted {
		return errors.AddContext(ErrDeleted, "cannot save a deleted uplodir")
	}
	metadataUpdate, err := sd.saveMetadataUpdate()
	if err != nil {
		return err
	}
	return sd.createAndApplyTransaction(metadataUpdate)
}

// Rename renames the uplodir to targetPath.
func (sd *Uplodir) Rename(targetPath string) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// Check if Deleted
	if sd.deleted {
		return errors.AddContext(ErrDeleted, "cannot rename a deleted uplodir")
	}
	return sd.rename(targetPath)
}

// SetPath sets the path field of the dir.
func (sd *Uplodir) SetPath(targetPath string) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	// Check if Deleted
	if sd.deleted {
		return errors.AddContext(ErrDeleted, "cannot set the path of a deleted uplodir")
	}
	sd.path = targetPath
	return nil
}

// UpdateBubbledMetadata updates the uplodir Metadata that is bubbled and saves
// the changes to disk. For fields that are not bubbled, this method sets them
// to the current values in the uplodir metadata
func (sd *Uplodir) UpdateBubbledMetadata(metadata Metadata) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	metadata.Mode = sd.metadata.Mode
	metadata.Version = sd.metadata.Version
	return sd.updateMetadata(metadata)
}

// UpdateLastHealthCheckTime updates the uplodir LastHealthCheckTime and
// AggregateLastHealthCheckTime and saves the changes to disk
func (sd *Uplodir) UpdateLastHealthCheckTime(aggregateLastHealthCheckTime, lastHealthCheckTime time.Time) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	md := sd.metadata
	md.AggregateLastHealthCheckTime = aggregateLastHealthCheckTime
	md.LastHealthCheckTime = lastHealthCheckTime
	return sd.updateMetadata(md)
}

// UpdateMetadata updates the uplodir metadata on disk
func (sd *Uplodir) UpdateMetadata(metadata Metadata) error {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.updateMetadata(metadata)
}

// updateMetadata updates the uplodir metadata on disk
func (sd *Uplodir) updateMetadata(metadata Metadata) error {
	// Check if the directory is deleted
	if sd.deleted {
		return errors.AddContext(ErrDeleted, "cannot update the metadata for a deleted directory")
	}

	// Update metadata
	sd.metadata.AggregateHealth = metadata.AggregateHealth
	sd.metadata.AggregateLastHealthCheckTime = metadata.AggregateLastHealthCheckTime
	sd.metadata.AggregateMinRedundancy = metadata.AggregateMinRedundancy
	sd.metadata.AggregateModTime = metadata.AggregateModTime
	sd.metadata.AggregateNumFiles = metadata.AggregateNumFiles
	sd.metadata.AggregateNumStuckChunks = metadata.AggregateNumStuckChunks
	sd.metadata.AggregateNumSubDirs = metadata.AggregateNumSubDirs
	sd.metadata.AggregateRemoteHealth = metadata.AggregateRemoteHealth
	sd.metadata.AggregateRepairSize = metadata.AggregateRepairSize
	sd.metadata.AggregateSize = metadata.AggregateSize
	sd.metadata.AggregateStuckHealth = metadata.AggregateStuckHealth
	sd.metadata.AggregateStuckSize = metadata.AggregateStuckSize

	sd.metadata.AggregateSkynetFiles = metadata.AggregateSkynetFiles
	sd.metadata.AggregateSkynetSize = metadata.AggregateSkynetSize

	sd.metadata.Health = metadata.Health
	sd.metadata.LastHealthCheckTime = metadata.LastHealthCheckTime
	sd.metadata.MinRedundancy = metadata.MinRedundancy
	sd.metadata.ModTime = metadata.ModTime
	sd.metadata.Mode = metadata.Mode
	sd.metadata.NumFiles = metadata.NumFiles
	sd.metadata.NumStuckChunks = metadata.NumStuckChunks
	sd.metadata.NumSubDirs = metadata.NumSubDirs
	sd.metadata.RemoteHealth = metadata.RemoteHealth
	sd.metadata.RepairSize = metadata.RepairSize
	sd.metadata.Size = metadata.Size
	sd.metadata.StuckHealth = metadata.StuckHealth
	sd.metadata.StuckSize = metadata.StuckSize

	sd.metadata.SkynetFiles = metadata.SkynetFiles
	sd.metadata.SkynetSize = metadata.SkynetSize

	sd.metadata.Version = metadata.Version

	// Testing check to ensure new fields aren't missed
	if build.Release == "testing" && !reflect.DeepEqual(sd.metadata, metadata) {
		str := fmt.Sprintf(`Input metadata not equal to set metadata
		metadata
		%v
		sd.metadata
		%v`, metadata, sd.metadata)
		build.Critical(str)
	}

	// Sanity check that uplodir is on disk
	_, err := os.Stat(sd.path)
	if os.IsNotExist(err) {
		build.Critical("UpdateMetadata called on a uplodir that does not exist on disk")
		err = os.MkdirAll(filepath.Dir(sd.path), modules.DefaultDirPerm)
		if err != nil {
			return errors.AddContext(err, "unable to create missing uplodir directory on disk")
		}
	}

	return sd.saveDir()
}
