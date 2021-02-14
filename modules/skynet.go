package modules

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/skykey"
	"github.com/uplo-tech/uplo/types"
)

const (
	// SkyfileLayoutSize describes the amount of space within the first sector
	// of a skyfile used to describe the rest of the skyfile.
	SkyfileLayoutSize = 99

	// SkyfileVersion establishes the current version for creating skyfiles.
	// The skyfile versions are different from the uplofile versions.
	SkyfileVersion = 1

	// layoutKeyDataSize is the size of the key-data field in a skyfileLayout.
	layoutKeyDataSize = 64
)

var (
	// BaseSectorNonceDerivation is the specifier used to derive a nonce for base
	// sector encryption
	BaseSectorNonceDerivation = types.NewSpecifier("BaseSectorNonce")

	// FanoutNonceDerivation is the specifier used to derive a nonce for
	// fanout encryption.
	FanoutNonceDerivation = types.NewSpecifier("FanoutNonce")

	// ExtendedSuffix is the suffix that is added to a skyfile uplopath if it is
	// a large file upload
	ExtendedSuffix = "-extended"
)

var (
	// SkyfileFormatNotSpecified is the default format for the endpoint when the
	// format isn't specified explicitly.
	SkyfileFormatNotSpecified = SkyfileFormat("")
	// SkyfileFormatConcat returns the skyfiles in a concatenated manner.
	SkyfileFormatConcat = SkyfileFormat("concat")
	// SkyfileFormatTar returns the skyfiles as a .tar.
	SkyfileFormatTar = SkyfileFormat("tar")
	// SkyfileFormatTarGz returns the skyfiles as a .tar.gz.
	SkyfileFormatTarGz = SkyfileFormat("targz")
	// SkyfileFormatZip returns the skyfiles as a .zip.
	SkyfileFormatZip = SkyfileFormat("zip")
)

type (
	// SkyfileSubfiles contains the subfiles of a skyfile, indexed by their
	// filename.
	SkyfileSubfiles map[string]SkyfileSubfileMetadata

	// SkyfileUploadParameters establishes the parameters such as the intra-root
	// erasure coding.
	SkyfileUploadParameters struct {
		// UploPath defines the uplopath that the skyfile is going to be uploaded
		// to. Recommended that the skyfile is placed in /var/skynet
		UploPath UploPath

		// DryRun allows to retrieve the skylink without actually uploading the
		// file to the Uplo network.
		DryRun bool

		// Force determines whether the upload should overwrite an existing
		// uplofile at 'UploPath'. If set to false, an error will be returned if
		// there is already a file or folder at 'UploPath'. If set to true, any
		// existing file or folder at 'UploPath' will be deleted and overwritten.
		Force bool

		// Root determines whether the upload should treat the filepath as a
		// path from system root, or if the path should be from /var/skynet.
		Root bool

		// The base chunk is always uploaded with a 1-of-N erasure coding
		// setting, meaning that only the redundancy needs to be configured by
		// the user.
		BaseChunkRedundancy uint8

		// Filename indicates the filename of the skyfile.
		Filename string

		// Mode indicates the file permissions of the skyfile.
		Mode os.FileMode

		// DefaultPath indicates what content to serve if the user has not
		// specified a path and the user is not trying to download the Skylink
		// as an archive. If left empty, it will be interpreted as "index.html"
		// on download, if the skyfile contains such a file, or the only file in
		// the skyfile, if the skyfile contains a single file.
		DefaultPath string

		// DisableDefaultPath prevents the usage of DefaultPath. As a result no
		// content will be automatically served for the skyfile.
		DisableDefaultPath bool

		// Reader supplies the file data for the skyfile.
		Reader io.Reader

		// SkykeyName is the name of the Skykey that should be used to encrypt
		// the Skyfile.
		SkykeyName string

		// SkykeyID is the ID of Skykey that should be used to encrypt the file.
		SkykeyID skykey.SkykeyID

		// If Encrypt is set to true and one of SkykeyName or SkykeyID was set,
		// a Skykey will be derived from the Master Skykey found under that
		// name/ID to be used for this specific upload.
		FileSpecificSkykey skykey.Skykey
	}

	// SkyfileMultipartUploadParameters defines the parameters specific to
	// multipart uploads. See SkyfileUploadParameters for a detailed description
	// of the fields.
	SkyfileMultipartUploadParameters struct {
		UploPath             UploPath
		Force               bool
		Root                bool
		BaseChunkRedundancy uint8
		Reader              io.Reader

		// Filename indicates the filename of the skyfile.
		Filename string

		// DefaultPath indicates the default file to be opened when opening skyfiles
		// that contain directories. If set to empty string no file will be opened
		// by default.
		DefaultPath string

		// DisableDefaultPath prevents the usage of DefaultPath. As a result no
		// content will be automatically served for the skyfile.
		DisableDefaultPath bool

		// ContentType indicates the media of the data supplied by the reader.
		ContentType string
	}

	// SkyfilePinParameters defines the parameters specific to pinning a
	// skylink. See SkyfileUploadParameters for a detailed description of the
	// fields.
	SkyfilePinParameters struct {
		UploPath             UploPath `json:"uplopath"`
		Force               bool    `json:"force"`
		Root                bool    `json:"root"`
		BaseChunkRedundancy uint8   `json:"basechunkredundancy"`
	}

	// SkyfileMetadata is all of the metadata that gets placed into the first
	// 4096 bytes of the skyfile, and is used to set the metadata of the file
	// when writing back to disk. The data is json-encoded when it is placed
	// into the leading bytes of the skyfile, meaning that this struct can be
	// extended without breaking compatibility.
	SkyfileMetadata struct {
		Filename           string          `json:"filename,omitempty"`
		Length             uint64          `json:"length,omitempty"`
		Mode               os.FileMode     `json:"mode,omitempty"`
		Subfiles           SkyfileSubfiles `json:"subfiles,omitempty"`
		DefaultPath        string          `json:"defaultpath,omitempty"`
		DisableDefaultPath bool            `json:"disabledefaultpath,omitempty"`
	}

	// SkynetPortal contains information identifying a Skynet portal.
	SkynetPortal struct {
		Address NetAddress `json:"address"` // the IP or domain name of the portal. Must be a valid network address
		Public  bool       `json:"public"`  // indicates whether the portal can be accessed publicly or not

	}
)

