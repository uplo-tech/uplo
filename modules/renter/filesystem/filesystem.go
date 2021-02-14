package filesystem

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplodir"
	"github.com/uplo-tech/uplo/modules/renter/filesystem/uplofile"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/writeaheadlog"
)

var (
	// ErrNotExist is returned when a file or folder can't be found on disk.
	ErrNotExist = errors.New("path does not exist")

	// ErrExists is returned when a file or folder already exists at a given
	// location.
	ErrExists = errors.New("a file or folder already exists at the specified path")

	// ErrDeleteFileIsDir is returned when the file delete method is used but
	// the filename corresponds to a directory
	ErrDeleteFileIsDir = errors.New("cannot delete file, file is a directory")
)

type (
	// FileSystem implements a thread-safe filesystem for Uplo for loading
	// UploFiles, uplodirs and potentially other supported Uplo types in the
	// future.
	FileSystem struct {
		DirNode
	}

	// node is a struct that contains the common fields of every node.
	node struct {
		// fields that all copies of a node share.
		path      *string
		parent    *DirNode
		name      *string
		staticWal *writeaheadlog.WAL
		threads   map[threadUID]struct{} // tracks all the threadUIDs of evey copy of the node
		staticLog *persist.Logger
		staticUID uint64
		mu        *sync.Mutex

		// fields that differ between copies of the same node.
		threadUID threadUID // unique ID of a copy of a node
	}
	threadUID uint64
)

// newNode is a convenience function to initialize a node.
func newNode(parent *DirNode, path, name string, uid threadUID, wal *writeaheadlog.WAL, log *persist.Logger) node {
	return node{
		path:      &path,
		parent:    parent,
		name:      &name,
		staticLog: log,
		staticUID: newInode(),
		staticWal: wal,
		threads:   make(map[threadUID]struct{}),
		threadUID: uid,
		mu:        new(sync.Mutex),
	}
}

// managedLockWithParent is a helper method which correctly acquires the lock of
// a node and it's parent. If no parent it available it will return 'nil'. In
// either case the node and potential parent will be locked after the call.
func (n *node) managedLockWithParent() *DirNode {
	var parent *DirNode
	for {
		// If a parent exists, we need to lock it while closing a child.
		n.mu.Lock()
		parent = n.parent
		n.mu.Unlock()
		if parent != nil {
			parent.mu.Lock()
		}
		n.mu.Lock()
		if n.parent != parent {
			n.mu.Unlock()
			parent.mu.Unlock()
			continue // try again
		}
		break
	}
	return parent
}

// NID returns the node's unique identifier.
func (n *node) Inode() uint64 {
	return n.staticUID
}

// newThreadUID returns a random threadUID to be used as the threadUID in the
// threads map of the node.
func newThreadUID() threadUID {
	return threadUID(fastrand.Uint64n(math.MaxUint64))
}

// newInode will create a unique identifier for a filesystem node.
//
// TODO: replace this with a function that doesn't repeat itself.
func newInode() uint64 {
	return fastrand.Uint64n(math.MaxUint64)
}

// nodeUploPath returns the UploPath of a node relative to a root path.
func nodeUploPath(rootPath string, n *node) (sp modules.UploPath) {
	if err := sp.FromSysPath(n.managedAbsPath(), rootPath); err != nil {
		build.Critical("FileSystem.managedUploPath: should never fail", err)
	}
	return sp
}

// closeNode removes a thread from the node's threads map. This should only be
// called from within other 'close' methods.
func (n *node) closeNode() {
	if _, exists := n.threads[n.threadUID]; !exists {
		build.Critical("threaduid doesn't exist in threads map: ", n.threadUID, len(n.threads))
	}
	delete(n.threads, n.threadUID)
}

// absPath returns the absolute path of the node.
func (n *node) absPath() string {
	return *n.path
}

// managedAbsPath returns the absolute path of the node.
func (n *node) managedAbsPath() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.absPath()
}

// New creates a new FileSystem at the specified root path. The folder will be
// created if it doesn't exist already.
func New(root string, log *persist.Logger, wal *writeaheadlog.WAL) (*FileSystem, error) {
	fs := &FileSystem{
		DirNode: DirNode{
			// The root doesn't require a parent, a name or uid.
			node:        newNode(nil, root, "", 0, wal, log),
			directories: make(map[string]*DirNode),
			files:       make(map[string]*FileNode),
			lazyuplodir:  new(*uplodir.uplodir),
		},
	}
	// Prepare root folder.
	err := fs.Newuplodir(modules.RootUploPath(), modules.DefaultDirPerm)
	if err != nil && !errors.Contains(err, ErrExists) {
		return nil, err
	}
	return fs, nil
}

