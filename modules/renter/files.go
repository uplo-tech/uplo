package renter

import (
	"github.com/uplo-tech/uplo/modules"

	"github.com/uplo-tech/errors"
)

// DeleteFile removes a file entry from the renter and deletes its data from
// the hosts it is stored on.
func (r *Renter) DeleteFile(uploPath modules.UploPath) error {
	err := r.tg.Add()
	if err != nil {
		return err
	}
	defer r.tg.Done()

	// Perform the delete operation.
	err = r.staticFileSystem.DeleteFile(uploPath)
	if err != nil {
		return errors.AddContext(err, "unable to delete uplofile from filesystem")
	}

	// Update the filesystem metadata.
	//
	// TODO: This is incorrect, should be running the metadata update call on a
	// node, not on a uploPath. The node should be returned by the delete call.
	// Need a metadata update func that operates on a node to do that.
	dirUploPath, err := uploPath.Dir()
	if err != nil {
		r.log.Printf("Unable to fetch the directory from a uploPath %v for deleted uplofile: %v", uploPath, err)
		// Return 'nil' because the delete operation succeeded, it was only the
		// metadata update operation that failed.
		return nil
	}
	go r.callThreadedBubbleMetadata(dirUploPath)
	return nil
}

// FileList loops over all the files within the directory specified by uploPath
// and will then call the provided listing function on the file.
func (r *Renter) FileList(uploPath modules.UploPath, recursive, cached bool, flf modules.FileListFunc) error {
	if err := r.tg.Add(); err != nil {
		return err
	}
	defer r.tg.Done()
	var err error
	if cached {
		err = r.staticFileSystem.CachedList(uploPath, recursive, flf, func(modules.DirectoryInfo) {})
	} else {
		offlineMap, goodForRenewMap, contractsMap := r.managedContractUtilityMaps()
		err = r.staticFileSystem.List(uploPath, recursive, offlineMap, goodForRenewMap, contractsMap, flf, func(modules.DirectoryInfo) {})
	}
	if err != nil {
		return err
	}
	return err
}

// File returns file from uploPath queried by user.
// Update based on FileList
func (r *Renter) File(uploPath modules.UploPath) (modules.FileInfo, error) {
	if err := r.tg.Add(); err != nil {
		return modules.FileInfo{}, err
	}
	defer r.tg.Done()
	offline, goodForRenew, contracts := r.managedContractUtilityMaps()
	fi, err := r.staticFileSystem.FileInfo(uploPath, offline, goodForRenew, contracts)
	if err != nil {
		return modules.FileInfo{}, errors.AddContext(err, "unable to get the fileinfo from the filesystem")
	}
	return fi, nil
}

// FileCached returns file from uploPath queried by user, using cached values for
// health and redundancy.
func (r *Renter) FileCached(uploPath modules.UploPath) (modules.FileInfo, error) {
	if err := r.tg.Add(); err != nil {
		return modules.FileInfo{}, err
	}
	defer r.tg.Done()
	return r.staticFileSystem.CachedFileInfo(uploPath)
}

// RenameFile takes an existing file and changes the nickname. The original
// file must exist, and there must not be any file that already has the
// replacement nickname.
func (r *Renter) RenameFile(currentName, newName modules.UploPath) error {
	if err := r.tg.Add(); err != nil {
		return err
	}
	defer r.tg.Done()

	// Rename file.
	err := r.staticFileSystem.RenameFile(currentName, newName)
	if err != nil {
		return err
	}

	// Call callThreadedBubbleMetadata on the old and new directories to make
	// sure the system metadata is updated to reflect the move.
	oldDirUploPath, err := currentName.Dir()
	if err != nil {
		return err
	}
	newDirUploPath, err := newName.Dir()
	if err != nil {
		return err
	}
	bubblePaths := r.newUniqueRefreshPaths()
	err = bubblePaths.callAdd(oldDirUploPath)
	if err != nil {
		r.log.Printf("failed to add old directory '%v' to bubble paths:  %v", oldDirUploPath, err)
	}
	err = bubblePaths.callAdd(newDirUploPath)
	if err != nil {
		r.log.Printf("failed to add new directory '%v' to bubble paths:  %v", newDirUploPath, err)
	}
	bubblePaths.callRefreshAll()
	return nil
}

// SetFileStuck sets the Stuck field of the whole uplofile to stuck.
func (r *Renter) SetFileStuck(uploPath modules.UploPath, stuck bool) (err error) {
	if err := r.tg.Add(); err != nil {
		return err
	}
	defer r.tg.Done()
	// Open the file.
	entry, err := r.staticFileSystem.OpenUploFile(uploPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, entry.Close())
	}()
	// Update the file.
	return entry.SetAllStuck(stuck)
}
