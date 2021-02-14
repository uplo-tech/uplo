package renter

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem"
	"github.com/uplo-tech/uplo/persist"
)

// newUploPath returns a new UploPath for testing and panics on error
func newUploPath(str string) modules.UploPath {
	sp, err := modules.NewUploPath(str)
	if err != nil {
		panic(err)
	}
	return sp
}

// createRenterTestFile creates a test file when the test has a renter so that the
// file is properly added to the renter. It returns the UploFileSetEntry that the
// UploFile is stored in
func (r *Renter) createRenterTestFile(uploPath modules.UploPath) (*filesystem.FileNode, error) {
	// Generate erasure coder
	_, rsc := testingFileParams()
	return r.createRenterTestFileWithParams(uploPath, rsc, crypto.RandomCipherType())
}

// createRenterTestFileWithParams creates a test file when the test has a renter
// so that the file is properly added to the renter. It returns the
// UploFileSetEntry that the UploFile is stored in
func (r *Renter) createRenterTestFileWithParams(uploPath modules.UploPath, rsc modules.ErasureCoder, ct crypto.CipherType) (*filesystem.FileNode, error) {
	// create the renter/files dir if it doesn't exist
	uploFilePath := r.staticFileSystem.FilePath(uploPath)
	dir, _ := filepath.Split(uploFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// Create File
	up := modules.FileUploadParams{
		Source:      "",
		UploPath:     uploPath,
		ErasureCode: rsc,
	}
	err := r.staticFileSystem.NewUploFile(up.UploPath, up.Source, up.ErasureCode, crypto.GenerateUploKey(ct), 1000, persist.DefaultDiskPermissionsTest, false)
	if err != nil {
		return nil, err
	}
	return r.staticFileSystem.OpenUploFile(up.UploPath)
}

// newRenterTestFile creates a test file when the test has a renter so that the
// file is properly added to the renter. It returns the UploFileSetEntry that the
// UploFile is stored in
func (r *Renter) newRenterTestFile() (*filesystem.FileNode, error) {
	// Generate name and erasure coding
	uploPath, rsc := testingFileParams()
	return r.createRenterTestFileWithParams(uploPath, rsc, crypto.RandomCipherType())
}

// TestRenterFileListLocalPath verifies that FileList() returns the correct
// local path information for an uploaded file.
func TestRenterFileListLocalPath(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	id := rt.renter.mu.Lock()
	entry, _ := rt.renter.newRenterTestFile()
	if err := entry.SetLocalPath("TestPath"); err != nil {
		t.Fatal(err)
	}
	rt.renter.mu.Unlock(id)
	files, err := rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("wrong number of files, got", len(files), "wanted one")
	}
	if files[0].LocalPath != "TestPath" {
		t.Fatal("file had wrong LocalPath: got", files[0].LocalPath, "wanted TestPath")
	}
}

// TestRenterDeleteFile probes the DeleteFile method of the renter type.
func TestRenterDeleteFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Delete a file from an empty renter.
	uploPath, err := modules.NewUploPath("dne")
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.DeleteFile(uploPath)
	// NOTE: using strings.Contains because errors.Contains does not recognize
	// errors when errors.Extend is used
	if !strings.Contains(err.Error(), filesystem.ErrNotExist.Error()) {
		t.Errorf("Expected error to contain %v but got '%v'", filesystem.ErrNotExist, err)
	}

	// Put a file in the renter.
	entry, err := rt.renter.newRenterTestFile()
	if err != nil {
		t.Fatal(err)
	}
	// Delete a different file.
	uploPathOne, err := modules.NewUploPath("one")
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.DeleteFile(uploPathOne)
	// NOTE: using strings.Contains because errors.Contains does not recognize
	// errors when errors.Extend is used
	if !strings.Contains(err.Error(), filesystem.ErrNotExist.Error()) {
		t.Errorf("Expected error to contain %v but got '%v'", filesystem.ErrNotExist, err)
	}
	// Delete the file.
	uplopath := rt.renter.staticFileSystem.FileUploPath(entry)

	if err := entry.Close(); err != nil {
		t.Fatal(err)
	}
	err = rt.renter.DeleteFile(uplopath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Error("file was deleted, but is still reported in FileList")
	}
	// Confirm that file was removed from UploFileSet
	_, err = rt.renter.staticFileSystem.OpenUploFile(uplopath)
	if err == nil {
		t.Fatal("Deleted file still found in staticFileSet")
	}

	// Put a file in the renter, then rename it.
	entry2, err := rt.renter.newRenterTestFile()
	if err != nil {
		t.Fatal(err)
	}
	uploPath1, err := modules.NewUploPath("1")
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(rt.renter.staticFileSystem.FileUploPath(entry2), uploPath1) // set name to "1"
	if err != nil {
		t.Fatal(err)
	}
	uplopath2 := rt.renter.staticFileSystem.FileUploPath(entry2)
	entry2.Close()
	uplopath2 = rt.renter.staticFileSystem.FileUploPath(entry2)
	err = rt.renter.RenameFile(uplopath2, uploPathOne)
	if err != nil {
		t.Fatal(err)
	}
	// Call delete on the previous name.
	err = rt.renter.DeleteFile(uploPath1)
	// NOTE: using strings.Contains because errors.Contains does not recognize
	// errors when errors.Extend is used
	if !strings.Contains(err.Error(), filesystem.ErrNotExist.Error()) {
		t.Errorf("Expected error to contain %v but got '%v'", filesystem.ErrNotExist, err)
	}
	// Call delete on the new name.
	err = rt.renter.DeleteFile(uploPathOne)
	if err != nil {
		t.Error(err)
	}

	// Check that all .uplo files have been deleted.
	var walkStr string
	rt.renter.staticFileSystem.Walk(modules.RootUploPath(), func(path string, _ os.FileInfo, _ error) error {
		// capture only .uplo files
		if filepath.Ext(path) == ".uplo" {
			rel, _ := filepath.Rel(rt.renter.staticFileSystem.Root(), path) // strip testdir prefix
			walkStr += rel
		}
		return nil
	})
	expWalkStr := ""
	if walkStr != expWalkStr {
		t.Fatalf("Bad walk string: expected %q, got %q", expWalkStr, walkStr)
	}
}

