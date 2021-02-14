package skynetblocklist

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
	"github.com/uplo-tech/errors"
)

const (
	blacklistPersistFile string = "skynetblacklist"
)

var (
	blacklistMetadataHeader = types.NewSpecifier("SkynetBlacklist\n")
	metadataVersionV143     = types.NewSpecifier("v1.4.3\n")

	// NOTE: There is a MetadataVersionV150 in the persist package
)

// tempPersistFileName is a helper for creating the file name for a temporary
// persist file during conversion
func tempPersistFileName(persistFileName string) string {
	return persistFileName + "_temp"
}

// convertPersistVersionFromv143Tov150 handles the compatibility code for
// upgrading the persistence from v1.4.3 to v1.5.0. The change in persistence is
// that the hash of the merkleroot is now persisted instead of the merkleroot
// itself.
func convertPersistVersionFromv143Tov150(persistDir string) (err error) {
	// Identify the filepath for the persist file and the temp persist file that
	// will be created during the conversion of the persistence from v1.4.3 to
	// v1.5.0
	persistFilePath := filepath.Join(persistDir, blacklistPersistFile)
	tempFilePath := filepath.Join(persistDir, tempPersistFileName(blacklistPersistFile))

	// Create a temporary file from v1.4.3 persist file
	readerv143, err := createTempFileFromPersistFile(persistDir, blacklistPersistFile, blacklistMetadataHeader, metadataVersionV143)
	if err != nil {
		return errors.AddContext(err, "unable to create temp file")
	}

	// Delete the v1.4.3 persist file
	err = os.RemoveAll(persistFilePath)
	if err != nil {
		return errors.AddContext(err, "unable to remove v1.4.3 persist file from disk")
	}

	// Unmarshal the persistence. We can still use the same unmarshalObjects
	// function since merkleroots are a crypto.Hash this code did not change
	merkleroots, err := unmarshalObjects(readerv143)
	if err != nil {
		return errors.AddContext(err, "unable to unmarshal persist objects")
	}

	// Convert merkleroots to hashes and marshal again
	var buf bytes.Buffer
	for mr := range merkleroots {
		hash := crypto.HashObject(mr)
		pe := persistEntry{hash, true}
		bytes := encoding.Marshal(pe)
		_, err = buf.Write(bytes)
		if err != nil {
			return errors.AddContext(err, "unable to write merkleroot to buffer")
		}
	}

	// Initialize new v1.5.0 persistence
	aopV150, _, err := persist.NewAppendOnlyPersist(persistDir, blacklistPersistFile, blacklistMetadataHeader, persist.MetadataVersionv150)
	if err != nil {
		return errors.AddContext(err, "unable to initialize v1.5.0 persist file")
	}
	defer func() {
		err = errors.Compose(err, aopV150.Close())
	}()

	// Write the hashes to the v1.5.0 persist file
	_, err = aopV150.Write(buf.Bytes())
	if err != nil {
		return errors.AddContext(err, "unable to write to v150 persist file")
	}

	// Delete the temporary file
	err = os.Remove(tempFilePath)
	if err != nil {
		return errors.AddContext(err, "unable to remove temp file from disk")
	}

	return nil
}

// convertPersistVersionFromv150Tov151 handles the compatibility code for
// upgrading the persistence from v1.5.0 to v1.5.1. The change in persistence is
// in the name of the header and the version.
func convertPersistVersionFromv150Tov151(persistDir string) error {
	// Identify the filepath for the persist file and the temp persist file that
	// will be created during the conversion of the persistence from
	// v1.5.0 to v1.5.1
	persistFilePath := filepath.Join(persistDir, blacklistPersistFile)
	tempFilePath := filepath.Join(persistDir, tempPersistFileName(blacklistPersistFile))

	// Create a temporary file from v1.5.0 persist file
	readerv150, err := createTempFileFromPersistFile(persistDir, blacklistPersistFile, blacklistMetadataHeader, persist.MetadataVersionv150)
	if err != nil {
		return errors.AddContext(err, "unable to create temp file")
	}

	// Delete the v1.5.0 persist file
	err = os.RemoveAll(persistFilePath)
	if err != nil {
		return errors.AddContext(err, "unable to remove v1.5.0 persist file from disk")
	}

	// Initialize new blocklist persistence
	aopBlocklist, _, err := persist.NewAppendOnlyPersist(persistDir, persistFile, metadataHeader, metadataVersion)
	if err != nil {
		return errors.AddContext(err, "unable to initialize blocklist persist file")
	}
	defer aopBlocklist.Close()

	// Write the persist data to the blocklist persist file
	data, err := ioutil.ReadAll(readerv150)
	if err != nil {
		return errors.AddContext(err, "unable to read data from v150 reader")
	}
	_, err = aopBlocklist.Write(data)
	if err != nil {
		return errors.AddContext(err, "unable to write to blocklist persist file")
	}

	// Delete the temporary file
	err = os.Remove(tempFilePath)
	if err != nil {
		return errors.AddContext(err, "unable to remove temp file from disk")
	}

	return nil
}