// ForPath returns a subset of the SkyfileMetadata that contains all of the
// subfiles for the given path. The path can lead to both a directory or a file.
// Note that this method will return the subfiles with offsets relative to the
// given path, so if a directory is requested, the subfiles in that directory
// will start at offset 0, relative to the path.
func (sm SkyfileMetadata) ForPath(path string) (SkyfileMetadata, bool, uint64, uint64) {
	// All paths must be absolute.
	path = EnsurePrefix(path, "/")
	metadata := SkyfileMetadata{
		Filename: path,
		Subfiles: make(SkyfileSubfiles),
	}

	// Try to find an exact match
	var isFile bool
	for _, sf := range sm.Subfiles {
		if EnsurePrefix(sf.Filename, "/") == path {
			isFile = true
			metadata.Subfiles[sf.Filename] = sf
			break
		}
	}

	// If there is no exact match look for directories.
	pathDir := EnsureSuffix(path, "/")
	if len(metadata.Subfiles) == 0 {
		for _, sf := range sm.Subfiles {
			// Check if the given file's path starts with `pathDir`.
			if strings.HasPrefix(EnsurePrefix(sf.Filename, "/"), pathDir) {
				metadata.Subfiles[sf.Filename] = sf
			}
		}
	}
	offset := metadata.offset()
	if offset > 0 {
		for _, sf := range metadata.Subfiles {
			sf.Offset -= offset
			metadata.Subfiles[sf.Filename] = sf
		}
	}
	// Set the metadata length by summing up the length of the subfiles.
	for _, file := range metadata.Subfiles {
		metadata.Length += file.Len
	}
	return metadata, isFile, offset, metadata.size()
}

// ContentType returns the Content Type of the data. We only return a
// content-type if it has exactly one subfile. As that is the only case where we
// can be sure of it.
func (sm SkyfileMetadata) ContentType() string {
	if len(sm.Subfiles) == 1 {
		for _, sf := range sm.Subfiles {
			return sf.ContentType
		}
	}
	return ""
}