// TestRenterDeleteFileMissingParent tries to delete a file for which the parent
// has been deleted before.
func TestRenterDeleteFileMissingParent(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Put a file in the renter.
	uploPath, err := modules.NewUploPath("parent/file")
	if err != nil {
		t.Fatal(err)
	}
	dirUploPath, err := uploPath.Dir()
	if err != nil {
		t.Fatal(err)
	}
	uploPath, rsc := testingFileParams()
	up := modules.FileUploadParams{
		Source:      "",
		UploPath:     uploPath,
		ErasureCode: rsc,
	}
	err = rt.renter.staticFileSystem.NewUploFile(up.UploPath, up.Source, up.ErasureCode, crypto.GenerateUploKey(crypto.RandomCipherType()), 1000, persist.DefaultDiskPermissionsTest, false)
	if err != nil {
		t.Fatal(err)
	}
	// Delete the parent.
	err = rt.renter.staticFileSystem.DeleteFile(dirUploPath)
	// NOTE: using strings.Contains because errors.Contains does not recognize
	// errors when errors.Extend is used
	if !strings.Contains(err.Error(), filesystem.ErrNotExist.Error()) {
		t.Errorf("Expected error to contain %v but got '%v'", filesystem.ErrNotExist, err)
	}
	// Delete the file. This should not return an error since it's already
	// deleted implicitly.
	if err := rt.renter.staticFileSystem.DeleteFile(up.UploPath); err != nil {
		t.Fatal(err)
	}
}

// TestRenterFileList probes the FileList method of the renter type.
func TestRenterFileList(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Get the file list of an empty renter.
	files, err := rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("FileList has non-zero length for empty renter?")
	}

	// Put a file in the renter.
	entry1, _ := rt.renter.newRenterTestFile()
	files, err = rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("FileList is not returning the only file in the renter")
	}
	entry1SP := rt.renter.staticFileSystem.FileUploPath(entry1)
	if !files[0].UploPath.Equals(entry1SP) {
		t.Error("FileList is not returning the correct filename for the only file")
	}

	// Put multiple files in the renter.
	entry2, _ := rt.renter.newRenterTestFile()
	entry2SP := rt.renter.staticFileSystem.FileUploPath(entry2)
	files, err = rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("Expected %v files, got %v", 2, len(files))
	}
	files, err = rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !((files[0].UploPath.Equals(entry1SP) || files[0].UploPath.Equals(entry2SP)) &&
		(files[1].UploPath.Equals(entry1SP) || files[1].UploPath.Equals(entry2SP)) &&
		(files[0].UploPath != files[1].UploPath)) {
		t.Log("files[0].UploPath", files[0].UploPath)
		t.Log("files[1].UploPath", files[1].UploPath)
		t.Log("file1.UploPath()", rt.renter.staticFileSystem.FileUploPath(entry1).String())
		t.Log("file2.UploPath()", rt.renter.staticFileSystem.FileUploPath(entry2).String())
		t.Error("FileList is returning wrong names for the files")
	}
}