// AddUploFileFromReader adds an existing UploFile to the set and stores it on
// disk. If the exact same file already exists, this is a no-op. If a file
// already exists with a different UID, the UID will be updated and a unique
// path will be chosen. If no file exists, the UID will be updated but the path
// remains the same.
func (fs *FileSystem) AddUploFileFromReader(rs io.ReadSeeker, uploPath modules.UploPath) (err error) {
	// Load the file.
	path := fs.FilePath(uploPath)
	sf, chunks, err := uplofile.LoadUploFileFromReaderWithChunks(rs, path, fs.staticWal)
	if err != nil {
		return err
	}
	// Create dir with same Mode as file if it doesn't exist already and open
	// it.
	dirUploPath, err := uploPath.Dir()
	if err != nil {
		return err
	}
	if err := fs.managedNewuplodir(dirUploPath, sf.Mode()); err != nil {
		return err
	}
	dir, err := fs.managedOpenDir(dirUploPath.String())
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, dir.Close())
	}()
	// Add the file to the dir.
	return dir.managedNewUploFileFromExisting(sf, chunks)
}

// CachedFileInfo returns the cached File Information of the uplofile
func (fs *FileSystem) CachedFileInfo(uploPath modules.UploPath) (modules.FileInfo, error) {
	return fs.managedFileInfo(uploPath, true, nil, nil, nil)
}

// CachedList lists the files and directories within a uplodir.
func (fs *FileSystem) CachedList(uploPath modules.UploPath, recursive bool, flf modules.FileListFunc, dlf modules.DirListFunc) error {
	return fs.managedList(uploPath, recursive, true, nil, nil, nil, flf, dlf)
}

// CachedListOnNode will return the files and directories within a given uplodir
// node in a non-recursive way.
func (fs *FileSystem) CachedListOnNode(d *DirNode) (fis []modules.FileInfo, dis []modules.DirectoryInfo, err error) {
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
	err = d.managedList(fs.managedAbsPath(), false, true, nil, nil, nil, flf, dlf)

	// Sort slices by UploPath.
	sort.Slice(dis, func(i, j int) bool {
		return dis[i].UploPath.String() < dis[j].UploPath.String()
	})
	sort.Slice(fis, func(i, j int) bool {
		return fis[i].UploPath.String() < fis[j].UploPath.String()
	})
	return
}

// DeleteDir deletes a dir from the filesystem. The dir will be marked as
// 'deleted' which should cause all remaining instances of the dir to be close
// shortly. Only when all instances of the dir are closed it will be removed
// from the tree. This means that as long as the deletion is in progress, no new
// file of the same path can be created and the existing file can't be opened
// until all instances of it are closed.
func (fs *FileSystem) DeleteDir(uploPath modules.UploPath) error {
	return fs.managedDeleteDir(uploPath.String())
}

// DeleteFile deletes a file from the filesystem. The file will be marked as
// 'deleted' which should cause all remaining instances of the file to be closed
// shortly. Only when all instances of the file are closed it will be removed
// from the tree. This means that as long as the deletion is in progress, no new
// file of the same path can be created and the existing file can't be opened
// until all instances of it are closed.
func (fs *FileSystem) DeleteFile(uploPath modules.UploPath) error {
	return fs.managedDeleteFile(uploPath.String())
}

// DirInfo returns the Directory Information of the uplodir
func (fs *FileSystem) DirInfo(uploPath modules.UploPath) (_ modules.DirectoryInfo, err error) {
	dir, err := fs.managedOpenDir(uploPath.String())
	if err != nil {
		return modules.DirectoryInfo{}, nil
	}
	defer func() {
		err = errors.Compose(err, dir.Close())
	}()
	di, err := dir.managedInfo(uploPath)
	if err != nil {
		return modules.DirectoryInfo{}, err
	}
	di.UploPath = uploPath
	return di, nil
}

// DirNodeInfo will return the DirectoryInfo of a uplodir given the node. This is
// more efficient than calling fs.DirInfo.
func (fs *FileSystem) DirNodeInfo(n *DirNode) (modules.DirectoryInfo, error) {
	sp := fs.DirUploPath(n)
	return n.managedInfo(sp)
}

