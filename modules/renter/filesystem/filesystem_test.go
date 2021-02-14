package filesystem

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplodir"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplofile"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/build"
)

// CachedListCollect calls CachedList but collects the returned infos into
// slices which are then sorted by uplopath. This should only be used in testing
// since it might result in a large memory allocation.
func (fs *FileSystem) CachedListCollect(uploPath modules.UploPath, recursive bool) (fis []modules.FileInfo, dis []modules.DirectoryInfo, err error) {
	var fmu, dmu sync.Mutex
	flf := func(fi modules.FileInfo) {
		fmu.Lock()
		fis = append(fis, fi)
		fmu.Unlock()
	}
	dlf := func(di modules.DirectoryInfo) {
		dmu.Lock()
		dis = append(dis, di)
		dmu.Unlock()
	}
	err = fs.CachedList(uploPath, recursive, flf, dlf)

	// Sort slices by UploPath.
	sort.Slice(dis, func(i, j int) bool {
		return dis[i].UploPath.String() < dis[j].UploPath.String()
	})
	sort.Slice(fis, func(i, j int) bool {
		return fis[i].UploPath.String() < fis[j].UploPath.String()
	})
	return
}

// newTestFileSystemWithFile creates a new FileSystem and UploFile and makes sure
// that they are linked
func newTestFileSystemWithFile(name string) (*FileNode, *FileSystem, error) {
	dir := testDir(name)
	fs := newTestFileSystem(dir)
	sp := modules.RandomUploPath()
	fs.addTestUploFile(sp)
	sf, err := fs.OpenUploFile(sp)
	return sf, fs, err
}

// newTestFileSystemWithDir creates a new FileSystem and uplodir and makes sure
// that they are linked
func newTestFileSystemWithDir(name string) (*DirNode, *FileSystem, error) {
	dir := testDir(name)
	fs := newTestFileSystem(dir)
	sp := modules.RandomUploPath()
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); err != nil {
		panic(err) // Reflect behavior of newTestFileSystemWithFile.
	}
	sd, err := fs.Openuplodir(sp)
	return sd, fs, err
}

// testDir creates a testing directory for a filesystem test.
func testDir(name string) string {
	dir := build.TempDir(name, filepath.Join("filesystem"))
	if err := os.MkdirAll(dir, persist.DefaultDiskPermissionsTest); err != nil {
		panic(err)
	}
	return dir
}

// newUploPath creates a new uplopath from the specified string.
func newUploPath(path string) modules.UploPath {
	sp, err := modules.NewUploPath(path)
	if err != nil {
		panic(err)
	}
	return sp
}

// newTestFileSystem creates a new filesystem for testing.
func newTestFileSystem(root string) *FileSystem {
	wal, _ := newTestWAL()
	logger, err := persist.NewLogger(ioutil.Discard)
	if err != nil {
		panic(err.Error())
	}
	fs, err := New(root, logger, wal)
	if err != nil {
		panic(err.Error())
	}
	return fs
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

// addTestUploFile is a convenience method to add a UploFile for testing to a
// FileSystem.
func (fs *FileSystem) addTestUploFile(uploPath modules.UploPath) {
	if err := fs.addTestUploFileWithErr(uploPath); err != nil {
		panic(err)
	}
}

// addTestUploFileWithErr is a convenience method to add a UploFile for testing to
// a FileSystem.
func (fs *FileSystem) addTestUploFileWithErr(uploPath modules.UploPath) error {
	ec, err := modules.NewRSSubCode(10, 20, crypto.SegmentSize)
	if err != nil {
		return err
	}
	err = fs.NewUploFile(uploPath, "", ec, crypto.GenerateUploKey(crypto.TypeDefaultRenter), uint64(fastrand.Intn(100)), persist.DefaultDiskPermissionsTest, false)
	if err != nil {
		return err
	}
	return nil
}

// TestNew tests creating a new FileSystem.
func TestNew(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Check fields.
	if fs.parent != nil {
		t.Fatalf("fs.parent shoud be 'nil' but wasn't")
	}
	if *fs.name != "" {
		t.Fatalf("fs.staticName should be %v but was %v", "", *fs.name)
	}
	if *fs.path != root {
		t.Fatalf("fs.path should be %v but was %v", root, *fs.path)
	}
	if fs.threads == nil || len(fs.threads) != 0 {
		t.Fatal("fs.threads is not an empty initialized map")
	}
	if fs.threadUID != 0 {
		t.Fatalf("fs.threadUID should be 0 but was %v", fs.threadUID)
	}
	if fs.directories == nil || len(fs.directories) != 0 {
		t.Fatal("fs.directories is not an empty initialized map")
	}
	if fs.files == nil || len(fs.files) != 0 {
		t.Fatal("fs.files is not an empty initialized map")
	}
	// Create the filesystem again at the same location.
	_ = newTestFileSystem(*fs.path)
}

// TestNewuplodir tests if creating a new directory using Newuplodir creates the
// correct folder structure.
func TestNewuplodir(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /sub/foo
	sp := newUploPath("sub/foo")
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); err != nil {
		t.Fatal(err)
	}
	// The whole path should exist.
	if _, err := os.Stat(filepath.Join(root, sp.String())); err != nil {
		t.Fatal(err)
	}
}

// TestNewUploFile tests if creating a new file using NewUploFiles creates the
// correct folder structure and file.
func TestNewUploFile(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create file /sub/foo/file
	sp := newUploPath("sub/foo/file")
	fs.addTestUploFile(sp)
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); !errors.Contains(err, ErrExists) {
		t.Fatal("err should be ErrExists but was", err)
	}
	if _, err := os.Stat(filepath.Join(root, sp.String())); !os.IsNotExist(err) {
		t.Fatal("there should be no dir on disk")
	}
	if _, err := os.Stat(filepath.Join(root, sp.String()+modules.UploFileExtension)); err != nil {
		t.Fatal(err)
	}
	// Create a file in the root dir.
	sp = newUploPath("file")
	fs.addTestUploFile(sp)
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); !errors.Contains(err, ErrExists) {
		t.Fatal("err should be ErrExists but was", err)
	}
	if _, err := os.Stat(filepath.Join(root, sp.String())); !os.IsNotExist(err) {
		t.Fatal("there should be no dir on disk")
	}
	if _, err := os.Stat(filepath.Join(root, sp.String()+modules.UploFileExtension)); err != nil {
		t.Fatal(err)
	}
}

func (d *DirNode) checkNode(numThreads, numDirs, numFiles int) error {
	if len(d.threads) != numThreads {
		return fmt.Errorf("Expected d.threads to have length %v but was %v", numThreads, len(d.threads))
	}
	if len(d.directories) != numDirs {
		return fmt.Errorf("Expected %v subdirectories in the root but got %v", numDirs, len(d.directories))
	}
	if len(d.files) != numFiles {
		return fmt.Errorf("Expected %v files in the root but got %v", numFiles, len(d.files))
	}
	return nil
}