// TestRenterRenameFile probes the rename method of the renter.
func TestRenterRenameFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Rename a file that doesn't exist.
	uploPath1, err := modules.NewUploPath("1")
	if err != nil {
		t.Fatal(err)
	}
	uploPath1a, err := modules.NewUploPath("1a")
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(uploPath1, uploPath1a)
	if err.Error() != filesystem.ErrNotExist.Error() {
		t.Errorf("Expected '%v' got '%v'", filesystem.ErrNotExist, err)
	}

	// Get the filesystem.
	sfs := rt.renter.staticFileSystem

	// Rename a file that does exist.
	entry, _ := rt.renter.newRenterTestFile()
	var sp modules.UploPath
	if err := sp.FromSysPath(entry.UploFilePath(), sfs.DirPath(modules.RootUploPath())); err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(sp, uploPath1)
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(uploPath1, uploPath1a)
	if err != nil {
		t.Fatal(err)
	}
	files, err := rt.renter.FileListCollect(modules.RootUploPath(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("FileList has unexpected number of files:", len(files))
	}
	if !files[0].UploPath.Equals(uploPath1a) {
		t.Errorf("RenameFile failed: expected %v, got %v", uploPath1a.String(), files[0].UploPath)
	}
	// Confirm UploFileSet was updated
	_, err = rt.renter.staticFileSystem.OpenUploFile(uploPath1a)
	if err != nil {
		t.Fatal("renter staticFileSet not updated to new file name:", err)
	}
	_, err = rt.renter.staticFileSystem.OpenUploFile(uploPath1)
	if err == nil {
		t.Fatal("old name not removed from renter staticFileSet")
	}
	// Rename a file to an existing name.
	entry2, err := rt.renter.newRenterTestFile()
	if err != nil {
		t.Fatal(err)
	}
	var sp2 modules.UploPath
	if err := sp2.FromSysPath(entry2.UploFilePath(), sfs.DirPath(modules.RootUploPath())); err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(sp2, uploPath1) // Rename to "1"
	if err != nil {
		t.Fatal(err)
	}
	entry2.Close()
	err = rt.renter.RenameFile(uploPath1, uploPath1a)
	if !errors.Contains(err, filesystem.ErrExists) {
		t.Fatal("Expecting ErrExists, got", err)
	}
	// Rename a file to the same name.
	err = rt.renter.RenameFile(uploPath1, uploPath1)
	if !errors.Contains(err, filesystem.ErrExists) {
		t.Fatal("Expecting ErrExists, got", err)
	}

	// Confirm ability to rename file
	uploPath1b, err := modules.NewUploPath("1b")
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(uploPath1, uploPath1b)
	if err != nil {
		t.Fatal(err)
	}
	// Rename file that would create a directory
	uploPathWithDir, err := modules.NewUploPath("new/name/with/dir/test")
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.RenameFile(uploPath1b, uploPathWithDir)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm directory metadatas exist
	for !uploPathWithDir.Equals(modules.RootUploPath()) {
		uploPathWithDir, err = uploPathWithDir.Dir()
		if err != nil {
			t.Fatal(err)
		}
		_, err = rt.renter.staticFileSystem.Openuplodir(uploPathWithDir)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestRenterFileDir tests that the renter files are uploaded to the files
// directory and not the root directory of the renter.
func TestRenterFileDir(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create local file to upload
	localDir := filepath.Join(rt.dir, "files")
	if err := os.MkdirAll(localDir, 0700); err != nil {
		t.Fatal(err)
	}
	size := 100
	fileName := fmt.Sprintf("%dbytes %s", size, hex.EncodeToString(fastrand.Bytes(4)))
	source := filepath.Join(localDir, fileName)
	bytes := fastrand.Bytes(size)
	if err := ioutil.WriteFile(source, bytes, 0600); err != nil {
		t.Fatal(err)
	}

	// Upload local file
	ec := modules.NewRSCodeDefault()
	uploPath, err := modules.NewUploPath(fileName)
	if err != nil {
		t.Fatal(err)
	}
	params := modules.FileUploadParams{
		Source:      source,
		UploPath:     uploPath,
		ErasureCode: ec,
	}
	err = rt.renter.Upload(params)
	if err != nil {
		t.Fatal("failed to upload file:", err)
	}

	// Get file and check uplopath
	f, err := rt.renter.File(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	if !f.UploPath.Equals(uploPath) {
		t.Fatalf("uplopath not set as expected: got %v expected %v", f.UploPath, fileName)
	}

	// Confirm .uplo file exists on disk in the UplopathRoot directory
	renterDir := filepath.Join(rt.dir, modules.RenterDir)
	uplopathRootDir := filepath.Join(renterDir, modules.FileSystemRoot)
	fullPath := uploPath.UploFileSysPath(uplopathRootDir)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Fatal("No .uplo file found on disk")
	}
}
