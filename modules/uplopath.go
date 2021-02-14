package modules

import (
	"encoding/base32"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// uplopath.go contains the types and methods for creating and manipulating
// uplopaths. Any methods such as filepath.Join should be implemented here for
// the UploPath type to ensure consistent handling across OS.

var (
	// ErrEmptyPath is an error when a path is empty
	ErrEmptyPath = errors.New("path must be a nonempty string")
	// ErrInvalidUploPath is the error for an invalid UploPath
	ErrInvalidUploPath = errors.New("invalid UploPath")
	// ErrInvalidPathString is the error for an invalid path
	ErrInvalidPathString = errors.New("invalid path string")

	// uplodirExtension is the extension for uplodir metadata files on disk
	uplodirExtension = ".uplodir"

	// UploFileExtension is the extension for uplofiles on disk
	UploFileExtension = ".uplo"

	// PartialsUploFileExtension is the extension for uplofiles which contain
	// combined chunks.
	PartialsUploFileExtension = ".cuplo"

	// CombinedChunkExtension is the extension for a combined chunk on disk.
	CombinedChunkExtension = ".cc"
	// UnfinishedChunkExtension is the extension for an unfinished combined chunk
	// and is appended to the file in addition to CombinedChunkExtension.
	UnfinishedChunkExtension = ".unfinished"
	// ChunkMetadataExtension is the extension of a metadata file for a combined
	// chunk.
	ChunkMetadataExtension = ".ccmd"
)

var (
	// BackupFolder is the Uplo folder where all of the renter's snapshot
	// uplofiles are stored by default.
	BackupFolder = NewGlobalUploPath("/snapshots")

	// HomeFolder is the Uplo folder that is used to store all of the user
	// accessible data.
	HomeFolder = NewGlobalUploPath("/home")

	// SkynetFolder is the Uplo folder where all of the skyfiles are stored by
	// default.
	SkynetFolder = NewGlobalUploPath("/var/skynet")

	// UserFolder is the Uplo folder that is used to store the renter's uplofiles.
	UserFolder = NewGlobalUploPath("/home/user")

	// VarFolder is the Uplo folder that contains the skynet folder.
	VarFolder = NewGlobalUploPath("/var")
)

type (
	// UploPath is the struct used to uniquely identify uplofiles and uplodirs across
	// Uplo
	UploPath struct {
		Path string `json:"path"`
	}
)

// NewUploPath returns a new UploPath with the path set
func NewUploPath(s string) (UploPath, error) {
	return newUploPath(s)
}

// NewGlobalUploPath can be used to create a global var which is a UploPath. If
// there is an error creating the UploPath, the function will panic, making this
// function unsuitable for typical use.
func NewGlobalUploPath(s string) UploPath {
	sp, err := NewUploPath(s)
	if err != nil {
		panic("error creating global uplopath: " + err.Error())
	}
	return sp
}

// RandomUploPath returns a random UploPath created from 20 bytes of base32
// encoded entropy.
func RandomUploPath() (sp UploPath) {
	sp.Path = base32.StdEncoding.EncodeToString(fastrand.Bytes(20))
	sp.Path = sp.Path[:20]
	return
}

// RootUploPath returns a UploPath for the root uplodir which has a blank path
func RootUploPath() UploPath {
	return UploPath{}
}

// CombinedUploFilePath returns the UploPath to a hidden uplofile which is used to
// store chunks that contain pieces of multiple uplofiles.
func CombinedUploFilePath(ec ErasureCoder) UploPath {
	return UploPath{Path: fmt.Sprintf(".%v", ec.Identifier())}
}

// clean cleans up the string by converting an OS separators to forward slashes
// and trims leading and trailing slashes
func clean(s string) string {
	s = filepath.ToSlash(s)
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimSuffix(s, "/")
	return s
}

// newUploPath returns a new UploPath with the path set
func newUploPath(s string) (UploPath, error) {
	sp := UploPath{
		Path: clean(s),
	}
	return sp, sp.Validate(false)
}

// AddSuffix adds a numeric suffix to the end of the UploPath.
func (sp UploPath) AddSuffix(suffix uint) UploPath {
	return UploPath{
		Path: sp.Path + fmt.Sprintf("_%v", suffix),
	}
}

// Dir returns the directory of the UploPath
func (sp UploPath) Dir() (UploPath, error) {
	pathElements := strings.Split(sp.Path, "/")
	// If there is only one path element, then the Uplopath was just a filename
	// and did not have a directory, return the root Uplopath
	if len(pathElements) <= 1 {
		return RootUploPath(), nil
	}
	dir := strings.Join(pathElements[:len(pathElements)-1], "/")
	// If dir is empty or a dot, return the root Uplopath
	if dir == "" || dir == "." {
		return RootUploPath(), nil
	}
	return newUploPath(dir)
}

// Equals compares two UploPath types for equality
func (sp UploPath) Equals(uploPath UploPath) bool {
	return sp.Path == uploPath.Path
}

// IsEmpty returns true if the uplopath is equal to the nil value
func (sp UploPath) IsEmpty() bool {
	return sp.Equals(UploPath{})
}

// IsRoot indicates whether or not the UploPath path is a root directory uplopath
func (sp UploPath) IsRoot() bool {
	return sp.Path == ""
}

// Join joins the string to the end of the UploPath with a "/" and returns the
// new UploPath.
func (sp UploPath) Join(s string) (UploPath, error) {
	cleanStr := clean(s)
	if s == "" || cleanStr == "" {
		return UploPath{}, errors.New("cannot join an empty string to a uplopath")
	}
	return newUploPath(sp.Path + "/" + cleanStr)
}

// LoadString sets the path of the UploPath to the provided string
func (sp *UploPath) LoadString(s string) error {
	sp.Path = clean(s)
	return sp.Validate(false)
}

// LoadSysPath loads a UploPath from a given system path by trimming the dir at
// the front of the path, the extension at the back and returning the remaining
// path as a UploPath.
func (sp *UploPath) LoadSysPath(dir, path string) error {
	if !strings.HasPrefix(path, dir) {
		return fmt.Errorf("%v is not a prefix of %v", dir, path)
	}
	path = strings.TrimSuffix(strings.TrimPrefix(path, dir), UploFileExtension)
	return sp.LoadString(path)
}

// MarshalJSON marshals a UploPath as a string.
func (sp UploPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(sp.String())
}

// Name returns the name of the file.
func (sp UploPath) Name() string {
	pathElements := strings.Split(sp.Path, "/")
	name := pathElements[len(pathElements)-1]
	// If name is a dot, return the root Uplopath name
	if name == "." {
		name = ""
	}
	return name
}

// Rebase changes the base of a uplopath from oldBase to newBase and returns a new UploPath.
// e.g. rebasing 'a/b/myfile' from oldBase 'a/b/' to 'a/' would result in 'a/myfile'
func (sp UploPath) Rebase(oldBase, newBase UploPath) (UploPath, error) {
	if !strings.HasPrefix(sp.Path, oldBase.Path) {
		return UploPath{}, fmt.Errorf("'%v' isn't the base of '%v'", oldBase.Path, sp.Path)
	}
	relPath := strings.TrimPrefix(sp.Path, oldBase.Path)
	if relPath == "" {
		return newBase, nil
	}
	return newBase.Join(relPath)
}

// UnmarshalJSON unmarshals a uplopath into a UploPath object.
func (sp *UploPath) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &sp.Path); err != nil {
		return err
	}
	sp.Path = clean(sp.Path)
	return sp.Validate(true)
}