// FileInfo returns the File Information of the uplofile
func (fs *FileSystem) FileInfo(uploPath modules.UploPath, offline map[string]bool, goodForRenew map[string]bool, contracts map[string]modules.RenterContract) (modules.FileInfo, error) {
	return fs.managedFileInfo(uploPath, false, offline, goodForRenew, contracts)
}

// FileNodeInfo returns the FileInfo of a uplofile given the node for the
// uplofile. This is faster than calling fs.FileInfo.
func (fs *FileSystem) FileNodeInfo(n *FileNode) (modules.FileInfo, error) {
	sp := fs.FileUploPath(n)
	return n.staticCachedInfo(sp)
}

// List lists the files and directories within a uplodir.
func (fs *FileSystem) List(uploPath modules.UploPath, recursive bool, offlineMap, goodForRenewMap map[string]bool, contractsMap map[string]modules.RenterContract, flf modules.FileListFunc, dlf modules.DirListFunc) error {
	return fs.managedList(uploPath, recursive, false, offlineMap, goodForRenewMap, contractsMap, flf, dlf)
}

// FileExists checks to see if a file with the provided uploPath already exists
// in the renter.
func (fs *FileSystem) FileExists(uploPath modules.UploPath) (bool, error) {
	path := fs.FilePath(uploPath)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

// FilePath converts a UploPath into a file's system path.
func (fs *FileSystem) FilePath(uploPath modules.UploPath) string {
	return uploPath.UploFileSysPath(fs.managedAbsPath())
}

// Newuplodir creates the folder for the specified uploPath.
func (fs *FileSystem) Newuplodir(uploPath modules.UploPath, mode os.FileMode) error {
	return fs.managedNewuplodir(uploPath, mode)
}

// NewUploFile creates a UploFile at the specified uploPath.
func (fs *FileSystem) NewUploFile(uploPath modules.UploPath, source string, ec modules.ErasureCoder, mk crypto.CipherKey, fileSize uint64, fileMode os.FileMode, disablePartialUpload bool) error {
	// Create uplodir for file.
	dirUploPath, err := uploPath.Dir()
	if err != nil {
		return err
	}
	if err = fs.Newuplodir(dirUploPath, fileMode); err != nil {
		return errors.AddContext(err, fmt.Sprintf("failed to create uplodir %v for UploFile %v", dirUploPath.String(), uploPath.String()))
	}
	return fs.managedNewUploFile(uploPath.String(), source, ec, mk, fileSize, fileMode, disablePartialUpload)
}

// ReadDir reads all the fileinfos of the specified dir.
func (fs *FileSystem) ReadDir(uploPath modules.UploPath) ([]os.FileInfo, error) {
	// Open dir.
	dirPath := uploPath.uplodirSysPath(fs.managedAbsPath())
	f, err := os.Open(dirPath)
	if err != nil {
		return nil, err
	}
	// Read it and close it.
	fis, err1 := f.Readdir(-1)
	err2 := f.Close()
	err = errors.Compose(err1, err2)
	return fis, err
}

// DirExists checks to see if a dir with the provided uploPath already exists in
// the renter.
func (fs *FileSystem) DirExists(uploPath modules.UploPath) (bool, error) {
	path := fs.DirPath(uploPath)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

// DirPath converts a UploPath into a dir's system path.
func (fs *FileSystem) DirPath(uploPath modules.UploPath) string {
	return uploPath.uplodirSysPath(fs.managedAbsPath())
}

// Root returns the root system path of the FileSystem.
func (fs *FileSystem) Root() string {
	return fs.DirPath(modules.RootUploPath())
}

// FileUploPath returns the UploPath of a file node.
func (fs *FileSystem) FileUploPath(n *FileNode) (sp modules.UploPath) {
	return fs.managedUploPath(&n.node)
}

// DirUploPath returns the UploPath of a dir node.
func (fs *FileSystem) DirUploPath(n *DirNode) (sp modules.UploPath) {
	return fs.managedUploPath(&n.node)
}

// UpdateDirMetadata updates the metadata of a uplodir.
func (fs *FileSystem) UpdateDirMetadata(uploPath modules.UploPath, metadata uplodir.Metadata) (err error) {
	dir, err := fs.Openuplodir(uploPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, dir.Close())
	}()
	return dir.UpdateMetadata(metadata)
}

// managedUploPath returns the UploPath of a node.
func (fs *FileSystem) managedUploPath(n *node) modules.UploPath {
	return nodeUploPath(fs.managedAbsPath(), n)
}

// Stat is a wrapper for os.Stat which takes a UploPath as an argument instead of
// a system path.
func (fs *FileSystem) Stat(uploPath modules.UploPath) (os.FileInfo, error) {
	path := uploPath.uplodirSysPath(fs.managedAbsPath())
	return os.Stat(path)
}

// Walk is a wrapper for filepath.Walk which takes a UploPath as an argument
// instead of a system path.
func (fs *FileSystem) Walk(uploPath modules.UploPath, walkFn filepath.WalkFunc) error {
	dirPath := uploPath.uplodirSysPath(fs.managedAbsPath())
	return filepath.Walk(dirPath, walkFn)
}

// WriteFile is a wrapper for ioutil.WriteFile which takes a UploPath as an
// argument instead of a system path.
func (fs *FileSystem) WriteFile(uploPath modules.UploPath, data []byte, perm os.FileMode) error {
	path := uploPath.UploFileSysPath(fs.managedAbsPath())
	return ioutil.WriteFile(path, data, perm)
}

// NewUploFileFromLegacyData creates a new UploFile from data that was previously loaded
// from a legacy file.
func (fs *FileSystem) NewUploFileFromLegacyData(fd uplofile.FileData) (_ *FileNode, err error) {
	// Get file's UploPath.
	sp, err := modules.UserFolder.Join(fd.Name)
	if err != nil {
		return nil, err
	}
	// Get uplopath of dir.
	dirUploPath, err := sp.Dir()
	if err != nil {
		return nil, err
	}
	// Create the dir if it doesn't exist.
	if err := fs.Newuplodir(dirUploPath, 0755); err != nil {
		return nil, err
	}
	// Open dir.
	dir, err := fs.managedOpenDir(dirUploPath.String())
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Compose(err, dir.Close())
	}()
	// Add the file to the dir.
	return dir.managedNewUploFileFromLegacyData(sp.Name(), fd)
}

