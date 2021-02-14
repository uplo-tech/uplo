package uplofile

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// closeFileInTest is a small helper for calling close on a file in a test
func closeFileInTest(t *testing.T, f *os.File) {
	err := f.Close()
	if err != nil {
		t.Fatal(err)
	}
}

// equalFiles is a helper that compares two UploFiles for equality.
func equalFiles(sf, sf2 *UploFile) error {
	// Backup the metadata structs for both files.
	md := sf.staticMetadata
	md2 := sf2.staticMetadata
	// Compare the timestamps first since they can't be compared with
	// DeepEqual.
	if sf.staticMetadata.AccessTime.Unix() != sf2.staticMetadata.AccessTime.Unix() {
		return errors.New("AccessTime's don't match")
	}
	if sf.staticMetadata.ChangeTime.Unix() != sf2.staticMetadata.ChangeTime.Unix() {
		return errors.New("ChangeTime's don't match")
	}
	if sf.staticMetadata.CreateTime.Unix() != sf2.staticMetadata.CreateTime.Unix() {
		return errors.New("CreateTime's don't match")
	}
	if sf.staticMetadata.ModTime.Unix() != sf2.staticMetadata.ModTime.Unix() {
		return errors.New("ModTime's don't match")
	}
	if sf.staticMetadata.LastHealthCheckTime.Unix() != sf2.staticMetadata.LastHealthCheckTime.Unix() {
		return errors.New("LastHealthCheckTime's don't match")
	}
	// Set the timestamps to zero for DeepEqual.
	sf.staticMetadata.AccessTime = time.Time{}
	sf.staticMetadata.ChangeTime = time.Time{}
	sf.staticMetadata.CreateTime = time.Time{}
	sf.staticMetadata.ModTime = time.Time{}
	sf.staticMetadata.LastHealthCheckTime = time.Time{}
	sf2.staticMetadata.AccessTime = time.Time{}
	sf2.staticMetadata.ChangeTime = time.Time{}
	sf2.staticMetadata.CreateTime = time.Time{}
	sf2.staticMetadata.ModTime = time.Time{}
	sf2.staticMetadata.LastHealthCheckTime = time.Time{}
	// Compare the rest of sf and sf2.
	if !reflect.DeepEqual(sf.staticMetadata, sf2.staticMetadata) {
		fmt.Println(sf.staticMetadata)
		fmt.Println(sf2.staticMetadata)
		return errors.New("sf metadata doesn't equal sf2 metadata")
	}
	if !reflect.DeepEqual(sf.pubKeyTable, sf2.pubKeyTable) {
		fmt.Println(sf.pubKeyTable)
		fmt.Println(sf2.pubKeyTable)
		return errors.New("sf pubKeyTable doesn't equal sf2 pubKeyTable")
	}
	if sf.numChunks != sf2.numChunks {
		return errors.New(fmt.Sprint("sf numChunks doesn't equal sf2 numChunks", sf.numChunks, sf2.numChunks))
	}
	if sf.uploFilePath != sf2.uploFilePath {
		return fmt.Errorf("sf2 filepath was %v but should be %v",
			sf2.uploFilePath, sf.uploFilePath)
	}
	// Restore the original metadata.
	sf.staticMetadata = md
	sf2.staticMetadata = md2
	return nil
}

// addRandomHostKeys adds n random host keys to the UploFile's pubKeyTable. It
// doesn't write them to disk.
func (sf *UploFile) addRandomHostKeys(n int) {
	for i := 0; i < n; i++ {
		// Create random specifier and key.
		algorithm := types.Specifier{}
		fastrand.Read(algorithm[:])

		// Create random key.
		key := fastrand.Bytes(32)

		// Append new key to slice.
		sf.pubKeyTable = append(sf.pubKeyTable, HostPublicKey{
			PublicKey: types.UploPublicKey{
				Algorithm: algorithm,
				Key:       key,
			},
			Used: true,
		})
	}
}