// createTempFileFromPersistFile copies the data from the persist file into
// a temporary file and returns a reader for the data. This function checks for
// the existence of a temp file first and will return a reader for the temporary
// file if the temporary file contains a valid checksum.
func createTempFileFromPersistFile(persistDir, fileName string, header, version types.Specifier) (_ io.Reader, err error) {
	// Try and load the temporary file first. This is done first because an
	// unclean shutdown could result in a valid temporary file existing but no
	// persist file existing. In this case we do not want a call to
	// NewAppendOnlyPersist to create a new persist file resulting in a loss of
	// the data in the temporary file
	tempFilePath := filepath.Join(persistDir, tempPersistFileName(fileName))
	reader, err := loadTempFile(tempFilePath)
	if err == nil {
		// Temporary file is valid, return the reader
		return reader, nil
	}

	// If there was an error loading the temporary file then we want to remove any
	// file in that location.
	err = os.RemoveAll(tempFilePath)
	if err != nil {
		return nil, err
	}

	// Open the persist file
	aop, reader, err := persist.NewAppendOnlyPersist(persistDir, fileName, header, version)
	if err != nil {
		return nil, errors.AddContext(err, "unable to load persistence")
	}
	defer func() {
		err = errors.Compose(err, aop.Close())
	}()

	// Read the persist file
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, errors.AddContext(err, "unable to read persist file")
	}

	// Create the checksum for the persist file
	checksum := crypto.HashBytes(data)

	// Create the temporary file
	f, err := os.Create(tempFilePath)
	if err != nil {
		return nil, errors.AddContext(err, "unable to open temp file")
	}
	defer func() {
		err = errors.Compose(err, f.Close())
	}()

	// Write the data to the temp file, leaving space for the checksum at the
	// beginning of the file
	//
	// We write the checksum second to protect against unclean shut downs. If we
	// wrote the checksum first and then there was an unclean shut down, we would
	// not be able to simply check for a checksum at the beginning of the file.
	offset := int64(len(checksum))
	_, err = f.WriteAt(data, offset)
	if err != nil {
		return nil, errors.AddContext(err, "unable to write persist data to temp file")
	}

	// Write the checksum to the beginning of the file
	_, err = f.WriteAt(checksum[:], 0)
	if err != nil {
		return nil, errors.AddContext(err, "unable to write persist checksum to temp file")
	}

	// Sync writes
	err = f.Sync()
	if err != nil {
		return nil, errors.AddContext(err, "unable to sync temp file")
	}

	// Since the reader has been read, create and return a new reader.
	return bytes.NewReader(data), nil
}

// loadTempFile will load a temporary file and verifies the checksum that was
// prefixed. If the checksum is valid a reader will be returned.
func loadTempFile(tempFilePath string) (_ io.Reader, err error) {
	// Open the temporary file
	f, err := os.Open(tempFilePath)
	if err != nil {
		return nil, errors.AddContext(err, "unable to open temp file")
	}
	defer func() {
		err = errors.Compose(err, f.Close())
	}()

	// Read file
	fileBytes, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, errors.AddContext(err, "unable to read file")
	}

	// Verify there is enough data for a checksum
	if len(fileBytes) < crypto.HashSize {
		return nil, errors.New("temp file does not contain enough bytes for a checksum")
	}

	// Verify checksum
	checksum := fileBytes[:crypto.HashSize]
	fileChecksum := crypto.HashBytes(fileBytes[crypto.HashSize:])
	if !bytes.Equal(checksum, fileChecksum[:]) {
		return nil, errors.New("checksum invalid")
	}

	// Return the data after the checksum as a reader
	return bytes.NewReader(fileBytes[crypto.HashSize:]), nil
}

// loadPersist will load the persistence from the persist file in a way that
// takes into account any previous persistence updates
func loadPersist(persistDir string) (*persist.AppendOnlyPersist, io.Reader, error) {
	// Check for any temp files indicating that a persistence update was
	// interrupted
	//
	// We check for a temp file first because in the event of an unclean shutdown
	// there is the potential for a temp file to exist but no persist file. In
	// this case a call to NewAppendOnlyPersist would create a new persist file
	// and we would lose the information in the temp file.
	tempFilePath := filepath.Join(persistDir, tempPersistFileName(blacklistPersistFile))
	_, err := os.Stat(tempFilePath)
	if !os.IsNotExist(err) {
		// Temp file exists. Continue persistence update.
		err = convertPersistence(persistDir)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to convert persistence with the existence of a temp file")
		}
	}

	// Check for the existence of the old persist file
	_, err = os.Stat(filepath.Join(persistDir, blacklistPersistFile))
	if !os.IsNotExist(err) {
		// Old persist file exists, try and update persistence
		err = convertPersistence(persistDir)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to convert persistence when old persist file exists")
		}
	}

	// Load Persistence
	aop, reader, err := persist.NewAppendOnlyPersist(persistDir, persistFile, metadataHeader, metadataVersion)
	if errors.Contains(err, persist.ErrWrongVersion) {
		// Wrong version, try and convert persistence
		err = convertPersistence(persistDir)
		if err != nil {
			return nil, nil, errors.AddContext(err, "unable to convert persistence after wrong version error")
		}
		// Load the v1.5.1 persistence
		aop, reader, err = persist.NewAppendOnlyPersist(persistDir, persistFile, metadataHeader, metadataVersion)
	}
	if err != nil {
		return nil, nil, errors.AddContext(err, fmt.Sprintf("unable to initialize the skynet blocklist persistence at '%v'", aop.FilePath()))
	}

	return aop, reader, nil
}

// convertPersistence will try and convert the persistence from the oldest
// persist version to the newest.
//
// NOTE: Errors from earlier versions will only be returned if there is an error
// with the newest version
func convertPersistence(persistDir string) error {
	// Try converting persistence from v1.4.3 to v1.5.0
	errv143Tov150 := convertPersistVersionFromv143Tov150(persistDir)

	// Try converting persistence from v1.5.0 to v1.5.1
	errv150TOv151 := convertPersistVersionFromv150Tov151(persistDir)
	if errv150TOv151 != nil {
		return errors.Compose(errv143Tov150, errv150TOv151)
	}
	return nil
}