// Openuplodir opens a uplodir and adds it and all of its parents to the
// filesystem tree.
func (fs *FileSystem) Openuplodir(uploPath modules.UploPath) (*DirNode, error) {
	return fs.OpenuplodirCustom(uploPath, false)
}

// OpenuplodirCustom opens a uplodir and adds it and all of its parents to the
// filesystem tree. If create is true it will create the dir if it doesn't
// exist.
func (fs *FileSystem) OpenuplodirCustom(uploPath modules.UploPath, create bool) (*DirNode, error) {
	dn, err := fs.managedOpenuplodir(uploPath)
	if create && errors.Contains(err, ErrNotExist) {
		// If uplodir doesn't exist create one
		err = fs.Newuplodir(uploPath, modules.DefaultDirPerm)
		if err != nil && !errors.Contains(err, ErrExists) {
			return nil, err
		}
		return fs.managedOpenuplodir(uploPath)
	}
	return dn, err
}

// OpenUploFile opens a UploFile and adds it and all of its parents to the
// filesystem tree.
func (fs *FileSystem) OpenUploFile(uploPath modules.UploPath) (*FileNode, error) {
	sf, err := fs.managedOpenFile(uploPath.String())
	if err != nil {
		return nil, err
	}
	return sf, nil
}

// RenameFile renames the file with oldUploPath to newUploPath.
func (fs *FileSystem) RenameFile(oldUploPath, newUploPath modules.UploPath) (err error) {
	// Open uplodir for file at old location.
	oldDirUploPath, err := oldUploPath.Dir()
	if err != nil {
		return err
	}
	oldDir, err := fs.managedOpenuplodir(oldDirUploPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, oldDir.Close())
	}()
	// Open the file.
	sf, err := oldDir.managedOpenFile(oldUploPath.Name())
	if errors.Contains(err, ErrNotExist) {
		return ErrNotExist
	}
	if err != nil {
		return errors.AddContext(err, "failed to open file for renaming")
	}
	defer func() {
		err = errors.Compose(err, sf.Close())
	}()

	// Create and Open uplodir for file at new location.
	newDirUploPath, err := newUploPath.Dir()
	if err != nil {
		return err
	}
	if err := fs.Newuplodir(newDirUploPath, sf.managedMode()); err != nil {
		return errors.AddContext(err, fmt.Sprintf("failed to create uplodir %v for UploFile %v", newDirUploPath.String(), oldUploPath.String()))
	}
	newDir, err := fs.managedOpenuplodir(newDirUploPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, newDir.Close())
	}()
	// Rename the file.
	return sf.managedRename(newUploPath.Name(), oldDir, newDir)
}