// customTestFileAndWAL creates an empty UploFile for testing and also returns
// the WAL used in the creation and the path of the WAL.
func customTestFileAndWAL(uploFilePath, source string, rc modules.ErasureCoder, sk crypto.CipherKey, fileSize uint64, numChunks int, fileMode os.FileMode) (*UploFile, *writeaheadlog.WAL, string) {
	// Create the path to the file.
	dir, _ := filepath.Split(uploFilePath)
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		panic(err)
	}
	// Create a test wal
	wal, walPath := newTestWAL()
	// Create the corresponding partials file if it doesn't exist already.
	var partialsUploFile *UploFile
	partialsUploPath := modules.CombinedUploFilePath(rc)
	partialsUploFilePath := partialsUploPath.UploPartialsFileSysPath(dir)
	if _, err = os.Stat(partialsUploFilePath); os.IsNotExist(err) {
		partialsUploFile, err = New(partialsUploFilePath, "", wal, rc, sk, 0, fileMode, nil, false)
	} else {
		partialsUploFile, err = LoadUploFile(partialsUploFilePath, wal)
	}
	if err != nil {
		panic(fmt.Sprint("failed to load partialsUploFile", err))
	}
	// Check that the partials file is sane.
	if partialsUploFile.numChunks > 0 {
		panic(fmt.Sprint("partialsUploFile shouldn't have any chunks but had ", partialsUploFile.numChunks))
	}
	/* PARTIAL TODO:
	partialsEntry := &UploFileSetEntry{
		partialsUploFile,
		uint64(fastrand.Intn(math.MaxInt32)),
	}
	*/
	// Create the file.
	sf, err := New(uploFilePath, source, wal, rc, sk, fileSize, fileMode, nil, false)
	if err != nil {
		panic(err)
	}
	// Check that the number of chunks in the file is correct.
	if numChunks >= 0 && sf.numChunks != numChunks {
		panic(fmt.Sprintf("newTestFile didn't create the expected number of chunks: %v %v %v", sf.numChunks, numChunks, fileSize))
	}
	return sf, wal, walPath
}

// newBlankTestFileAndWAL is like customTestFileAndWAL but uses random params
// and allows the caller to specify how many chunks the file should at least
// contain.
func newBlankTestFileAndWAL(minChunks int) (*UploFile, *writeaheadlog.WAL, string) {
	uploFilePath, _, source, rc, sk, fileSize, numChunks, fileMode := newTestFileParams(minChunks, true)
	return customTestFileAndWAL(uploFilePath, source, rc, sk, fileSize, numChunks, fileMode)
}

// newBlankTestFile is a helper method to create a UploFile for testing without
// any hosts or uploaded pieces.
func newBlankTestFile() *UploFile {
	sf, _, _ := newBlankTestFileAndWAL(1)
	return sf
}

// newTestFile creates a UploFile for testing where each chunk has a random
// number of pieces.
func newTestFile() *UploFile {
	sf := newBlankTestFile()
	if err := setCombinedChunkOfTestFile(sf); err != nil {
		panic(err)
	}
	// Add pieces to each chunk.
	for chunkIndex := 0; chunkIndex < sf.numChunks; chunkIndex++ {
		for pieceIndex := 0; pieceIndex < sf.ErasureCode().NumPieces(); pieceIndex++ {
			numPieces := fastrand.Intn(3) // up to 2 hosts for each piece
			for i := 0; i < numPieces; i++ {
				pk := types.UploPublicKey{Key: fastrand.Bytes(crypto.EntropySize)}
				mr := crypto.Hash{}
				fastrand.Read(mr[:])
				if err := sf.AddPiece(pk, uint64(chunkIndex), uint64(pieceIndex), mr); err != nil {
					panic(err)
				}
			}
		}
	}
	return sf
}

// newTestFileParams creates the required parameters for creating a uplofile and
// creates a directory for the file
func newTestFileParams(minChunks int, partialChunk bool) (string, modules.UploPath, string, modules.ErasureCoder, crypto.CipherKey, uint64, int, os.FileMode) {
	rc, err := modules.NewRSCode(10, 20)
	if err != nil {
		panic(err)
	}
	return newTestFileParamsWithRC(minChunks, partialChunk, rc)
}

// newTestFileParamsWithRC creates the required parameters for creating a uplofile and
// creates a directory for the file.
func newTestFileParamsWithRC(minChunks int, partialChunk bool, rc modules.ErasureCoder) (string, modules.UploPath, string, modules.ErasureCoder, crypto.CipherKey, uint64, int, os.FileMode) {
	// Create arguments for new file.
	sk := crypto.GenerateUploKey(crypto.TypeDefaultRenter)
	pieceSize := modules.SectorSize - sk.Type().Overhead()
	uploPath := modules.RandomUploPath()
	numChunks := fastrand.Intn(10) + minChunks
	chunkSize := pieceSize * uint64(rc.MinPieces())
	fileSize := chunkSize * uint64(numChunks)
	if partialChunk {
		fileSize-- // force partial chunk
	}
	fileMode := os.FileMode(777)
	source := string(hex.EncodeToString(fastrand.Bytes(8)))

	// Create the path to the file.
	uploFilePath := uploPath.UploFileSysPath(filepath.Join(os.TempDir(), "uplofiles", hex.EncodeToString(fastrand.Bytes(16))))
	dir, _ := filepath.Split(uploFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		panic(err)
	}
	return uploFilePath, uploPath, source, rc, sk, fileSize, numChunks, fileMode
}