// TestOpenuplodir confirms that a previoiusly created uplodir can be opened and
// that the filesystem tree is extended accordingly in the process.
func TestOpenuplodir(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /foo
	sp := newUploPath("foo")
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); err != nil {
		t.Fatal(err)
	}
	// Open the newly created dir.
	foo, err := fs.Openuplodir(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := foo.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Create dir /sub/foo. This time don't use Newuplodir but Openuplodir with
	// the create flag set to `true`.
	sp = newUploPath("sub/foo")
	sd, err := fs.OpenuplodirCustom(sp, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sd.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Confirm the integrity of the root node.
	if err := fs.checkNode(0, 2, 0); err != nil {
		t.Fatal(err)
	}
	// Open the root node manually and confirm that they are the same.
	rootSD, err := fs.Openuplodir(modules.RootUploPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.checkNode(len(rootSD.threads), len(rootSD.directories), len(rootSD.files)); err != nil {
		t.Fatal(err)
	}
	// Confirm the integrity of the /sub node.
	subNode, exists := fs.directories["sub"]
	if !exists {
		t.Fatal("expected root to contain the 'sub' node")
	}
	if *subNode.name != "sub" {
		t.Fatalf("subNode name should be 'sub' but was %v", *subNode.name)
	}
	if path := filepath.Join(*subNode.parent.path, *subNode.name); path != *subNode.path {
		t.Fatalf("subNode path should be %v but was %v", path, *subNode.path)
	}
	if err := subNode.checkNode(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	// Confirm the integrity of the /sub/foo node.
	fooNode, exists := subNode.directories["foo"]
	if !exists {
		t.Fatal("expected /sub to contain /sub/foo")
	}
	if *fooNode.name != "foo" {
		t.Fatalf("fooNode name should be 'foo' but was %v", *fooNode.name)
	}
	if path := filepath.Join(*fooNode.parent.path, *fooNode.name); path != *fooNode.path {
		t.Fatalf("fooNode path should be %v but was %v", path, *fooNode.path)
	}
	if err := fooNode.checkNode(1, 0, 0); err != nil {
		t.Fatal(err)
	}
	// Open the newly created dir again.
	sd2, err := fs.Openuplodir(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sd2.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// They should have different UIDs.
	if sd.threadUID == 0 {
		t.Fatal("threaduid shouldn't be 0")
	}
	if sd2.threadUID == 0 {
		t.Fatal("threaduid shouldn't be 0")
	}
	if sd.threadUID == sd2.threadUID {
		t.Fatal("sd and sd2 should have different threaduids")
	}
	if len(sd.threads) != 2 || len(sd2.threads) != 2 {
		t.Fatal("sd and sd2 should both have 2 threads registered")
	}
	_, exists1 := sd.threads[sd.threadUID]
	_, exists2 := sd.threads[sd2.threadUID]
	_, exists3 := sd2.threads[sd.threadUID]
	_, exists4 := sd2.threads[sd2.threadUID]
	if exists := exists1 && exists2 && exists3 && exists4; !exists {
		t.Fatal("sd and sd1's threads don't contain the right uids")
	}
	// Open /sub manually and make sure that subDir and sdSub are consistent.
	sdSub, err := fs.Openuplodir(newUploPath("sub"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sdSub.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if err := subNode.checkNode(1, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := sdSub.checkNode(1, 1, 0); err != nil {
		t.Fatal(err)
	}
}

// TestOpenUploFile confirms that a previously created UploFile can be opened and
// that the filesystem tree is extended accordingly in the process.
func TestOpenUploFile(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create file /file
	sp := newUploPath("file")
	fs.addTestUploFile(sp)
	// Open the newly created file.
	sf, err := fs.OpenUploFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sf.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Confirm the integrity of the file.
	if *sf.name != "file" {
		t.Fatalf("name of file should be file but was %v", *sf.name)
	}
	if *sf.path != filepath.Join(root, (*sf.name)+modules.UploFileExtension) {
		t.Fatal("file has wrong path", *sf.path)
	}
	if sf.parent != &fs.DirNode {
		t.Fatalf("parent of file should be %v but was %v", &fs.node, sf.parent)
	}
	if sf.threadUID == 0 {
		t.Fatal("threaduid wasn't set")
	}
	if len(sf.threads) != 1 {
		t.Fatalf("len(threads) should be 1 but was %v", len(sf.threads))
	}
	if _, exists := sf.threads[sf.threadUID]; !exists {
		t.Fatal("threaduid doesn't exist in threads map")
	}
	// Confirm the integrity of the root node.
	if len(fs.threads) != 0 {
		t.Fatalf("Expected fs.threads to have length 0 but was %v", len(fs.threads))
	}
	if len(fs.directories) != 0 {
		t.Fatalf("Expected 0 subdirectories in the root but got %v", len(fs.directories))
	}
	if len(fs.files) != 1 {
		t.Fatalf("Expected 1 file in the root but got %v", len(fs.files))
	}
	// Create file /sub/file
	sp = newUploPath("/sub1/sub2/file")
	fs.addTestUploFile(sp)
	// Open the newly created file.
	sf2, err := fs.OpenUploFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sf2.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Confirm the integrity of the file.
	if *sf2.name != "file" {
		t.Fatalf("name of file should be file but was %v", *sf2.name)
	}
	if *sf2.parent.name != "sub2" {
		t.Fatalf("parent of file should be %v but was %v", "sub", *sf2.parent.name)
	}
	if sf2.threadUID == 0 {
		t.Fatal("threaduid wasn't set")
	}
	if len(sf2.threads) != 1 {
		t.Fatalf("len(threads) should be 1 but was %v", len(sf2.threads))
	}
	// Confirm the integrity of the "sub2" folder.
	sub2 := sf2.parent
	if err := sub2.checkNode(0, 0, 1); err != nil {
		t.Fatal(err)
	}
	if _, exists := sf2.threads[sf2.threadUID]; !exists {
		t.Fatal("threaduid doesn't exist in threads map")
	}
	// Confirm the integrity of the "sub1" folder.
	sub1 := sub2.parent
	if err := sub1.checkNode(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, exists := sf2.threads[sf2.threadUID]; !exists {
		t.Fatal("threaduid doesn't exist in threads map")
	}
}

// TestCloseuplodir tests that closing an opened directory shrinks the tree
// accordingly.
func TestCloseuplodir(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /sub/foo
	sp := newUploPath("sub1/foo")
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); err != nil {
		t.Fatal(err)
	}
	// Open the newly created dir.
	sd, err := fs.Openuplodir(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(sd.threads) != 1 {
		t.Fatalf("There should be 1 thread in sd.threads but got %v", len(sd.threads))
	}
	if len(sd.parent.threads) != 0 {
		t.Fatalf("The parent shouldn't have any threads but had %v", len(sd.parent.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("There should be 1 directory in fs.directories but got %v", len(fs.directories))
	}
	if len(sd.parent.directories) != 1 {
		t.Fatalf("The parent should have 1 directory but got %v", len(sd.parent.directories))
	}
	// After closing it the thread should be gone.
	sd.Close()
	if err := fs.checkNode(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	// Open the dir again. This time twice.
	sd1, err := fs.Openuplodir(sp)
	if err != nil {
		t.Fatal(err)
	}
	sd2, err := fs.OpenuplodirCustom(sp, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sd1.threads) != 2 || len(sd2.threads) != 2 {
		t.Fatalf("There should be 2 threads in sd.threads but got %v", len(sd1.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("There should be 1 directory in fs.directories but got %v", len(fs.directories))
	}
	if len(sd1.parent.directories) != 1 || len(sd2.parent.directories) != 1 {
		t.Fatalf("The parent should have 1 directory but got %v", len(sd.parent.directories))
	}
	// Close one instance.
	sd1.Close()
	if len(sd1.threads) != 1 || len(sd2.threads) != 1 {
		t.Fatalf("There should be 1 thread in sd.threads but got %v", len(sd1.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("There should be 1 directory in fs.directories but got %v", len(fs.directories))
	}
	if len(sd1.parent.directories) != 1 || len(sd2.parent.directories) != 1 {
		t.Fatalf("The parent should have 1 directory but got %v", len(sd.parent.directories))
	}
	// Close the second one.
	if err := sd2.Close(); err != nil {
		t.Fatal(err)
	}
	if len(fs.threads) != 0 {
		t.Fatalf("There should be 0 threads in fs.threads but got %v", len(fs.threads))
	}
	if len(sd1.threads) != 0 || len(sd2.threads) != 0 {
		t.Fatalf("There should be 0 threads in sd.threads but got %v", len(sd1.threads))
	}
	if len(fs.directories) != 0 {
		t.Fatalf("There should be 0 directories in fs.directories but got %v", len(fs.directories))
	}
}

// TestCloseUploFile tests that closing an opened file shrinks the tree
// accordingly.
func TestCloseUploFile(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create file /sub/file
	sp := newUploPath("sub/file")
	fs.addTestUploFile(sp)
	// Open the newly created file.
	sf, err := fs.OpenUploFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(sf.threads) != 1 {
		t.Fatalf("There should be 1 thread in sf.threads but got %v", len(sf.threads))
	}
	if len(sf.parent.threads) != 0 {
		t.Fatalf("The parent shouldn't have any threads but had %v", len(sf.parent.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("There should be 1 directory in fs.directories but got %v", len(fs.directories))
	}
	if len(sf.parent.files) != 1 {
		t.Fatalf("The parent should have 1 file but got %v", len(sf.parent.files))
	}
	// After closing it the thread should be gone.
	sf.Close()
	if len(fs.threads) != 0 {
		t.Fatalf("There should be 0 threads in fs.threads but got %v", len(fs.threads))
	}
	if len(sf.threads) != 0 {
		t.Fatalf("There should be 0 threads in sd.threads but got %v", len(sf.threads))
	}
	if len(fs.files) != 0 {
		t.Fatalf("There should be 0 files in fs.files but got %v", len(fs.files))
	}
	// Open the file again. This time twice.
	sf1, err := fs.OpenUploFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	sf2, err := fs.OpenUploFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(sf1.threads) != 2 || len(sf2.threads) != 2 {
		t.Fatalf("There should be 2 threads in sf1.threads but got %v", len(sf1.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("There should be 1 directory in fs.directories but got %v", len(fs.directories))
	}
	if len(sf1.parent.files) != 1 || len(sf2.parent.files) != 1 {
		t.Fatalf("The parent should have 1 file but got %v", len(sf1.parent.files))
	}
	// Close one instance.
	sf1.Close()
	if len(sf1.threads) != 1 || len(sf2.threads) != 1 {
		t.Fatalf("There should be 1 thread in sf1.threads but got %v", len(sf1.threads))
	}
	if len(fs.directories) != 1 {
		t.Fatalf("There should be 1 dir in fs.directories but got %v", len(fs.directories))
	}
	if len(sf1.parent.files) != 1 || len(sf2.parent.files) != 1 {
		t.Fatalf("The parent should have 1 file but got %v", len(sf1.parent.files))
	}
	if len(sf1.parent.parent.directories) != 1 {
		t.Fatalf("The root should have 1 directory but had %v", len(sf1.parent.parent.directories))
	}
	// Close the second one.
	sf2.Close()
	if len(fs.threads) != 0 {
		t.Fatalf("There should be 0 threads in fs.threads but got %v", len(fs.threads))
	}
	if len(sf1.threads) != 0 || len(sf2.threads) != 0 {
		t.Fatalf("There should be 0 threads in sd.threads but got %v", len(sf1.threads))
	}
	if len(fs.directories) != 0 {
		t.Fatalf("There should be 0 directories in fs.directories but got %v", len(fs.directories))
	}
	if len(sf1.parent.files) != 0 || len(sf2.parent.files) != 0 {
		t.Fatalf("The parent should have 0 files but got %v", len(sf1.parent.files))
	}
	if len(sf1.parent.parent.directories) != 0 {
		t.Fatalf("The root should have 0 directories but had %v", len(sf1.parent.parent.directories))
	}
}

// TestDeleteFile tests that deleting a file works as expected and that certain
// edge cases are covered.
func TestDeleteFile(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Add a file to the root dir.
	sp := newUploPath("foo")
	fs.addTestUploFile(sp)
	// Open the file.
	sf, err := fs.OpenUploFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	// File shouldn't be deleted yet.
	if sf.Deleted() {
		t.Fatal("foo is deleted before calling delete")
	}
	// Delete it using the filesystem.
	if err := fs.DeleteFile(sp); err != nil {
		t.Fatal(err)
	}
	// Check that the open instance is marked as deleted.
	if !sf.Deleted() {
		t.Fatal("foo should be marked as deleted but wasn't")
	}
	// Check that we can't open another instance of foo and that we can't create
	// a new file at the same path.
	if _, err := fs.OpenUploFile(sp); !errors.Contains(err, ErrNotExist) {
		t.Fatal("err should be ErrNotExist but was:", err)
	}
	if err := fs.addTestUploFileWithErr(sp); err != nil {
		t.Fatal("err should be nil but was:", err)
	}
}

// TestDeleteDirectory tests if deleting a directory correctly and recursively
// removes the dir.
func TestDeleteDirectory(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Add some files.
	fs.addTestUploFile(newUploPath("dir/foo/bar/file1"))
	fs.addTestUploFile(newUploPath("dir/foo/bar/file2"))
	fs.addTestUploFile(newUploPath("dir/foo/bar/file3"))
	// Delete "foo"
	if err := fs.DeleteDir(newUploPath("/dir/foo")); err != nil {
		t.Fatal(err)
	}
	// Check that /dir still exists.
	if _, err := os.Stat(filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	// Check that /dir is empty.
	if fis, err := ioutil.ReadDir(filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	} else if len(fis) != 1 {
		for i, fi := range fis {
			t.Logf("fi%v: %v", i, fi.Name())
		}
		t.Fatalf("expected 1 file in 'dir' but contains %v files", len(fis))
	}
}

// TestRenameFile tests if renaming a single file works as expected.
func TestRenameFile(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Add a file to the root dir.
	foo := newUploPath("foo")
	foobar := newUploPath("foobar")
	barfoo := newUploPath("bar/foo")
	fs.addTestUploFile(foo)
	// Rename the file.
	if err := fs.RenameFile(foo, foobar); err != nil {
		t.Fatal(err)
	}
	// Check if the file was renamed.
	if _, err := fs.OpenUploFile(foo); !errors.Contains(err, ErrNotExist) {
		t.Fatal("expected ErrNotExist but got:", err)
	}
	sf, err := fs.OpenUploFile(foobar)
	if err != nil {
		t.Fatal("expected ErrNotExist but got:", err)
	}
	sf.Close()
	// Rename the file again. This time it changes to a non-existent folder.
	if err := fs.RenameFile(foobar, barfoo); err != nil {
		t.Fatal(err)
	}
	sf, err = fs.OpenUploFile(barfoo)
	if err != nil {
		t.Fatal("expected ErrNotExist but got:", err)
	}
	sf.Close()
}

// TestThreadedAccess tests rapidly opening and closing files and directories
// from multiple threads to check the locking conventions.
func TestThreadedAccess(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Specify the file structure for the test.
	filePaths := []string{
		"f0",
		"f1",
		"f2",

		"d0/f0", "d0/f1", "d0/f2",
		"d1/f0", "d1/f1", "d1/f2",
		"d2/f0", "d2/f1", "d2/f2",

		"d0/d0/f0", "d0/d0/f1", "d0/d0/f2",
		"d0/d1/f0", "d0/d1/f1", "d0/d1/f2",
		"d0/d2/f0", "d0/d2/f1", "d0/d2/f2",

		"d1/d0/f0", "d1/d0/f1", "d1/d0/f2",
		"d1/d1/f0", "d1/d1/f1", "d1/d1/f2",
		"d1/d2/f0", "d1/d2/f1", "d1/d2/f2",

		"d2/d0/f0", "d2/d0/f1", "d2/d0/f2",
		"d2/d1/f0", "d2/d1/f1", "d2/d1/f2",
		"d2/d2/f0", "d2/d2/f1", "d2/d2/f2",
	}
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	for _, fp := range filePaths {
		fs.addTestUploFile(newUploPath(fp))
	}
	// Create a few threads which open files
	var wg sync.WaitGroup
	numThreads := 5
	maxNumActions := uint64(50000)
	numActions := uint64(0)
	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if atomic.LoadUint64(&numActions) >= maxNumActions {
					break
				}
				atomic.AddUint64(&numActions, 1)
				sp := newUploPath(filePaths[fastrand.Intn(len(filePaths))])
				sf, err := fs.OpenUploFile(sp)
				if err != nil {
					t.Fatal(err)
				}
				sf.Close()
			}
		}()
	}
	// Create a few threads which open dirs
	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if atomic.LoadUint64(&numActions) >= maxNumActions {
					break
				}
				atomic.AddUint64(&numActions, 1)
				sp := newUploPath(filePaths[fastrand.Intn(len(filePaths))])
				sp, err := sp.Dir()
				if err != nil {
					t.Fatal(err)
				}
				sd, err := fs.Openuplodir(sp)
				if err != nil {
					t.Fatal(err)
				}
				sd.Close()
			}
		}()
	}
	wg.Wait()

	// Check the root's integrity. Since all files and dirs were closed, the
	// node's maps should reflect that.
	if len(fs.threads) != 0 {
		t.Fatalf("fs should have 0 threads but had %v", len(fs.threads))
	}
	if len(fs.directories) != 0 {
		t.Fatalf("fs should have 0 directories but had %v", len(fs.directories))
	}
	if len(fs.files) != 0 {
		t.Fatalf("fs should have 0 files but had %v", len(fs.files))
	}
}

// TestuplodirRename tests the Rename method of the uplodirset.
func TestuplodirRename(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Prepare a filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	os.RemoveAll(root)
	fs := newTestFileSystem(root)

	// Specify a directory structure for this test.
	var dirStructure = []string{
		"dir1",
		"dir1/subdir1",
		"dir1/subdir1/subsubdir1",
		"dir1/subdir1/subsubdir2",
		"dir1/subdir1/subsubdir3",
		"dir1/subdir2",
		"dir1/subdir2/subsubdir1",
		"dir1/subdir2/subsubdir2",
		"dir1/subdir2/subsubdir3",
		"dir1/subdir3",
		"dir1/subdir3/subsubdir1",
		"dir1/subdir3/subsubdir2",
		"dir1/subdir3/subsubdir3",
	}
	// Specify a function that's executed in parallel which continuously saves dirs
	// to disk.
	stop := make(chan struct{})
	wg := new(sync.WaitGroup)
	f := func(entry *DirNode) {
		defer wg.Done()
		defer func() {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		for {
			select {
			case <-stop:
				return
			default:
			}
			err := entry.UpdateMetadata(uplodir.Metadata{})
			if err != nil {
				t.Error(err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	// Create the structure and spawn a goroutine that keeps saving the structure
	// to disk for each directory.
	for _, dir := range dirStructure {
		sp, err := modules.NewUploPath(dir)
		if err != nil {
			t.Fatal(err)
		}
		err = fs.Newuplodir(sp, modules.DefaultDirPerm)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := fs.Openuplodir(sp)
		if err != nil {
			t.Fatal(err)
		}
		// 50% chance to spawn goroutine. It's not realistic to assume that all dirs
		// are loaded.
		if fastrand.Intn(2) == 0 {
			wg.Add(1)
			go f(entry)
		} else {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Wait a second for the goroutines to write to disk a few times.
	time.Sleep(time.Second)
	// Rename dir1 to dir2.
	oldPath, err1 := modules.NewUploPath(dirStructure[0])
	newPath, err2 := modules.NewUploPath("dir2")
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if err := fs.RenameDir(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	// Wait another second for more writes to disk after renaming the dir before
	// killing the goroutines.
	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
	time.Sleep(time.Second)
	// Make sure we can't open any of the old folders on disk but we can open the
	// new ones.
	for _, dir := range dirStructure {
		oldDir, err1 := modules.NewUploPath(dir)
		newDir, err2 := oldDir.Rebase(oldPath, newPath)
		if err := errors.Compose(err1, err2); err != nil {
			t.Fatal(err)
		}
		// Open entry with old dir. Shouldn't work.
		_, err := fs.Openuplodir(oldDir)
		if !errors.Contains(err, ErrNotExist) {
			t.Fatal("shouldn't be able to open old path", oldDir.String(), err)
		}
		// Open entry with new dir. Should succeed.
		entry, err := fs.Openuplodir(newDir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		// Check path of entry.
		if expectedPath := fs.DirPath(newDir); *entry.path != expectedPath {
			t.Fatalf("entry should have path '%v' but was '%v'", expectedPath, entry.path)
		}
	}
}

// TestAddUploFileFromReader tests the AddUploFileFromReader method's behavior.
func TestAddUploFileFromReader(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	// Create a fileset with file.
	sf, sfs, err := newTestFileSystemWithFile(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	// Add the existing file to the set again this shouldn't do anything.
	sr, err := sf.SnapshotReader()
	if err != nil {
		t.Fatal(err)
	}
	d, err := ioutil.ReadAll(sr)
	sr.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs.AddUploFileFromReader(bytes.NewReader(d), sfs.FileUploPath(sf)); err != nil {
		t.Fatal(err)
	}
	numUploFiles := 0
	err = sfs.Walk(modules.RootUploPath(), func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) == modules.UploFileExtension {
			numUploFiles++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// There should be 1 uplofile.
	if numUploFiles != 1 {
		t.Fatalf("Found %v uplofiles but expected %v", numUploFiles, 1)
	}
	// Load the same uplofile again, but change the UID.
	b, err := ioutil.ReadFile(sf.UploFilePath())
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(b)
	newSF, newChunks, err := uplofile.LoadUploFileFromReaderWithChunks(reader, sf.UploFilePath(), sfs.staticWal)
	if err != nil {
		t.Fatal(err)
	}
	// Save the file to a temporary location with the new uid.
	newSF.UpdateUniqueID()
	newSF.SetUploFilePath(sf.UploFilePath() + "_tmp")
	if err := newSF.SaveWithChunks(newChunks); err != nil {
		t.Fatal(err)
	}
	// Grab the pre-import UID after changing it.
	preImportUID := newSF.UID()
	// Import the file. This should work because the files no longer share the same
	// UID.
	b, err = ioutil.ReadFile(newSF.UploFilePath())
	if err != nil {
		t.Fatal(err)
	}
	// Remove file at temporary location after reading it.
	if err := os.Remove(newSF.UploFilePath()); err != nil {
		t.Fatal(err)
	}
	reader = bytes.NewReader(b)
	var newSFUploPath modules.UploPath
	if err := newSFUploPath.FromSysPath(sf.UploFilePath(), sfs.Root()); err != nil {
		t.Fatal(err)
	}
	if err := sfs.AddUploFileFromReader(reader, newSFUploPath); err != nil {
		t.Fatal(err)
	}
	// Reload newSF with the new expected path.
	newSFPath := filepath.Join(filepath.Dir(sf.UploFilePath()), newSFUploPath.String()+"_1"+modules.UploFileExtension)
	newSF, err = uplofile.LoadUploFile(newSFPath, sfs.staticWal)
	if err != nil {
		t.Fatal(err)
	}
	// sf and newSF should have the same pieces.
	for chunkIndex := uint64(0); chunkIndex < sf.NumChunks(); chunkIndex++ {
		piecesOld, err1 := sf.Pieces(chunkIndex)
		piecesNew, err2 := newSF.Pieces(chunkIndex)
		if err := errors.Compose(err1, err2); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(piecesOld, piecesNew) {
			t.Log("piecesOld: ", piecesOld)
			t.Log("piecesNew: ", piecesNew)
			t.Fatal("old pieces don't match new pieces")
		}
	}
	numUploFiles = 0
	err = sfs.Walk(modules.RootUploPath(), func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) == modules.UploFileExtension {
			numUploFiles++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// There should be 2 uplofiles.
	if numUploFiles != 2 {
		t.Fatalf("Found %v uplofiles but expected %v", numUploFiles, 2)
	}
	// The UID should have changed.
	if newSF.UID() == preImportUID {
		t.Fatal("newSF UID should have changed after importing the file")
	}
	if !strings.HasSuffix(newSF.UploFilePath(), "_1"+modules.UploFileExtension) {
		t.Fatal("UploFile should have a suffix but didn't")
	}
	// Should be able to open the new file from disk.
	if _, err := os.Stat(newSF.UploFilePath()); err != nil {
		t.Fatal(err)
	}
}

// TestUploFileSetDeleteOpen checks that deleting an entry from the set followed
// by creating a Uplofile with the same name without closing the deleted entry
// works as expected.
func TestUploFileSetDeleteOpen(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create filesystem.
	sfs := newTestFileSystem(testDir(t.Name()))
	uploPath := modules.RandomUploPath()
	rc, _ := modules.NewRSSubCode(10, 20, crypto.SegmentSize)
	fileSize := uint64(100)
	source := ""
	sk := crypto.GenerateUploKey(crypto.TypeDefaultRenter)
	fileMode := os.FileMode(persist.DefaultDiskPermissionsTest)

	// Repeatedly create a UploFile and delete it while still keeping the entry
	// around. That should only be possible without errors if the correctly
	// delete the entry from the set.
	var entries []*FileNode
	for i := 0; i < 10; i++ {
		// Create UploFile
		up := modules.FileUploadParams{
			Source:              source,
			UploPath:             uploPath,
			ErasureCode:         rc,
			DisablePartialChunk: true,
		}
		err := sfs.NewUploFile(up.UploPath, up.Source, up.ErasureCode, sk, fileSize, fileMode, up.DisablePartialChunk)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := sfs.OpenUploFile(up.UploPath)
		if err != nil {
			t.Fatal(err)
		}
		// Delete UploFile
		if err := sfs.DeleteFile(sfs.FileUploPath(entry)); err != nil {
			t.Fatal(err)
		}
		// The map should be empty.
		if len(sfs.files) != 0 {
			t.Fatal("UploFileMap should have 1 file")
		}
		// Append the entry to make sure we can close it later.
		entries = append(entries, entry)
	}
	// The UploFile shouldn't exist anymore.
	_, err := sfs.OpenUploFile(uploPath)
	if !errors.Contains(err, ErrNotExist) {
		t.Fatal("UploFile shouldn't exist anymore")
	}
	// Close the entries.
	for _, entry := range entries {
		if err := entry.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestUploFileSetOpenClose tests that the threadCount of the uplofile is
// incremented and decremented properly when Open() and Close() are called
func TestUploFileSetOpenClose(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create UploFileSet with UploFile
	entry, sfs, err := newTestFileSystemWithFile(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	uploPath := sfs.FileUploPath(entry)
	exists, _ := sfs.FileExists(uploPath)
	if !exists {
		t.Fatal("No UploFileSetEntry found")
	}
	if err != nil {
		t.Fatal(err)
	}

	// Confirm 1 file is in memory
	if len(sfs.files) != 1 {
		t.Fatalf("Expected UploFileSet map to be of length 1, instead is length %v", len(sfs.files))
	}

	// Confirm threadCount is incremented properly
	if len(entry.threads) != 1 {
		t.Fatalf("Expected threadMap to be of length 1, got %v", len(entry.threads))
	}

	// Close UploFileSetEntry
	if err := entry.Close(); err != nil {
		t.Fatal(err)
	}

	// Confirm that threadCount was decremented
	if len(entry.threads) != 0 {
		t.Fatalf("Expected threadCount to be 0, got %v", len(entry.threads))
	}

	// Confirm file and partialsUploFile were removed from memory
	if len(sfs.files) != 0 {
		t.Fatalf("Expected UploFileSet map to contain 0 files, instead is length %v", len(sfs.files))
	}

	// Open uplofile again and confirm threadCount was incremented
	entry, err = sfs.OpenUploFile(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.threads) != 1 {
		t.Fatalf("Expected threadCount to be 1, got %v", len(entry.threads))
	}
}

// TestFilesInMemory confirms that files are added and removed from memory
// as expected when files are in use and not in use
func TestFilesInMemory(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create UploFileSet with UploFile
	entry, sfs, err := newTestFileSystemWithFile(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	uploPath := sfs.FileUploPath(entry)
	exists, _ := sfs.FileExists(uploPath)
	if !exists {
		t.Fatal("No UploFileSetEntry found")
	}
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is 1 file in memory.
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 files in memory, got:", len(sfs.files))
	}
	// Close File
	if err := entry.Close(); err != nil {
		t.Fatal(err)
	}
	// Confirm there are no files in memory
	if len(sfs.files) != 0 {
		t.Fatal("Expected 0 files in memory, got:", len(sfs.files))
	}

	// Test accessing the same file from two separate threads
	//
	// Open file
	entry1, err := sfs.OpenUploFile(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is 1 file in memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}
	// Access the file again
	entry2, err := sfs.OpenUploFile(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is still only has 1 file in memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}
	// Close one of the file instances
	entry1.Close()
	// Confirm there is still only has 1 file in memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}

	// Confirm closing out remaining files removes all files from memory
	//
	// Close last instance of the first file
	entry2.Close()
	// Confirm there is no file in memory
	if len(sfs.files) != 0 {
		t.Fatal("Expected 0 files in memory, got:", len(sfs.files))
	}
}

// TestRenameFileInMemory confirms that threads that have access to a file
// will continue to have access to the file even it another thread renames it
func TestRenameFileInMemory(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create FileSystem with corresponding uplofile.
	entry, sfs, err := newTestFileSystemWithFile(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	uploPath := sfs.FileUploPath(entry)
	exists, _ := sfs.FileExists(uploPath)
	if !exists {
		t.Fatal("No UploFile found")
	}
	if err != nil {
		t.Fatal(err)
	}

	// Confirm there is 1 file in memory.
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}

	// Test renaming an instance of a file
	//
	// Access file with another instance
	entry2, err := sfs.OpenUploFile(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm that renter still only has 1 file in memory.
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}
	_, err = os.Stat(entry.UploFilePath())
	if err != nil {
		t.Fatal(err)
	}
	// Rename second instance
	newUploPath := modules.RandomUploPath()
	err = sfs.RenameFile(uploPath, newUploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there are still only 1 file in memory as renaming doesn't add
	// the new name to memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}
	// Close instance of renamed file
	err = entry2.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there are still only 1 file in memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}
	// Close other instance of second file
	err = entry.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is no file in memory
	if len(sfs.files) != 0 {
		t.Fatal("Expected 0 files in memory, got:", len(sfs.files))
	}
}

// TestDeleteFileInMemory confirms that threads that have access to a file
// will continue to have access to the file even if another thread deletes it
func TestDeleteFileInMemory(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create FileSystem with UploFile
	entry, sfs, err := newTestFileSystemWithFile(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	uploPath := sfs.FileUploPath(entry)
	exists, _ := sfs.FileExists(uploPath)
	if !exists {
		t.Fatal("No UploFileSetEntry found")
	}
	if err != nil {
		t.Fatal(err)
	}

	// Confirm there is 1 file in memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}

	// Test deleting an instance of a file
	//
	// Access the file again
	entry2, err := sfs.OpenUploFile(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is still only has 1 file in memory
	if len(sfs.files) != 1 {
		t.Fatal("Expected 1 file in memory, got:", len(sfs.files))
	}
	// Delete and close instance of file.
	if err := sfs.DeleteFile(uploPath); err != nil {
		t.Fatal(err)
	}
	err = entry2.Close()
	if err != nil {
		t.Fatal(err)
	}
	// There should be no file in the filesystem after deleting it.
	if len(sfs.files) != 0 {
		t.Fatal("Expected 0 files in memory, got:", len(sfs.files))
	}
	// Confirm other instance is still in memory by calling methods on it.
	if !entry.Deleted() {
		t.Fatal("Expected file to be deleted")
	}

	// Confirm closing out remaining files removes all files from memory
	//
	// Close last instance of the first file
	err = entry.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm renter has one file in memory
	if len(sfs.files) != 0 {
		t.Fatal("Expected 0 file in memory, got:", len(sfs.files))
	}
}

// TestDeleteCorruptUploFile confirms that the uplofileset will delete a uplofile
// even if it cannot be opened
func TestDeleteCorruptUploFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create uplofileset
	_, sfs, err := newTestFileSystemWithFile(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create uplofile on disk with random bytes
	uploPath, err := modules.NewUploPath("badFile")
	if err != nil {
		t.Fatal(err)
	}
	uploFilePath := uploPath.UploFileSysPath(sfs.Root())
	err = ioutil.WriteFile(uploFilePath, fastrand.Bytes(100), persist.DefaultDiskPermissionsTest)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the uplofile cannot be opened
	_, err = sfs.OpenUploFile(uploPath)
	if err == nil || errors.Contains(err, ErrNotExist) {
		t.Fatal("expected open to fail for read error but instead got:", err)
	}

	// Delete the uplofile
	err = sfs.DeleteFile(uploPath)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the file is no longer on disk
	_, err = os.Stat(uploFilePath)
	if !os.IsNotExist(err) {
		t.Fatal("Expected err to be that file does not exists but was:", err)
	}
}

// TestuplodirDelete tests the DeleteDir method of the uplofileset.
func TestuplodirDelete(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Prepare a uplodirset
	root := filepath.Join(testDir(t.Name()), "fs-root")
	os.RemoveAll(root)
	fs := newTestFileSystem(root)

	// Specify a directory structure for this test.
	var dirStructure = []string{
		"dir1",
		"dir1/subdir1",
		"dir1/subdir1/subsubdir1",
		"dir1/subdir1/subsubdir2",
		"dir1/subdir1/subsubdir3",
		"dir1/subdir2",
		"dir1/subdir2/subsubdir1",
		"dir1/subdir2/subsubdir2",
		"dir1/subdir2/subsubdir3",
		"dir1/subdir3",
		"dir1/subdir3/subsubdir1",
		"dir1/subdir3/subsubdir2",
		"dir1/subdir3/subsubdir3",
	}
	// Specify a function that's executed in parallel which continuously saves a
	// file to disk.
	stop := make(chan struct{})
	wg := new(sync.WaitGroup)
	f := func(entry *FileNode) {
		defer wg.Done()
		defer func() {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		for {
			select {
			case <-stop:
				return
			default:
			}
			err := entry.SaveHeader()
			if err != nil && !errors.Contains(err, uplofile.ErrDeleted) {
				t.Error(err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	// Create the structure and spawn a goroutine that keeps saving the structure
	// to disk for each directory.
	for _, dir := range dirStructure {
		sp, err := modules.NewUploPath(dir)
		if err != nil {
			t.Fatal(err)
		}
		err = fs.Newuplodir(sp, modules.DefaultDirPerm)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := fs.Openuplodir(sp)
		if err != nil {
			t.Fatal(err)
		}
		// 50% chance to close the dir.
		if fastrand.Intn(2) == 0 {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}
		// Create a file in the dir.
		fileSP, err := sp.Join(hex.EncodeToString(fastrand.Bytes(16)))
		if err != nil {
			t.Fatal(err)
		}
		ec, _ := modules.NewRSSubCode(10, 20, crypto.SegmentSize)
		up := modules.FileUploadParams{Source: "", UploPath: fileSP, ErasureCode: ec}
		err = fs.NewUploFile(up.UploPath, up.Source, up.ErasureCode, crypto.GenerateUploKey(crypto.TypeDefaultRenter), 100, persist.DefaultDiskPermissionsTest, up.DisablePartialChunk)
		if err != nil {
			t.Fatal(err)
		}
		sf, err := fs.OpenUploFile(up.UploPath)
		if err != nil {
			t.Fatal(err)
		}
		// 50% chance to spawn goroutine. It's not realistic to assume that all dirs
		// are loaded.
		if fastrand.Intn(2) == 0 {
			wg.Add(1)
			go f(sf)
		} else {
			sf.Close()
		}
	}
	// Wait a second for the goroutines to write to disk a few times.
	time.Sleep(time.Second)
	// Delete dir1.
	sp, err := modules.NewUploPath("dir1")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteDir(sp); err != nil {
		t.Fatal(err)
	}

	// Wait another second for more writes to disk after renaming the dir before
	// killing the goroutines.
	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
	time.Sleep(time.Second)
	// The root uplofile dir should be empty except for 1 .uplodir file.
	files, err := fs.ReadDir(modules.RootUploPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		for _, file := range files {
			t.Log("Found ", file.Name())
		}
		t.Fatalf("There should be %v files/folders in the root dir but found %v\n", 1, len(files))
	}
	for _, file := range files {
		if filepath.Ext(file.Name()) != modules.uplodirExtension &&
			filepath.Ext(file.Name()) != modules.PartialsUploFileExtension {
			t.Fatal("Encountered unexpected file:", file.Name())
		}
	}
}

// TestuplodirRenameWithFiles tests the RenameDir method of the filesystem with
// files.
func TestuplodirRenameWithFiles(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Prepare a filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	os.RemoveAll(root)
	fs := newTestFileSystem(root)

	// Prepare parameters for uplofiles.
	rc, _ := modules.NewRSSubCode(10, 20, crypto.SegmentSize)
	fileSize := uint64(100)
	source := ""
	sk := crypto.GenerateUploKey(crypto.TypeDefaultRenter)
	fileMode := os.FileMode(persist.DefaultDiskPermissionsTest)

	// Specify a directory structure for this test.
	var dirStructure = []string{
		"dir1",
		"dir1/subdir1",
		"dir1/subdir1/subsubdir1",
		"dir1/subdir1/subsubdir2",
		"dir1/subdir1/subsubdir3",
		"dir1/subdir2",
		"dir1/subdir2/subsubdir1",
		"dir1/subdir2/subsubdir2",
		"dir1/subdir2/subsubdir3",
		"dir1/subdir3",
		"dir1/subdir3/subsubdir1",
		"dir1/subdir3/subsubdir2",
		"dir1/subdir3/subsubdir3",
	}
	// Specify a function that's executed in parallel which continuously saves a
	// file to disk.
	stop := make(chan struct{})
	wg := new(sync.WaitGroup)
	f := func(entry *FileNode) {
		defer wg.Done()
		defer func() {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		for {
			select {
			case <-stop:
				return
			default:
			}
			err := entry.SaveHeader()
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	// Create the structure and spawn a goroutine that keeps saving the structure
	// to disk for each directory.
	for _, dir := range dirStructure {
		sp, err := modules.NewUploPath(dir)
		if err != nil {
			t.Fatal(err)
		}
		err = fs.Newuplodir(sp, modules.DefaultDirPerm)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := fs.Openuplodir(sp)
		// 50% chance to close the dir.
		if fastrand.Intn(2) == 0 {
			if err := entry.Close(); err != nil {
				t.Fatal(err)
			}
		}
		// Create a file in the dir.
		fileSP, err := sp.Join(hex.EncodeToString(fastrand.Bytes(16)))
		if err != nil {
			t.Fatal(err)
		}
		err = fs.NewUploFile(fileSP, source, rc, sk, fileSize, fileMode, true)
		if err != nil {
			t.Fatal(err)
		}
		sf, err := fs.OpenUploFile(fileSP)
		if err != nil {
			t.Fatal(err)
		}
		// 50% chance to spawn goroutine. It's not realistic to assume that all dirs
		// are loaded.
		if fastrand.Intn(2) == 0 {
			wg.Add(1)
			go f(sf)
		} else {
			sf.Close()
		}
	}
	// Wait a second for the goroutines to write to disk a few times.
	time.Sleep(time.Second)
	// Rename dir1 to dir2.
	oldPath, err1 := modules.NewUploPath(dirStructure[0])
	newPath, err2 := modules.NewUploPath("dir2")
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if err := fs.RenameDir(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	// Wait another second for more writes to disk after renaming the dir before
	// killing the goroutines.
	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
	time.Sleep(time.Second)
	// Make sure we can't open any of the old folders/files on disk but we can open
	// the new ones.
	for _, dir := range dirStructure {
		oldDir, err1 := modules.NewUploPath(dir)
		newDir, err2 := oldDir.Rebase(oldPath, newPath)
		if err := errors.Compose(err1, err2); err != nil {
			t.Fatal(err)
		}
		// Open entry with old dir. Shouldn't work.
		_, err := fs.Openuplodir(oldDir)
		if !errors.Contains(err, ErrNotExist) {
			t.Fatal("shouldn't be able to open old path", oldDir.String(), err)
		}
		// Old dir shouldn't exist.
		if _, err = fs.Stat(oldDir); !os.IsNotExist(err) {
			t.Fatal(err)
		}
		// Open entry with new dir. Should succeed.
		entry, err := fs.Openuplodir(newDir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			err = entry.Close()
			if err != nil {
				t.Fatal(err)
			}
		}()
		// New dir should contain 1 uplofile.
		fis, err := fs.ReadDir(newDir)
		if err != nil {
			t.Fatal(err)
		}
		numFiles := 0
		for _, fi := range fis {
			if !fi.IsDir() && filepath.Ext(fi.Name()) == modules.UploFileExtension {
				numFiles++
			}
		}
		if numFiles != 1 {
			t.Fatalf("there should be 1 file in the new dir not %v", numFiles)
		}
		// Check uplopath of entry.
		if entry.managedAbsPath() != fs.DirPath(newDir) {
			t.Fatalf("entry should have path '%v' but was '%v'", fs.DirPath(newDir), entry.managedAbsPath())
		}
	}
}

// TestRenameDirInMemory confirms that threads that have access to a dir
// will continue to have access to the dir even if the dir is renamed.
func TestRenameDirInMemory(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create FileSystem with corresponding uplodir.
	entry, fs, err := newTestFileSystemWithDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	uploPath := fs.DirUploPath(entry)
	exists, _ := fs.DirExists(uploPath)
	if !exists {
		t.Fatal("No uplodir found")
	}

	// Confirm there are 1 dir in memory
	if len(fs.directories) != 1 {
		t.Fatal("Expected 1 dir in memory, got:", len(fs.directories))
	}

	// Access dir with another instance
	entry2, err := fs.Openuplodir(uploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm that renter still only has 1 dir in memory
	if len(fs.directories) != 1 {
		t.Fatal("Expected 1 dir in memory, got:", len(fs.directories))
	}
	path, err := entry2.Path()
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rename the directory.
	newUploPath := modules.RandomUploPath()
	err = fs.RenameDir(uploPath, newUploPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm both instances are still in memory.
	deleted, err := entry.Deleted()
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("Expected file to not be deleted")
	}
	deleted, err = entry2.Deleted()
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("Expected file to not be deleted")
	}

	// Create a new dir at the same path.
	err = fs.Newuplodir(uploPath, modules.DefaultDirPerm)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the directory is still in memory.
	deleted, err = entry.Deleted()
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("Expected file to not be deleted")
	}

	// Confirm there is still only 1 dir in memory as renaming doesn't add
	// the new name to memory.
	if len(fs.directories) != 1 {
		t.Fatal("Expected 1 dir in memory, got:", len(fs.directories))
	}
	// Close instance of renamed dir.
	err = entry2.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there are still only 1 dir in memory.
	if len(fs.directories) != 1 {
		t.Fatal("Expected 1 dir in memory, got:", len(fs.directories))
	}
	// Close other instance of second dir.
	err = entry.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is no dir in memory.
	if len(fs.directories) != 0 {
		t.Fatal("Expected 0 dirs in memory, got:", len(fs.directories))
	}
}

// TestDeleteDirInMemory confirms that threads that have access to a dir
// will continue to have access to the dir even if another thread deletes it
func TestDeleteDirInMemory(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create FileSystem with corresponding uplodir.
	entry, fs, err := newTestFileSystemWithDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	dirPath := fs.DirUploPath(entry)
	exists, _ := fs.DirExists(dirPath)
	if !exists {
		t.Fatal("No uplodirSetEntry found")
	}

	// Confirm there is 1 dir in memory
	if len(fs.directories) != 1 {
		t.Fatal("Expected 1 dir in memory, got:", len(fs.directories))
	}

	// Add files to the dir and open them.
	filepath1, err := dirPath.Join("file1")
	if err != nil {
		t.Fatal(err)
	}
	fs.addTestUploFile(filepath1)
	fileEntry1, err := fs.OpenUploFile(filepath1)
	if err != nil {
		t.Fatal(err)
	}
	filepath2, err := dirPath.Join("file2")
	if err != nil {
		t.Fatal(err)
	}
	fs.addTestUploFile(filepath2)
	fileEntry2, err := fs.OpenUploFile(filepath2)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the files are in the filesystem.
	if len(entry.files) != 2 {
		t.Fatal("Expected 2 files in memory, got:", len(entry.files))
	}

	// Test deleting an instance of a dir
	//
	// Access the dir again
	entry2, err := fs.Openuplodir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm there is still only has 1 dir in memory
	if len(fs.directories) != 1 {
		t.Fatal("Expected 1 dir in memory, got:", len(fs.directories))
	}
	// The files should not have been deleted yet.
	if fileEntry1.Deleted() {
		t.Fatal("expected file1 not to be marked deleted")
	}
	if fileEntry2.Deleted() {
		t.Fatal("expected file2 not to be marked deleted")
	}
	// Delete and close instance of dir.
	err = fs.DeleteDir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	err = entry2.Close()
	if err != nil {
		t.Fatal(err)
	}
	// There should be no dir in the filesystem after deleting it.
	if len(fs.directories) != 0 {
		t.Fatal("Expected 0 dirs in memory, got:", len(fs.directories))
	}
	// The files should have been deleted as well.
	if !fileEntry1.Deleted() {
		t.Fatal("expected file1 to be marked deleted")
	}
	if !fileEntry2.Deleted() {
		t.Fatal("expected file2 to be marked deleted")
	}

	// Confirm other instance is still in memory by calling methods on it.
	deleted, err := entry.Deleted()
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("Expected dir to be deleted")
	}

	// Create a new dir at the same path.
	err = fs.Newuplodir(dirPath, modules.DefaultDirPerm)
	if err != nil {
		t.Fatal(err)
	}
	// Get the entry.
	entry3, err := fs.Openuplodir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the other instance is still in memory.
	deleted, err = entry.Deleted()
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("Expected dir to be deleted")
	}

	// New dir should not link to the files of the old dir.
	if len(entry3.files) != 0 {
		t.Fatal("Expected 0 files in memory, got:", len(entry3.files))
	}

	// Confirm closing out remaining dirs removes all dirs from memory
	//
	// Close last instance of the first dir
	err = entry.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = entry3.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm renter has no dirs in memory
	if len(fs.directories) != 0 {
		t.Fatal("Expected 0 dirs in memory, got:", len(fs.directories))
	}
}

// TestLazyuplodir tests that uplodir correctly reads and sets the lazyuplodir
// field.
func TestLazyuplodir(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /foo
	sp := newUploPath("foo")
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); err != nil {
		t.Fatal(err)
	}
	// Open the newly created dir.
	foo, err := fs.Openuplodir(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := foo.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Get the uplodir.
	sd, err := foo.uplodir()
	if err != nil {
		t.Fatal(err)
	}
	// Lazydir should be set.
	if *foo.lazyuplodir != sd {
		t.Fatal(err)
	}
	// Fetching foo from root should also have lazydir set.
	fooRoot := fs.directories["foo"]
	if *fooRoot.lazyuplodir != sd {
		t.Fatal("fooRoot doesn't have lazydir set")
	}
	// Open foo again.
	foo2, err := fs.Openuplodir(sp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := foo2.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Lazydir should already be loaded.
	if *foo2.lazyuplodir != sd {
		t.Fatal("foo2.lazyuplodir isn't set correctly", foo2.lazyuplodir)
	}
}

// TestLazyuplodir tests that uplodir correctly reads and sets the lazyuplodir
// field.
func TestOpenCloseRoot(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)

	rootNode, err := fs.Openuplodir(modules.RootUploPath())
	if err != nil {
		t.Fatal(err)
	}
	err = rootNode.Close()
	if err != nil {
		t.Fatal(err)
	}
}

// TestFailedOpenFileFolder makes sure that a failed call to OpenUploFile or
// Openuplodir doesn't leave any nodes dangling in memory.
func TestFailedOpenFileFolder(t *testing.T) {
	if testing.Short() && !build.VLONG {
		t.SkipNow()
	}
	t.Parallel()
	// Create filesystem.
	root := filepath.Join(testDir(t.Name()), "fs-root")
	fs := newTestFileSystem(root)
	// Create dir /sub1/sub2
	sp := newUploPath("sub1/sub2")
	if err := fs.Newuplodir(sp, modules.DefaultDirPerm); err != nil {
		t.Fatal(err)
	}
	// Prepare a path to "foo"
	foo, err := sp.Join("foo")
	if err != nil {
		t.Fatal(err)
	}
	// Open "foo" as a dir.
	_, err = fs.Openuplodir(foo)
	if !errors.Contains(err, ErrNotExist) {
		t.Fatal("err should be ErrNotExist but was", err)
	}
	if len(fs.files) != 0 || len(fs.directories) != 0 {
		t.Fatal("Expected 0 files and folders but got", len(fs.files), len(fs.directories))
	}
	// Open "foo" as a file.
	_, err = fs.OpenUploFile(foo)
	if !errors.Contains(err, ErrNotExist) {
		t.Fatal("err should be ErrNotExist but was", err)
	}
	if len(fs.files) != 0 || len(fs.directories) != 0 {
		t.Fatal("Expected 0 files and folders but got", len(fs.files), len(fs.directories))
	}
}

// TestFileDirConflict makes sure that files and dirs cannot share the same
// name.
func TestFileDirConflict(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	testFileDirConflict(t, false)
	testFileDirConflict(t, true)
}

// testFileDirConflict runs a subtest for TestFileDirConflict. When `open` is
// true we first open the already existing file/dir before trying to produce
// `ErrExist` and delete it.
func testFileDirConflict(t *testing.T, open bool) {
	// Prepare a filesystem.
	root := filepath.Join(testDir(t.Name()), fmt.Sprintf("open-%v", open), "fs-root")
	err := os.RemoveAll(root)
	if err != nil {
		t.Fatal(err)
	}
	fs := newTestFileSystem(root)

	// Create a file.
	filepath := newUploPath("dir1/file1")
	fs.addTestUploFile(filepath)

	if open {
		// Open the file. This shouldn't affect later checks.
		node, err := fs.OpenUploFile(filepath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			err := node.Close()
			if err != nil {
				t.Fatal(err)
			}
		}()
	}

	// Make sure we can't create another file with the same name.
	err = fs.addTestUploFileWithErr(filepath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Make sure we can't rename another file to the same name.
	filepath2 := newUploPath("dir1/file2")
	fs.addTestUploFile(filepath2)
	err = fs.RenameFile(filepath2, filepath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Make sure we (still) can't create another file with the same name.
	err = fs.addTestUploFileWithErr(filepath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Make sure we can't create a dir with the same name.
	err = fs.Newuplodir(filepath, modules.DefaultDirPerm)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Create a dir.
	dirpath := newUploPath("dir2/dir3")
	err = fs.Newuplodir(dirpath, modules.DefaultDirPerm)
	if err != nil {
		t.Fatal(err)
	}

	if open {
		// Open the dir. This shouldn't affect later checks.
		node, err := fs.Openuplodir(dirpath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			err := node.Close()
			if err != nil {
				t.Fatal(err)
			}
		}()
	}

	// Make sure we CAN create another dir with the same name as the first
	// dir.
	err = fs.Newuplodir(dirpath, modules.DefaultDirPerm)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we can't rename a dir to the same name as the first file.
	err = fs.RenameDir(dirpath, filepath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Make sure we still CAN create another dir with the same name as the first
	// dir.
	err = fs.Newuplodir(dirpath, modules.DefaultDirPerm)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we can't create a file with the same name as the dir.
	err = fs.addTestUploFileWithErr(dirpath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Make sure we can't rename a file to the same name as the first dir.
	err = fs.RenameFile(filepath, dirpath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}

	// Make sure we can't rename another dir to the same name as the first dir.
	dirpath2 := newUploPath("dir2/dir4")
	err = fs.Newuplodir(dirpath2, modules.DefaultDirPerm)
	if err != nil {
		t.Fatal(err)
	}
	err = fs.RenameDir(dirpath2, dirpath)
	if !errors.Contains(err, ErrExists) {
		t.Fatalf("Expected err %v, got %v", ErrExists, err)
	}
}

// TestList tests that the list method of the filesystem returns the correct
// number of file and directory information
func TestList(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Prepare a uplodirset
	root := filepath.Join(testDir(t.Name()), "fs-root")
	os.RemoveAll(root)
	fs := newTestFileSystem(root)

	// Specify a directory structure for this test.
	var dirStructure = []string{
		"dir1",
		"dir1/subdir1",
		"dir1/subdir1/subsubdir1",
		"dir1/subdir1/subsubdir2",
		"dir1/subdir1/subsubdir3",
		"dir1/subdir2",
		"dir1/subdir2/subsubdir1",
		"dir1/subdir2/subsubdir2",
		"dir1/subdir2/subsubdir3",
		"dir1/subdir3",
		"dir1/subdir3/subsubdir1",
		"dir1/subdir3/subsubdir2",
		"dir1/subdir3/subsubdir3",
	}

	// Create filesystem
	for _, d := range dirStructure {
		// Create directory
		uploPath := newUploPath(d)
		err := fs.Newuplodir(uploPath, persist.DefaultDiskPermissionsTest)
		if err != nil {
			t.Fatal(err)
		}

		// Add a file
		fileUploPath, err := uploPath.Join("file")
		if err != nil {
			t.Fatal(err)
		}
		fs.addTestUploFile(fileUploPath)
	}

	// Get the cached information
	fis, dis, err := fs.CachedListCollect(newUploPath(dirStructure[0]), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(fis) != len(dirStructure) {
		t.Fatal("wrong number of files", len(fis), len(dirStructure))
	}
	if len(dis) != len(dirStructure) {
		t.Fatal("wrong number of dirs", len(dis), len(dirStructure))
	}
}
