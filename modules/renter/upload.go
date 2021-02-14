package renter

// upload.go performs basic preprocessing on upload requests and then adds the
// requested files into the repair heap.
//
// TODO: Currently the minimum contracts check is not enforced while testing,
// which means that code is not covered at all. Enabling enforcement during
// testing will probably break a ton of existing tests, which means they will
// all need to be fixed when we do enable it, but we should enable it.

import (
	"fmt"
	"os"

	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem"
)

var (
	// ErrUploadDirectory is returned if the user tries to upload a directory.
	ErrUploadDirectory = errors.New("cannot upload directory")
)

// Upload instructs the renter to start tracking a file. The renter will
// automatically upload and repair tracked files using a background loop.
func (r *Renter) Upload(up modules.FileUploadParams) error {
	if err := r.tg.Add(); err != nil {
		return err
	}
	defer r.tg.Done()

	// Check if the file is a directory.
	sourceInfo, err := os.Stat(up.Source)
	if err != nil {
		return errors.AddContext(err, "unable to stat input file")
	}
	if sourceInfo.IsDir() {
		return ErrUploadDirectory
	}

	// Check for read access.
	file, err := os.Open(up.Source)
	if err != nil {
		return errors.AddContext(err, "unable to open the source file")
	}
	err = file.Close()
	if err != nil {
		return errors.AddContext(err, "unable to close file after checking permissions")
	}

	// Delete existing file if overwrite flag is set. Ignore ErrUnknownPath.
	if up.Force {
		err := r.DeleteFile(up.UploPath)
		if err != nil && !errors.Contains(err, filesystem.ErrNotExist) {
			return errors.AddContext(err, "unable to delete existing file")
		}
	}

	// Fill in any missing upload params with sensible defaults.
	if up.ErasureCode == nil {
		up.ErasureCode = modules.NewRSSubCodeDefault()
	}

	// Check that we have contracts to upload to. We need at least data +
	// parity/2 contracts. NumPieces is equal to data+parity, and min pieces is
	// equal to parity. Therefore (NumPieces+MinPieces)/2 = (data+data+parity)/2
	// = data+parity/2.
	numContracts := len(r.hostContractor.Contracts())
	requiredContracts := (up.ErasureCode.NumPieces() + up.ErasureCode.MinPieces()) / 2
	if numContracts < requiredContracts && build.Release != "testing" {
		return fmt.Errorf("not enough contracts to upload file: got %v, needed %v", numContracts, (up.ErasureCode.NumPieces()+up.ErasureCode.MinPieces())/2)
	}

	// Create the directory path on disk. Renter directory is already present so
	// only files not in top level directory need to have directories created
	dirUploPath, err := up.UploPath.Dir()
	if err != nil {
		return err
	}

	// Determine what type of encryption key to use. If no cipher type has been
	// set, the default renter type will be used.
	var ct crypto.CipherType
	if up.CipherType == ct {
		up.CipherType = crypto.TypeDefaultRenter
	}
	// Generate a key using the cipher type.
	cipherKey := crypto.GenerateUploKey(up.CipherType)

	// Create the Uplofile and add to renter
	err = r.staticFileSystem.NewUploFile(up.UploPath, up.Source, up.ErasureCode, cipherKey, uint64(sourceInfo.Size()), sourceInfo.Mode(), up.DisablePartialChunk)
	if err != nil {
		return errors.AddContext(err, "could not create a new uplo file")
	}
	entry, err := r.staticFileSystem.OpenUploFile(up.UploPath)
	if err != nil {
		return errors.AddContext(err, "could not open the new uplo file")
	}

	// No need to upload zero-byte files.
	if sourceInfo.Size() == 0 {
		return nil
	}

	// Bubble the health of the UploFile directory to ensure the health is
	// updated with the new file
	go r.callThreadedBubbleMetadata(dirUploPath)

	// Create nil maps for offline and goodForRenew to pass in to
	// callBuildAndPushChunks. These maps are used to determine the health of
	// the file and its chunks. Nil maps will result in the file and its chunks
	// having the worst possible health which is accurate since the file hasn't
	// been uploaded yet
	nilMap := make(map[string]bool)
	// Send the upload to the repair loop.
	hosts := r.managedRefreshHostsAndWorkers()
	r.callBuildAndPushChunks([]*filesystem.FileNode{entry}, hosts, targetUnstuckChunks, nilMap, nilMap)
	select {
	case r.uploadHeap.newUploads <- struct{}{}:
	default:
	}
	return nil
}