// RenameDir takes an existing directory and changes the path. The original
// directory must exist, and there must not be any directory that already has
// the replacement path.  All uplo files within directory will also be renamed
func (fs *FileSystem) RenameDir(oldUploPath, newUploPath modules.UploPath) error {
	// Open uplodir for parent dir at old location.
	oldDirUploPath, err := oldUploPath.Dir()
	if err != nil {
		return err
	}
	oldDir, err := fs.managedOpenuplodir(oldDirUploPath)
	if err != nil {
		return err
	}
	defer func() {
		oldDir.Close()
	}()
	// Open the dir to rename.
	sd, err := oldDir.managedOpenDir(oldUploPath.Name())
	if errors.Contains(err, ErrNotExist) {
		return ErrNotExist
	}
	if err != nil {
		return errors.AddContext(err, "failed to open file for renaming")
	}
	defer func() {
		sd.Close()
	}()

	// Create and Open parent uplodir for dir at new location.
	newDirUploPath, err := newUploPath.Dir()
	if err != nil {
		return err
	}
	md, err := sd.Metadata()
	if err != nil {
		return err
	}
	if err := fs.Newuplodir(newDirUploPath, md.Mode); err != nil {
		return errors.AddContext(err, fmt.Sprintf("failed to create uplodir %v for UploFile %v", newDirUploPath.String(), oldUploPath.String()))
	}
	newDir, err := fs.managedOpenuplodir(newDirUploPath)
	if err != nil {
		return err
	}
	defer func() {
		newDir.Close()
	}()
	// Rename the dir.
	err = sd.managedRename(newUploPath.Name(), oldDir, newDir)
	return err
}

// managedDeleteFile opens the parent folder of the file to delete and calls
// managedDeleteFile on it.
func (fs *FileSystem) managedDeleteFile(relPath string) (err error) {
	// Open the folder that contains the file.
	dirPath, fileName := filepath.Split(relPath)
	var dir *DirNode
	if dirPath == string(filepath.Separator) || dirPath == "." || dirPath == "" {
		dir = &fs.DirNode // file is in the root dir
	} else {
		var err error
		dir, err = fs.managedOpenDir(filepath.Dir(relPath))
		if err != nil {
			return errors.AddContext(err, "failed to open parent dir of file")
		}
		// Close the dir since we are not returning it. The open file keeps it
		// loaded in memory.
		defer func() {
			err = errors.Compose(err, dir.Close())
		}()
	}
	return dir.managedDeleteFile(fileName)
}

// managedDeleteDir opens the parent folder of the dir to delete and calls
// managedDelete on it.
func (fs *FileSystem) managedDeleteDir(path string) (err error) {
	// Open the dir.
	dir, err := fs.managedOpenDir(path)
	if err != nil {
		return errors.AddContext(err, "failed to open parent dir of file")
	}
	// Close the dir since we are not returning it. The open file keeps it
	// loaded in memory.
	defer func() {
		err = errors.Compose(err, dir.Close())
	}()
	return dir.managedDelete()
}

// managedFileInfo returns the FileInfo of the uplofile.
func (fs *FileSystem) managedFileInfo(uploPath modules.UploPath, cached bool, offline map[string]bool, goodForRenew map[string]bool, contracts map[string]modules.RenterContract) (_ modules.FileInfo, err error) {
	// Open the file.
	file, err := fs.managedOpenFile(uploPath.String())
	if err != nil {
		return modules.FileInfo{}, err
	}
	defer func() {
		err = errors.Compose(err, file.Close())
	}()
	if cached {
		return file.staticCachedInfo(uploPath)
	}
	return file.managedFileInfo(uploPath, offline, goodForRenew, contracts)
}

