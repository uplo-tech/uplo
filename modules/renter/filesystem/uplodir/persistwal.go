package uplodir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/writeaheadlog"
)

// applyUpdate applies the wal update
func applyUpdate(deps modules.Dependencies, update writeaheadlog.Update) error {
	switch update.Name {
	case updateDeleteName:
		return readAndApplyDeleteUpdate(update)
	case updateMetadataName:
		return readAndApplyMetadataUpdate(deps, update)
	default:
		return fmt.Errorf("update not recognized: %v", update.Name)
	}
}

// createMetadataUpdate is a helper method which creates a writeaheadlog update
// for updating the uplodir metadata
func createMetadataUpdate(path string, metadata Metadata) (writeaheadlog.Update, error) {
	// Encode metadata
	data, err := json.Marshal(metadata)
	if err != nil {
		return writeaheadlog.Update{}, err
	}
	// Create update
	return writeaheadlog.Update{
		Name:         updateMetadataName,
		Instructions: encoding.MarshalAll(data, path),
	}, nil
}

// readAndApplyDeleteUpdate reads the delete update and then applies it. This
// helper assumes that the file is not currently open and so should only be
// called on startup before any uplodir is loaded from disk
func readAndApplyDeleteUpdate(update writeaheadlog.Update) error {
	if !IsuplodirUpdate(update) {
		err := errors.New("readAndApplyDeleteUpdate can't read non-uplodir update")
		build.Critical(err)
		return err
	}
	// Delete from disk
	err := os.RemoveAll(readDeleteUpdate(update))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// readAndApplyMetadataUpdate reads the metadata update and then applies it.
// This helper assumes that the file is not currently open and so should only be
// called on startup before any uplodir is loaded from disk
func readAndApplyMetadataUpdate(deps modules.Dependencies, update writeaheadlog.Update) (err error) {
	if !IsuplodirUpdate(update) {
		err := errors.New("readAndApplyMetadataUpdate can't read non-uplodir update")
		build.Critical(err)
		return err
	}
	// Decode update.
	data, path, err := readMetadataUpdate(update)
	if err != nil {
		return err
	}

	// Create the folder if it doesn't exist yet.
	err = os.MkdirAll(filepath.Dir(path), modules.DefaultDirPerm)
	if err != nil {
		return err
	}

	// Write out the data to the real file, with a sync.
	file, err := deps.OpenFile(path, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, file.Close())
	}()

	// Write and sync.
	n, err := file.Write(data)
	if err != nil {
		return err
	}
	if n < len(data) {
		return fmt.Errorf("update was only applied partially - %v / %v", n, len(data))
	}
	return file.Sync()
}

// readDeleteUpdate unmarshals the update's instructions and returns the
// encoded path.
func readDeleteUpdate(update writeaheadlog.Update) string {
	if update.Name != updateDeleteName {
		err := errors.New("readDeleteUpdate can't read non-delete updates")
		build.Critical(err)
		return ""
	}
	return string(update.Instructions)
}

// readMetadataUpdate unmarshals the update's instructions and returns the
// metadata encoded in the instructions.
func readMetadataUpdate(update writeaheadlog.Update) (data []byte, path string, err error) {
	if update.Name != updateMetadataName {
		err = errors.New("readMetadataUpdate can't read non-metadata updates")
		build.Critical(err)
		return
	}
	err = encoding.UnmarshalAll(update.Instructions, &data, &path)
	return
}

// applyUpdates applies a number of writeaheadlog updates to the corresponding
// uplodir.
func (sd *Uplodir) applyUpdates(updates ...writeaheadlog.Update) error {
	// If the set of updates contains a delete, all updates prior to that delete
	// are irrelevant, so perform the last delete and then process the remaining
	// updates. This also prevents a bug on Windows where we attempt to delete
	// the file while holding a open file handle.
	for i := len(updates) - 1; i >= 0; i-- {
		u := updates[i]
		if u.Name != updateDeleteName {
			continue
		}
		// Read and apply the delete update.
		if err := readAndApplyDeleteUpdate(u); err != nil {
			return err
		}
		// Truncate the updates and break out of the for loop.
		updates = updates[i+1:]
		break
	}
	if len(updates) == 0 {
		return nil
	}

	// Open the file
	file, err := sd.deps.OpenFile(sd.mdPath(), os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			// If no error occurred we sync and close the file.
			err = errors.Compose(file.Sync(), file.Close())
		} else {
			// Otherwise we still need to close the file.
			err = errors.Compose(err, file.Close())
		}
	}()

	// Apply updates.
	for _, u := range updates {
		err := func() error {
			switch u.Name {
			case updateDeleteName:
				build.Critical("Unexpected delete update")
				return nil
			case updateMetadataName:
				return sd.readAndApplyMetadataUpdate(file, u)
			default:
				return fmt.Errorf("update not recognized: %v", u.Name)
			}
		}()
		if err != nil {
			return errors.AddContext(err, "failed to apply update")
		}
	}
	return nil
}

// createAndApplyTransaction is a helper method that creates a writeaheadlog
// transaction and applies it.
func (sd *Uplodir) createAndApplyTransaction(updates ...writeaheadlog.Update) (err error) {
	// This should never be called on a deleted directory.
	if sd.deleted {
		return errors.New("shouldn't apply updates on deleted directory")
	}
	// Create the writeaheadlog transaction.
	txn, err := sd.wal.NewTransaction(updates)
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
	if err := sd.applyUpdates(updates...); err != nil {
		return errors.AddContext(err, "failed to apply updates")
	}
	// Updates are applied. Let the writeaheadlog know.
	if err := txn.SignalUpdatesApplied(); err != nil {
		return errors.AddContext(err, "failed to signal that updates are applied")
	}
	return nil
}

// createDeleteUpdate is a helper method that creates a writeaheadlog for
// deleting a directory.
func (sd *Uplodir) createDeleteUpdate() writeaheadlog.Update {
	return writeaheadlog.Update{
		Name:         updateDeleteName,
		Instructions: []byte(sd.path),
	}
}

// readAndApplyMetadataUpdate reads the metadata update for a file and then
// applies it.
//
// NOTE: This method does not fsync after the write.
func (sd *Uplodir) readAndApplyMetadataUpdate(file modules.File, update writeaheadlog.Update) error {
	// Decode update.
	data, path, err := readMetadataUpdate(update)
	if err != nil {
		return err
	}

	// Sanity check path belongs to uplodir
	if path != sd.mdPath() {
		build.Critical(fmt.Sprintf("can't apply update for file %s to uplodir %s", path, sd.path))
		return nil
	}

	// Write.
	n, err := file.Write(data)
	if err != nil {
		return err
	}
	if n < len(data) {
		return fmt.Errorf("update was only applied partially - %v / %v", n, len(data))
	}
	return nil
}

// saveMetadataUpdate saves the metadata of the uplodir
func (sd *Uplodir) saveMetadataUpdate() (writeaheadlog.Update, error) {
	return createMetadataUpdate(sd.mdPath(), sd.metadata)
}