// IsDirectory returns true if the SkyfileMetadata represents a directory.
func (sm SkyfileMetadata) IsDirectory() bool {
	if len(sm.Subfiles) > 1 {
		return true
	}
	if len(sm.Subfiles) == 1 {
		var name string
		for _, sf := range sm.Subfiles {
			name = sf.Filename
			break
		}
		if sm.Filename != name {
			return true
		}
	}
	return false
}

// size returns the total size, which is the sum of the length of all subfiles.
func (sm SkyfileMetadata) size() uint64 {
	var total uint64
	for _, sf := range sm.Subfiles {
		total += sf.Len
	}
	return total
}

// offset returns the offset of the subfile with the smallest offset.
func (sm SkyfileMetadata) offset() uint64 {
	if len(sm.Subfiles) == 0 {
		return 0
	}
	var min uint64 = math.MaxUint64
	for _, sf := range sm.Subfiles {
		if sf.Offset < min {
			min = sf.Offset
		}
	}
	return min
}

// SkyfileLayout explains the layout information that is used for storing data
// inside of the skyfile. The SkyfileLayout always appears as the first bytes
// of the leading chunk.
type SkyfileLayout struct {
	Version            uint8
	Filesize           uint64
	MetadataSize       uint64
	FanoutSize         uint64
	FanoutDataPieces   uint8
	FanoutParityPieces uint8
	CipherType         crypto.CipherType
	KeyData            [layoutKeyDataSize]byte // keyData is incompatible with ciphers that need keys larger than 64 bytes
}

// Decode will take a []byte and load the layout from that []byte.
func (sl *SkyfileLayout) Decode(b []byte) {
	offset := 0
	sl.Version = b[offset]
	offset++
	sl.Filesize = binary.LittleEndian.Uint64(b[offset:])
	offset += 8
	sl.MetadataSize = binary.LittleEndian.Uint64(b[offset:])
	offset += 8
	sl.FanoutSize = binary.LittleEndian.Uint64(b[offset:])
	offset += 8
	sl.FanoutDataPieces = b[offset]
	offset++
	sl.FanoutParityPieces = b[offset]
	offset++
	copy(sl.CipherType[:], b[offset:])
	offset += len(sl.CipherType)
	copy(sl.KeyData[:], b[offset:])
	offset += len(sl.KeyData)

	// Sanity check. If this check fails, decode() does not match the
	// SkyfileLayoutSize.
	if offset != SkyfileLayoutSize {
		build.Critical("layout size does not match the amount of data decoded")
	}
}