// newTestWal is a helper method to create a WAL for testing.
func newTestWAL() (*writeaheadlog.WAL, string) {
	// Create the wal.
	walsDir := filepath.Join(os.TempDir(), "wals")
	if err := os.MkdirAll(walsDir, 0700); err != nil {
		panic(err)
	}
	walFilePath := filepath.Join(walsDir, hex.EncodeToString(fastrand.Bytes(8)))
	_, wal, err := writeaheadlog.New(walFilePath)
	if err != nil {
		panic(err)
	}
	return wal, walFilePath
}

// TestNewFile tests that a new file has the correct contents and size and that
// loading it from disk also works.
func TestNewFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a uplofile without a partial chunk.
	uploFilePath, _, source, rc, sk, fileSize, numChunks, fileMode := newTestFileParams(1, false)
	sf, _, _ := customTestFileAndWAL(uploFilePath, source, rc, sk, fileSize, numChunks, fileMode)

	// Add pieces to each chunk.
	for chunkIndex := 0; chunkIndex < sf.numChunks; chunkIndex++ {
		for pieceIndex := 0; pieceIndex < sf.ErasureCode().NumPieces(); pieceIndex++ {
			numPieces := fastrand.Intn(3) // up to 2 hosts for each piece
			for i := 0; i < numPieces; i++ {
				pk := types.UploPublicKey{Key: fastrand.Bytes(crypto.EntropySize)}
				mr := crypto.Hash{}
				fastrand.Read(mr[:])
				if err := sf.AddPiece(pk, uint64(chunkIndex), uint64(pieceIndex), mr); err != nil {
					panic(err)
				}
			}
		}
	}

	// Check that StaticPagesPerChunk was set correctly.
	if sf.staticMetadata.StaticPagesPerChunk != numChunkPagesRequired(sf.staticMetadata.staticErasureCode.NumPieces()) {
		t.Fatal("StaticPagesPerChunk wasn't set correctly")
	}

	// Marshal the metadata.
	md, err := marshalMetadata(sf.staticMetadata)
	if err != nil {
		t.Fatal(err)
	}
	// Marshal the pubKeyTable.
	pkt, err := marshalPubKeyTable(sf.pubKeyTable)
	if err != nil {
		t.Fatal(err)
	}
	// Marshal the chunks.
	var chunks [][]byte
	var chunksMarshaled []chunk
	err = sf.iterateChunksReadonly(func(chunk chunk) error {
		c := marshalChunk(chunk)
		chunks = append(chunks, c)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Save the UploFile to make sure cached fields are persisted too.
	if err := sf.saveFile(chunksMarshaled); err != nil {
		t.Fatal(err)
	}

	// Open the file.
	f, err := os.OpenFile(sf.uploFilePath, os.O_RDWR, 777)
	if err != nil {
		t.Fatal("Failed to open file", err)
	}
	defer closeFileInTest(t, f)
	// Check the filesize. It should be equal to the offset of the last chunk
	// on disk + its marshaled length.
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	// If the file only has 1 partial chunk and no full chunk don't do this check.
	if fi.Size() != sf.chunkOffset(sf.numChunks-1)+int64(len(chunks[len(chunks)-1])) {
		t.Fatal("file doesn't have right size")
	}
	// Compare the metadata to the on-disk metadata.
	readMD := make([]byte, len(md))
	_, err = f.ReadAt(readMD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readMD, md) {
		t.Log(string(readMD))
		t.Log(string(md))
		t.Fatal("metadata doesn't equal on-disk metadata")
	}
	// Compare the pubKeyTable to the on-disk pubKeyTable.
	readPKT := make([]byte, len(pkt))
	_, err = f.ReadAt(readPKT, sf.staticMetadata.PubKeyTableOffset)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readPKT, pkt) {
		t.Fatal("pubKeyTable doesn't equal on-disk pubKeyTable")
	}
	// Compare the chunks to the on-disk chunks one-by-one.
	readChunk := make([]byte, int(sf.staticMetadata.StaticPagesPerChunk)*pageSize)
	err = sf.iterateChunksReadonly(func(chunk chunk) error {
		_, err := f.ReadAt(readChunk, sf.chunkOffset(chunk.Index))
		if err != nil && !errors.Contains(err, io.EOF) {
			t.Fatal(err)
		}
		if !bytes.Equal(readChunk[:len(chunks[chunk.Index])], chunks[chunk.Index]) {
			t.Fatal("readChunks don't equal on-disk readChunks")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Load the file from disk and check that they are the same.
	sf2, err := LoadUploFile(sf.uploFilePath, sf.wal)
	if err != nil {
		t.Fatal("failed to load UploFile from disk", err)
	}
	sf2.SetPartialsUploFile(sf.partialsUploFile)
	// Compare the files.
	if err := equalFiles(sf, sf2); err != nil {
		t.Fatal(err)
	}
}

// TestCreateReadInsertUpdate tests if an update can be created using createInsertUpdate
// and if the created update can be read using readInsertUpdate.
func TestCreateReadInsertUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newTestFile()
	// Create update randomly
	index := int64(fastrand.Intn(100))
	data := fastrand.Bytes(10)
	update := sf.createInsertUpdate(index, data)
	// Read update
	readPath, readIndex, readData, err := readInsertUpdate(update)
	if err != nil {
		t.Fatal("Failed to read update", err)
	}
	// Compare values
	if readPath != sf.uploFilePath {
		t.Error("paths doesn't match")
	}
	if readIndex != index {
		t.Error("index doesn't match")
	}
	if !bytes.Equal(readData, data) {
		t.Error("data doesn't match")
	}
}

// TestCreateReadDeleteUpdate tests if an update can be created using
// createDeleteUpdate and if the created update can be read using
// readDeleteUpdate.
func TestCreateReadDeleteUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newTestFile()
	update := sf.createDeleteUpdate()
	// Read update
	path := readDeleteUpdate(update)
	// Compare values
	if path != sf.uploFilePath {
		t.Error("paths doesn't match")
	}
}

// TestDelete tests if deleting a uplofile removes the file from disk and sets
// the deleted flag correctly.
func TestDelete(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create UploFileSet with UploFile
	entry := newTestFile()
	// Delete file.
	if err := entry.Delete(); err != nil {
		t.Fatal("Failed to delete file", err)
	}
	// Check if file was deleted and if deleted flag was set.
	if !entry.Deleted() {
		t.Fatal("Deleted flag was not set correctly")
	}
	if _, err := os.Open(entry.uploFilePath); !os.IsNotExist(err) {
		t.Fatal("Expected a file doesn't exist error but got", err)
	}
}

// TestRename tests if renaming a uplofile moves the file correctly and also
// updates the metadata.
func TestRename(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create UploFileSet with UploFile
	entry := newTestFile()

	// Create new paths for the file.
	oldUploFilePath := entry.UploFilePath()
	newUploFilePath := strings.TrimSuffix(entry.UploFilePath(), modules.UploFileExtension) + "_renamed" + modules.UploFileExtension

	// Rename file
	if err := entry.Rename(newUploFilePath); err != nil {
		t.Fatal("Failed to rename file", err)
	}

	// Check if the file was moved.
	if _, err := os.Open(oldUploFilePath); !os.IsNotExist(err) {
		t.Fatal("Expected a file doesn't exist error but got", err)
	}
	f, err := os.Open(newUploFilePath)
	if err != nil {
		t.Fatal("Failed to open file at new location", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// Check the metadata.
	if entry.uploFilePath != newUploFilePath {
		t.Fatal("UploFilePath wasn't updated correctly")
	}
	if entry.UploFilePath() != newUploFilePath {
		t.Fatal("UploPath wasn't updated correctly", entry.UploFilePath(), newUploFilePath)
	}
}

// TestApplyUpdates tests a variety of functions that are used to apply
// updates.
func TestApplyUpdates(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	t.Run("TestApplyUpdates", func(t *testing.T) {
		uploFile := newTestFile()
		testApply(t, uploFile, ApplyUpdates)
	})
	t.Run("TestUploFileApplyUpdates", func(t *testing.T) {
		uploFile := newTestFile()
		testApply(t, uploFile, uploFile.applyUpdates)
	})
	t.Run("TestCreateAndApplyTransaction", func(t *testing.T) {
		uploFile := newTestFile()
		testApply(t, uploFile, uploFile.createAndApplyTransaction)
	})
}

// TestZeroByteFileCompat checks that 0-byte uplofiles that have been uploaded
// before caching was introduced have the correct cached values after being
// loaded.
func TestZeroByteFileCompat(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create the file.
	uploFilePath, _, source, rc, sk, _, _, fileMode := newTestFileParams(1, true)
	sf, wal, _ := customTestFileAndWAL(uploFilePath, source, rc, sk, 0, 0, fileMode)
	// Check that the number of chunks in the file is correct.
	if sf.numChunks != 0 {
		panic(fmt.Sprintf("newTestFile didn't create the expected number of chunks: %v", sf.numChunks))
	}
	// Set the cached fields to 0 like they would be if the file was already
	// uploaded before caching was introduced.
	sf.staticMetadata.CachedHealth = 0
	sf.staticMetadata.CachedStuckHealth = 0
	sf.staticMetadata.CachedRedundancy = 0
	sf.staticMetadata.CachedUserRedundancy = 0
	sf.staticMetadata.CachedUploadProgress = 0
	// Save the file and reload it.
	if err := sf.SaveMetadata(); err != nil {
		t.Fatal(err)
	}
	sf, err := loadUploFile(uploFilePath, wal, modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	// Make sure the loaded file has the correct cached values.
	if sf.staticMetadata.CachedHealth != 0 {
		t.Fatalf("CachedHealth should be 0 but was %v", sf.staticMetadata.CachedHealth)
	}
	if sf.staticMetadata.CachedStuckHealth != 0 {
		t.Fatalf("CachedStuckHealth should be 0 but was %v", sf.staticMetadata.CachedStuckHealth)
	}
	expectedRedundancy := float64(rc.NumPieces()) / float64(rc.MinPieces())
	if sf.staticMetadata.CachedRedundancy != expectedRedundancy {
		t.Fatalf("CachedRedundancy should be %v but was %v", expectedRedundancy, sf.staticMetadata.CachedRedundancy)
	}
	if sf.staticMetadata.CachedUserRedundancy != expectedRedundancy {
		t.Fatalf("CachedRedundancy should be %v but was %v", expectedRedundancy, sf.staticMetadata.CachedUserRedundancy)
	}
	if sf.staticMetadata.CachedUploadProgress != 100 {
		t.Fatalf("CachedUploadProgress should be 100 but was %v", sf.staticMetadata.CachedUploadProgress)
	}
}

// TestSaveSmallHeader tests the saveHeader method for a header that is not big
// enough to need more than a single page on disk.
func TestSaveSmallHeader(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newBlankTestFile()

	// Add some host keys.
	sf.addRandomHostKeys(10)

	// Save the header.
	updates, err := sf.saveHeaderUpdates()
	if err != nil {
		t.Fatal("Failed to create updates to save header", err)
	}
	if err := sf.createAndApplyTransaction(updates...); err != nil {
		t.Fatal("Failed to save header", err)
	}

	// Manually open the file to check its contents.
	f, err := os.Open(sf.uploFilePath)
	if err != nil {
		t.Fatal("Failed to open file", err)
	}
	defer closeFileInTest(t, f)

	// Make sure the metadata was written to disk correctly.
	rawMetadata, err := marshalMetadata(sf.staticMetadata)
	if err != nil {
		t.Fatal("Failed to marshal metadata", err)
	}
	readMetadata := make([]byte, len(rawMetadata))
	if _, err := f.ReadAt(readMetadata, 0); err != nil {
		t.Fatal("Failed to read metadata", err)
	}
	if !bytes.Equal(rawMetadata, readMetadata) {
		fmt.Println(string(rawMetadata))
		fmt.Println(string(readMetadata))
		t.Fatal("Metadata on disk doesn't match marshaled metadata")
	}

	// Make sure that the pubKeyTable was written to disk correctly.
	rawPubKeyTAble, err := marshalPubKeyTable(sf.pubKeyTable)
	if err != nil {
		t.Fatal("Failed to marshal pubKeyTable", err)
	}
	readPubKeyTable := make([]byte, len(rawPubKeyTAble))
	if _, err := f.ReadAt(readPubKeyTable, sf.staticMetadata.PubKeyTableOffset); err != nil {
		t.Fatal("Failed to read pubKeyTable", err)
	}
	if !bytes.Equal(rawPubKeyTAble, readPubKeyTable) {
		t.Fatal("pubKeyTable on disk doesn't match marshaled pubKeyTable")
	}
}

// TestSaveLargeHeader tests the saveHeader method for a header that uses more than a single page on disk and forces a call to allocateHeaderPage
func TestSaveLargeHeader(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newBlankTestFile()

	// Add some host keys. This should force the UploFile to allocate a new page
	// for the pubKeyTable.
	sf.addRandomHostKeys(100)

	// Open the file.
	f, err := os.OpenFile(sf.uploFilePath, os.O_RDWR, 777)
	if err != nil {
		t.Fatal("Failed to open file", err)
	}
	defer closeFileInTest(t, f)

	// Write some data right after the ChunkOffset as a checksum.
	chunkData := fastrand.Bytes(100)
	_, err = f.WriteAt(chunkData, sf.staticMetadata.ChunkOffset)
	if err != nil {
		t.Fatal("Failed to write random chunk data", err)
	}

	// Save the header.
	updates, err := sf.saveHeaderUpdates()
	if err != nil {
		t.Fatal("Failed to create updates to save header", err)
	}
	if err := sf.createAndApplyTransaction(updates...); err != nil {
		t.Fatal("Failed to save header", err)
	}

	// Make sure the chunkOffset was updated correctly.
	if sf.staticMetadata.ChunkOffset != 2*pageSize {
		t.Fatal("ChunkOffset wasn't updated correctly", sf.staticMetadata.ChunkOffset, 2*pageSize)
	}

	// Make sure that the checksum was moved correctly.
	readChunkData := make([]byte, len(chunkData))
	if _, err := f.ReadAt(readChunkData, sf.staticMetadata.ChunkOffset); err != nil {
		t.Fatal("Checksum wasn't moved correctly")
	}

	// Make sure the metadata was written to disk correctly.
	rawMetadata, err := marshalMetadata(sf.staticMetadata)
	if err != nil {
		t.Fatal("Failed to marshal metadata", err)
	}
	readMetadata := make([]byte, len(rawMetadata))
	if _, err := f.ReadAt(readMetadata, 0); err != nil {
		t.Fatal("Failed to read metadata", err)
	}
	if !bytes.Equal(rawMetadata, readMetadata) {
		fmt.Println(string(rawMetadata))
		fmt.Println(string(readMetadata))
		t.Fatal("Metadata on disk doesn't match marshaled metadata")
	}

	// Make sure that the pubKeyTable was written to disk correctly.
	rawPubKeyTAble, err := marshalPubKeyTable(sf.pubKeyTable)
	if err != nil {
		t.Fatal("Failed to marshal pubKeyTable", err)
	}
	readPubKeyTable := make([]byte, len(rawPubKeyTAble))
	if _, err := f.ReadAt(readPubKeyTable, sf.staticMetadata.PubKeyTableOffset); err != nil {
		t.Fatal("Failed to read pubKeyTable", err)
	}
	if !bytes.Equal(rawPubKeyTAble, readPubKeyTable) {
		t.Fatal("pubKeyTable on disk doesn't match marshaled pubKeyTable")
	}
}

// testApply tests if a given method applies a set of updates correctly.
func testApply(t *testing.T, uploFile *UploFile, apply func(...writeaheadlog.Update) error) {
	// Create an update that writes random data to a random index i.
	index := fastrand.Intn(100) + 1
	data := fastrand.Bytes(100)
	update := uploFile.createInsertUpdate(int64(index), data)

	// Apply update.
	if err := apply(update); err != nil {
		t.Fatal("Failed to apply update", err)
	}
	// Open file.
	file, err := os.Open(uploFile.uploFilePath)
	if err != nil {
		t.Fatal("Failed to open file", err)
	}
	// Check if correct data was written.
	readData := make([]byte, len(data))
	if _, err := file.ReadAt(readData, int64(index)); err != nil {
		t.Fatal("Failed to read written data back from disk", err)
	}
	if !bytes.Equal(data, readData) {
		t.Fatal("Read data doesn't equal written data")
	}
}

// TestUpdateUsedHosts tests the UpdateUsedHosts method.
func TestUpdateUsedHosts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newBlankTestFile()
	sf.addRandomHostKeys(10)

	// All the host keys should be used.
	for _, entry := range sf.pubKeyTable {
		if !entry.Used {
			t.Fatal("all hosts are expected to be used at the beginning of the test")
		}
	}

	// Report only half the hosts as still being used.
	var used []types.UploPublicKey
	for i, entry := range sf.pubKeyTable {
		if i%2 == 0 {
			used = append(used, entry.PublicKey)
		}
	}
	if err := sf.UpdateUsedHosts(used); err != nil {
		t.Fatal("failed to update hosts", err)
	}

	// Create a map of the used keys for faster lookups.
	usedMap := make(map[string]struct{})
	for _, key := range used {
		usedMap[key.String()] = struct{}{}
	}

	// Check that the flag was set correctly.
	for _, entry := range sf.pubKeyTable {
		_, exists := usedMap[entry.PublicKey.String()]
		if entry.Used != exists {
			t.Errorf("expected flag to be %v but was %v", exists, entry.Used)
		}
	}

	// Reload the uplofile to see if the flags were also persisted.
	var err error
	sf, err = LoadUploFile(sf.uploFilePath, sf.wal)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the flags are still set correctly.
	for _, entry := range sf.pubKeyTable {
		_, exists := usedMap[entry.PublicKey.String()]
		if entry.Used != exists {
			t.Errorf("expected flag to be %v but was %v", exists, entry.Used)
		}
	}

	// Also check the flags in order. Making sure that persisting them didn't
	// change the order.
	for i, entry := range sf.pubKeyTable {
		expectedUsed := i%2 == 0
		if entry.Used != expectedUsed {
			t.Errorf("expected flag to be %v but was %v", expectedUsed, entry.Used)
		}
	}
}

// TestChunkOffset tests the chunkOffset method.
func TestChunkOffset(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newTestFile()

	// Set the static pages per chunk to a random value.
	sf.staticMetadata.StaticPagesPerChunk = uint8(fastrand.Intn(5)) + 1

	// Calculate the offset of the first chunk.
	offset1 := sf.chunkOffset(0)
	if expectedOffset := sf.staticMetadata.ChunkOffset; expectedOffset != offset1 {
		t.Fatalf("expected offset %v but got %v", sf.staticMetadata.ChunkOffset, offset1)
	}

	// Calculate the offset of the second chunk.
	offset2 := sf.chunkOffset(1)
	if expectedOffset := offset1 + int64(sf.staticMetadata.StaticPagesPerChunk)*pageSize; expectedOffset != offset2 {
		t.Fatalf("expected offset %v but got %v", expectedOffset, offset2)
	}

	// Make sure that the offsets we calculated are not the same due to not
	// initializing the file correctly.
	if offset2 == offset1 {
		t.Fatal("the calculated offsets are the same")
	}
}

// TestSaveChunk checks that saveChunk creates an updated which if applied
// writes the correct data to disk.
func TestSaveChunk(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sf := newTestFile()

	// Choose a random chunk from the file and replace it.
	chunkIndex := fastrand.Intn(sf.numChunks)
	chunk := randomChunk()
	chunk.Index = chunkIndex

	// Write the chunk to disk using saveChunk.
	update := sf.saveChunkUpdate(chunk)
	if err := sf.createAndApplyTransaction(update); err != nil {
		t.Fatal(err)
	}

	// Marshal the chunk.
	marshaledChunk := marshalChunk(chunk)

	// Read the chunk from disk.
	f, err := os.Open(sf.uploFilePath)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFileInTest(t, f)

	readChunk := make([]byte, len(marshaledChunk))
	if n, err := f.ReadAt(readChunk, sf.chunkOffset(chunkIndex)); err != nil {
		t.Fatal(err, n, len(marshaledChunk))
	}

	// The marshaled chunk should equal the chunk we read from disk.
	if !bytes.Equal(readChunk, marshaledChunk) {
		t.Fatal("marshaled chunk doesn't equal chunk on disk", len(readChunk), len(marshaledChunk))
	}
}

// TestUniqueIDMissing makes sure that loading a uplofile sets the unique id in
// the metadata if it wasn't set before.
func TestUniqueIDMissing(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a new file.
	sf, wal, _ := newBlankTestFileAndWAL(1)
	// It should have a UID.
	if sf.staticMetadata.UniqueID == "" {
		t.Fatal("unique ID wasn't set")
	}
	// Set the UID to a blank string and save the file.
	sf.staticMetadata.UniqueID = ""
	updates, err := sf.saveMetadataUpdates()
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.createAndApplyTransaction(updates...); err != nil {
		t.Fatal(err)
	}
	// Load the file again.
	sf, err = LoadUploFile(sf.uploFilePath, wal)
	if err != nil {
		t.Fatal(err)
	}
	// It should have a UID now.
	if sf.staticMetadata.UniqueID == "" {
		t.Fatal("unique ID wasn't set after loading file")
	}
}

// TestSetCombinedChunkSingle tests SetCombinedChunk for a partial chunk with a
// single combined chunk.
func TestSetCombinedChunkSingle(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	t.Skip("skip until partial chunks are enabled")

	// Create two UploFiles with partial chunks and link them by giving the second
	// one the same partials uplofile as the first one.
	sf, _, _ := newBlankTestFileAndWAL(1)
	sf2, _, _ := newBlankTestFileAndWAL(1)
	sf2.SetPartialsUploFile(sf.partialsUploFile)

	// Calculate the partial chunks' sizes.
	partialChunkSize := int64(sf.Size() % sf.ChunkSize())
	if partialChunkSize == 0 {
		t.Fatal("no partial chunk at end of file")
	}
	partialChunkSize2 := int64(sf2.Size() % sf2.ChunkSize())
	if partialChunkSize2 == 0 {
		t.Fatal("no partial chunk at end of file2")
	}

	// Set the combined chunk of the first file to have offset 0.
	cid := modules.CombinedChunkID("chunkid")
	partialChunks := []modules.PartialChunk{
		{
			ChunkID:        cid,
			InPartialsFile: false,
			Length:         uint64(partialChunkSize),
			Offset:         0,
		},
	}
	if err := sf.SetPartialChunks(partialChunks, nil); err != nil {
		t.Fatal(err)
	}
	// The metadata of the uplofile should be set correctly now.
	ccs := sf.staticMetadata.PartialChunks
	if len(sf.staticMetadata.PartialChunks) != 1 {
		t.Fatal("expected exactly 1 combined chunk but got",
			len(sf.staticMetadata.PartialChunks))
	}
	if ccs[0].ID != cid {
		t.Fatal("combined chunk id doesn't match expected id",
			ccs[0].ID, cid)
	}
	if ccs[0].Index != 0 {
		t.Fatal("expected chunk index to be 0 but was",
			ccs[0].Index)
	}
	if ccs[0].Length != uint64(partialChunkSize) {
		t.Fatal("wrong combinedchunklength",
			ccs[0].Length, partialChunkSize)
	}
	if ccs[0].Offset != 0 {
		t.Fatal("wrong combinedchunkoffset",
			ccs[0].Offset, 0)
	}
	if ccs[0].Status != CombinedChunkStatusInComplete {
		t.Fatal("wrong combinedchunkstatus",
			ccs[0].Status, CombinedChunkStatusInComplete)
	}
	// The partials uplofile should have one chunk now.
	if sf.partialsUploFile.NumChunks() != 1 {
		t.Fatal("expected partialsUploFile to have one chunk but had", sf.partialsUploFile.NumChunks())
	}

	// The first combined chunk's HasPartialsChunk can be set to 'true' now since
	// it was added to the partials file.
	partialChunks[0].InPartialsFile = true
	// The second chunk should start at an offset at the end of the combined chunk
	// to force it to be spread across 2 combined chunks.
	cid2 := modules.CombinedChunkID("chunkid2")
	partialChunks = []modules.PartialChunk{
		{
			ChunkID:        cid,
			InPartialsFile: true,
			Length:         1,
			Offset:         uint64(sf2.ChunkSize() - 1),
		},
		{
			ChunkID:        cid2,
			InPartialsFile: false,
			Length:         uint64(partialChunkSize2) - 1,
			Offset:         0,
		},
	}
	// Set the combined chunk of the second file to have offset chunkSize-1.
	if err := sf2.SetPartialChunks(partialChunks, nil); err != nil {
		t.Fatal(err)
	}
	// The metadata of the second uplofile should be set correctly now.
	ccs2 := sf2.staticMetadata.PartialChunks
	if len(ccs2) != 2 {
		t.Fatal("expected exactly 2 combined chunks but got",
			len(ccs2))
	}
	if ccs2[0].ID != cid {
		t.Fatal("combined chunk id doesn't match expected id",
			ccs2[0].ID, cid)
	}
	if ccs2[1].ID != cid2 {
		t.Fatal("combined chunk id doesn't match expected id",
			ccs2[0].ID, cid)
	}
	if ccs2[0].Index != 0 {
		t.Fatal("expected chunk index to be 0 but was",
			ccs2[0].Index)
	}
	if ccs2[1].Index != 1 {
		t.Fatal("expected chunk index to be 1 but was",
			ccs2[1].Index)
	}
	if ccs2[0].Length+ccs2[1].Length != uint64(partialChunkSize2) {
		t.Fatal("wrong combinedchunklength",
			ccs2[0].Length, partialChunkSize)
	}
	if ccs2[0].Offset != sf2.ChunkSize()-1 {
		t.Fatal("wrong combinedchunkoffset",
			ccs2[0].Offset, 0)
	}
	if ccs2[0].Status != CombinedChunkStatusInComplete {
		t.Fatal("wrong combinedchunkstatus",
			ccs2[0].Status, CombinedChunkStatusInComplete)
	}
	// The partials uplofile should have two chunks now.
	if sf.partialsUploFile.NumChunks() != 2 {
		t.Fatal("expected partialsUploFile to have two chunks but had", sf.partialsUploFile.NumChunks())
	}
	if sf2.partialsUploFile.NumChunks() != 2 {
		t.Fatal("expected partialsUploFile to have two chunks but had", sf.partialsUploFile.NumChunks())
	}
}

// TestCreateAndApplyTransactionPanic verifies that the
// createAndApplyTransaction helpers panic when the updates can't be applied.
func TestCreateAndApplyTransactionPanic(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create invalid update that triggers a panic.
	update := writeaheadlog.Update{
		Name: "invalid name",
	}

	// Declare a helper to check for a panic.
	assertRecover := func() {
		if r := recover(); r == nil {
			t.Fatalf("Expected a panic")
		}
	}

	// Run the test for both the method and function
	sf := newBlankTestFile()
	func() {
		defer assertRecover()
		_ = sf.createAndApplyTransaction(update)
	}()
	func() {
		defer assertRecover()
		_ = createAndApplyTransaction(sf.wal, update)
	}()
}

// TestDeleteUpdateRegression is a regression test that ensure apply updates
// won't panic when called with a set of updates with the last one being
// a delete update.
func TestDeleteUpdateRegression(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create uplofile
	sf := newBlankTestFile()

	// Apply updates with the last update as a delete update. This use to trigger
	// a panic. No need to check the return value as we are only concerned with the
	// panic
	update := sf.createDeleteUpdate()
	sf.createAndApplyTransaction(update, update)
}