// uplodirSysPath returns the system path needed to read a directory on disk, the
// input dir is the root uplodir directory on disk
func (sp UploPath) uplodirSysPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(sp.Path), "")
}

// uplodirMetadataSysPath returns the system path needed to read the uplodir
// metadata file from disk, the input dir is the root uplodir directory on disk
func (sp UploPath) uplodirMetadataSysPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(sp.Path), uplodirExtension)
}

// UploFileSysPath returns the system path needed to read the UploFile from disk,
// the input dir is the root uplofile directory on disk
func (sp UploPath) UploFileSysPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(sp.Path)+UploFileExtension)
}

// UploPartialsFileSysPath returns the system path needed to read the
// PartialsUploFile from disk, the input dir is the root uplofile directory on
// disk
func (sp UploPath) UploPartialsFileSysPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(sp.Path)+PartialsUploFileExtension)
}

// String returns the UploPath's path
func (sp UploPath) String() string {
	return sp.Path
}

// FromSysPath creates a UploPath from a uploFilePath and corresponding root files
// dir.
func (sp *UploPath) FromSysPath(uploFilePath, dir string) (err error) {
	if !strings.HasPrefix(uploFilePath, dir) {
		return fmt.Errorf("UploFilePath %v is not within dir %v", uploFilePath, dir)
	}
	relPath := strings.TrimPrefix(uploFilePath, dir)
	relPath = strings.TrimSuffix(relPath, UploFileExtension)
	relPath = strings.TrimSuffix(relPath, PartialsUploFileExtension)
	*sp, err = newUploPath(relPath)
	return
}

// Validate checks that a Uplopath is a legal filename.
func (sp UploPath) Validate(isRoot bool) error {
	if err := validatePath(sp.Path, isRoot); err != nil {
		return errors.Extend(err, ErrInvalidUploPath)
	}
	return nil
}

// ValidatePathString validates a path given a string.
func ValidatePathString(path string, isRoot bool) error {
	if err := validatePath(path, isRoot); err != nil {
		return errors.Extend(err, ErrInvalidPathString)
	}
	return nil
}

// validatePath validates a path. ../ and ./ are disallowed to prevent directory
// traversal, and paths must not begin with / or be empty.
func validatePath(path string, isRoot bool) error {
	if path == "" && !isRoot {
		return ErrEmptyPath
	}
	if path == ".." {
		return errors.New("path cannot be '..'")
	}
	if path == "." {
		return errors.New("path cannot be '.'")
	}
	// check prefix
	if strings.HasPrefix(path, "/") {
		return errors.New("path cannot begin with /")
	}
	if strings.HasPrefix(path, "../") {
		return errors.New("path cannot begin with ../")
	}
	if strings.HasPrefix(path, "./") {
		return errors.New("path connot begin with ./")
	}
	var prevElem string
	for _, pathElem := range strings.Split(path, "/") {
		if pathElem == "." || pathElem == ".." {
			return errors.New("path cannot contain . or .. elements")
		}
		if prevElem != "" && pathElem == "" {
			return ErrEmptyPath
		}
		if prevElem == "/" || pathElem == "/" {
			return errors.New("path cannot contain //")
		}
		prevElem = pathElem
	}

	// Final check for a valid utf8
	if !utf8.ValidString(path) {
		return errors.New("path is not a valid utf8 path")
	}

	return nil
}