// DecodeFanoutIntoChunks will take the fanout bytes from a skyfile and decode
// them in to chunks.
func (sl *SkyfileLayout) DecodeFanoutIntoChunks(fanoutBytes []byte) ([][]crypto.Hash, error) {
	// There is no fanout if there are no fanout settings.
	if len(fanoutBytes) == 0 {
		return nil, nil
	}

	// Special case: if the data of the file is using 1-of-N erasure coding,
	// each piece will be identical, so the fanout will only have encoded a
	// single piece for each chunk.
	var piecesPerChunk uint64
	var chunkRootsSize uint64
	if sl.FanoutDataPieces == 1 && sl.CipherType == crypto.TypePlain {
		piecesPerChunk = 1
		chunkRootsSize = crypto.HashSize
	} else {
		// This is the case where the file data is not 1-of-N. Every piece is
		// different, so every piece must get enumerated.
		piecesPerChunk = uint64(sl.FanoutDataPieces) + uint64(sl.FanoutParityPieces)
		chunkRootsSize = crypto.HashSize * piecesPerChunk
	}
	// Sanity check - the fanout bytes should be an even number of chunks.
	if uint64(len(fanoutBytes))%chunkRootsSize != 0 {
		return nil, errors.New("the fanout bytes do not contain an even number of chunks")
	}
	numChunks := uint64(len(fanoutBytes)) / chunkRootsSize

	// Decode the fanout data into the list of chunks for the
	// fanoutStreamBufferDataSource.
	chunks := make([][]crypto.Hash, 0, numChunks)
	for i := uint64(0); i < numChunks; i++ {
		chunk := make([]crypto.Hash, piecesPerChunk)
		for j := uint64(0); j < piecesPerChunk; j++ {
			fanoutOffset := (i * chunkRootsSize) + (j * crypto.HashSize)
			copy(chunk[j][:], fanoutBytes[fanoutOffset:])
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// Encode will return a []byte that has compactly encoded all of the layout
// data.
func (sl *SkyfileLayout) Encode() []byte {
	b := make([]byte, SkyfileLayoutSize)
	offset := 0
	b[offset] = sl.Version
	offset++
	binary.LittleEndian.PutUint64(b[offset:], sl.Filesize)
	offset += 8
	binary.LittleEndian.PutUint64(b[offset:], sl.MetadataSize)
	offset += 8
	binary.LittleEndian.PutUint64(b[offset:], sl.FanoutSize)
	offset += 8
	b[offset] = sl.FanoutDataPieces
	offset++
	b[offset] = sl.FanoutParityPieces
	offset++
	copy(b[offset:], sl.CipherType[:])
	offset += len(sl.CipherType)
	copy(b[offset:], sl.KeyData[:])
	offset += len(sl.KeyData)

	// Sanity check. If this check fails, encode() does not match the
	// SkyfileLayoutSize.
	if offset != SkyfileLayoutSize {
		build.Critical("layout size does not match the amount of data encoded")
	}
	return b
}

// SkyfileSubfileMetadata is all of the metadata that belongs to a subfile in a
// skyfile. Most importantly it contains the offset at which the subfile is
// written and its length. Its filename can potentially include a '/' character
// as nested files and directories are allowed within a single Skyfile, but it
// is not allowed to contain ./, ../, be empty, or start with a forward slash.
type SkyfileSubfileMetadata struct {
	FileMode    os.FileMode `json:"mode,omitempty,uplomismatch"` // different json name for compat reasons
	Filename    string      `json:"filename,omitempty"`
	ContentType string      `json:"contenttype,omitempty"`
	Offset      uint64      `json:"offset,omitempty"`
	Len         uint64      `json:"len,omitempty"`
}

// IsDir implements the os.FileInfo interface for SkyfileSubfileMetadata.
func (sm SkyfileSubfileMetadata) IsDir() bool {
	return false
}

// IsHTML returns whether or not this subfile is an HTML file
func (sm SkyfileSubfileMetadata) IsHTML() bool {
	extension := filepath.Ext(sm.Filename)
	return extension == ".html" || extension == ".htm"
}

// Mode implements the os.FileInfo interface for SkyfileSubfileMetadata.
func (sm SkyfileSubfileMetadata) Mode() os.FileMode {
	return sm.FileMode
}

// ModTime implements the os.FileInfo interface for SkyfileSubfileMetadata.
func (sm SkyfileSubfileMetadata) ModTime() time.Time {
	return time.Time{} // no modtime available
}

// Name implements the os.FileInfo interface for SkyfileSubfileMetadata.
func (sm SkyfileSubfileMetadata) Name() string {
	return filepath.Base(sm.Filename)
}

// Size implements the os.FileInfo interface for SkyfileSubfileMetadata.
func (sm SkyfileSubfileMetadata) Size() int64 {
	return int64(sm.Len)
}

// Sys implements the os.FileInfo interface for SkyfileSubfileMetadata.
func (sm SkyfileSubfileMetadata) Sys() interface{} {
	return nil
}

// SkyfileFormat is the file format the API uses to return a Skyfile as.
type SkyfileFormat string

// Extension returns the extension for the format
func (sf SkyfileFormat) Extension() string {
	switch sf {
	case SkyfileFormatZip:
		return ".zip"
	case SkyfileFormatTar:
		return ".tar"
	case SkyfileFormatTarGz:
		return ".tar.gz"
	default:
		return ""
	}
}

// IsArchive returns true if the format is an archive.
func (sf SkyfileFormat) IsArchive() bool {
	return sf == SkyfileFormatTar ||
		sf == SkyfileFormatTarGz ||
		sf == SkyfileFormatZip
}