// managedList returns the files and dirs within the uplodir specified by uploPath.
// offlineMap, goodForRenewMap and contractMap don't need to be provided if
// 'cached' is set to 'true'.
func (fs *FileSystem) managedList(uploPath modules.UploPath, recursive, cached bool, offlineMap map[string]bool, goodForRenewMap map[string]bool, contractsMap map[string]modules.RenterContract, flf modules.FileListFunc, dlf modules.DirListFunc) (err error) {
	// Open the folder.
	dir, err := fs.managedOpenDir(uploPath.String())
	if err != nil {
		return errors.AddContext(err, fmt.Sprintf("failed to open folder '%v' specified by FileList", uploPath))
	}
	defer func() {
		err = errors.Compose(err, dir.Close())
	}()
	return dir.managedList(fs.managedAbsPath(), recursive, cached, offlineMap, goodForRenewMap, contractsMap, flf, dlf)
}

// managedNewuplodir creates the folder at the specified uploPath.
func (fs *FileSystem) managedNewuplodir(uploPath modules.UploPath, mode os.FileMode) (err error) {
	// If uploPath is the root dir we just create the metadata for it.
	if uploPath.IsRoot() {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		dirPath := uploPath.uplodirSysPath(fs.absPath())
		_, err := uplodir.New(dirPath, fs.absPath(), mode, fs.staticWal)
		// If the uplodir already exists on disk, return without an error.
		if os.IsExist(err) {
			return nil // nothing to do
		}
		return err
	}
	// If uploPath isn't the root dir we need to grab the parent.
	parentPath, err := uploPath.Dir()
	if err != nil {
		return err
	}
	parent, err := fs.managedOpenDir(parentPath.String())
	if errors.Contains(err, ErrNotExist) {
		// If the parent doesn't exist yet we create it.
		err = fs.managedNewuplodir(parentPath, mode)
		if err == nil {
			parent, err = fs.managedOpenDir(parentPath.String())
		}
	}
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Compose(err, parent.Close())
	}()
	// Create the dir within the parent.
	return parent.managedNewuplodir(uploPath.Name(), fs.managedAbsPath(), mode)
}

// managedOpenFile opens a UploFile and adds it and all of its parents to the
// filesystem tree.
func (fs *FileSystem) managedOpenFile(relPath string) (_ *FileNode, err error) {
	// Open the folder that contains the file.
	dirPath, fileName := filepath.Split(relPath)
	var dir *DirNode
	if dirPath == string(filepath.Separator) || dirPath == "." || dirPath == "" {
		dir = &fs.DirNode // file is in the root dir
	} else {
		var err error
		dir, err = fs.managedOpenDir(filepath.Dir(relPath))
		if err != nil {
			return nil, errors.AddContext(err, "failed to open parent dir of file")
		}
		// Close the dir since we are not returning it. The open file keeps it
		// loaded in memory.
		defer func() {
			err = errors.Compose(err, dir.Close())
		}()
	}
	return dir.managedOpenFile(fileName)
}

// managedNewUploFile opens the parent folder of the new UploFile and calls
// managedNewUploFile on it.
func (fs *FileSystem) managedNewUploFile(relPath string, source string, ec modules.ErasureCoder, mk crypto.CipherKey, fileSize uint64, fileMode os.FileMode, disablePartialUpload bool) (err error) {
	// Open the folder that contains the file.
	dirPath, fileName := filepath.Split(relPath)
	var dir *DirNode
	if dirPath == string(filepath.Separator) || dirPath == "." || dirPath == "" {
		dir = &fs.DirNode // file is in the root dir
	} else {
		var err error
		dir, err = fs.managedOpenDir(filepath.Dir(relPath))
		if err != nil {
			return errors.AddContext(err, "failed to open parent dir of new file")
		}
		defer func() {
			err = errors.Compose(err, dir.Close())
		}()
	}
	return dir.managedNewUploFile(fileName, source, ec, mk, fileSize, fileMode, disablePartialUpload)
}

// managedOpenuplodir opens a uplodir and adds it and all of its parents to the
// filesystem tree.
func (fs *FileSystem) managedOpenuplodir(uploPath modules.UploPath) (*DirNode, error) {
	if uploPath.IsRoot() {
		// Make sure the metadata exists.
		_, err := os.Stat(filepath.Join(fs.absPath(), modules.uplodirExtension))
		if os.IsNotExist(err) {
			return nil, ErrNotExist
		}
		return fs.DirNode.managedCopy(), nil
	}
	dir, err := fs.DirNode.managedOpenDir(uploPath.String())
	if err != nil {
		return nil, err
	}
	return dir, nil
}
