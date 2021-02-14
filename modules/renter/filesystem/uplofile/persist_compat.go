package uplofile

import (
	"os"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
)

type (
	// FileData is a helper struct that contains all the relevant information
	// of a file. It simplifies passing the necessary data between modules and
	// keeps the interface clean.
	FileData struct {
		Name        string
		FileSize    uint64
		MasterKey   [crypto.EntropySize]byte
		ErasureCode modules.ErasureCoder
		RepairPath  string
		PieceSize   uint64
		Mode        os.FileMode
		Deleted     bool
		UID         UplofileUID
		Chunks      []FileChunk
	}
	// FileChunk is a helper struct that contains data about a chunk.
	FileChunk struct {
		Pieces [][]Piece
	}
)

// NewFromLegacyData creates a new UploFile from data that was previously loaded
// from a legacy file.
func NewFromLegacyData(fd FileData, uploFilePath string, wal *writeaheadlog.WAL) (*UploFile, error) {
	// Legacy master keys are always twofish keys.
	mk, err := crypto.NewUploKey(crypto.TypeTwofish, fd.MasterKey[:])
	if err != nil {
		return nil, errors.AddContext(err, "failed to restore master key")
	}
	currentTime := time.Now()
	ecType, ecParams := marshalErasureCoder(fd.ErasureCode)
	zeroHealth := float64(1 + fd.ErasureCode.MinPieces()/(fd.ErasureCode.NumPieces()-fd.ErasureCode.MinPieces()))
	file := &UploFile{
		staticMetadata: Metadata{
			AccessTime:              currentTime,
			ChunkOffset:             defaultReservedMDPages * pageSize,
			ChangeTime:              currentTime,
			HasPartialChunk:         false,
			CreateTime:              currentTime,
			CachedHealth:            zeroHealth,
			CachedStuckHealth:       0,
			CachedRedundancy:        0,
			CachedUserRedundancy:    0,
			CachedUploadProgress:    0,
			FileSize:                int64(fd.FileSize),
			LocalPath:               fd.RepairPath,
			StaticMasterKey:         mk.Key(),
			StaticMasterKeyType:     mk.Type(),
			Mode:                    fd.Mode,
			ModTime:                 currentTime,
			staticErasureCode:       fd.ErasureCode,
			StaticErasureCodeType:   ecType,
			StaticErasureCodeParams: ecParams,
			StaticPagesPerChunk:     numChunkPagesRequired(fd.ErasureCode.NumPieces()),
			StaticPieceSize:         fd.PieceSize,
			UniqueID:                UplofileUID(fd.UID),
		},
		deps:        modules.ProdDependencies,
		deleted:     fd.Deleted,
		numChunks:   len(fd.Chunks),
		uploFilePath: uploFilePath,
		wal:         wal,
	}
	// Update cached fields for 0-Byte files.
	if file.staticMetadata.FileSize == 0 {
		file.staticMetadata.CachedHealth = 0
		file.staticMetadata.CachedStuckHealth = 0
		file.staticMetadata.CachedRedundancy = float64(fd.ErasureCode.NumPieces()) / float64(fd.ErasureCode.MinPieces())
		file.staticMetadata.CachedUserRedundancy = file.staticMetadata.CachedRedundancy
		file.staticMetadata.CachedUploadProgress = 100
	}

	// Create the chunks.
	chunks := make([]chunk, len(fd.Chunks))
	for i := range chunks {
		chunks[i].Pieces = make([][]piece, file.staticMetadata.staticErasureCode.NumPieces())
		chunks[i].Index = i
	}

	// Populate the pubKeyTable of the file and add the pieces.
	pubKeyMap := make(map[string]uint32)
	for chunkIndex, chunk := range fd.Chunks {
		for pieceIndex, pieceSet := range chunk.Pieces {
			for _, p := range pieceSet {
				// Check if we already added that public key.
				tableOffset, exists := pubKeyMap[string(p.HostPubKey.Key)]
				if !exists {
					tableOffset = uint32(len(file.pubKeyTable))
					pubKeyMap[string(p.HostPubKey.Key)] = tableOffset
					file.pubKeyTable = append(file.pubKeyTable, HostPublicKey{
						PublicKey: p.HostPubKey,
						Used:      true,
					})
				}
				// Add the piece to the UploFile.
				chunks[chunkIndex].Pieces[pieceIndex] = append(chunks[chunkIndex].Pieces[pieceIndex], piece{
					HostTableOffset: tableOffset,
					MerkleRoot:      p.MerkleRoot,
				})
			}
		}
	}

	// Save file to disk.
	if err := file.saveFile(chunks); err != nil {
		return nil, errors.AddContext(err, "unable to save file")
	}

	// Update the cached fields for progress and uploaded bytes.
	_, _, err = file.UploadProgressAndBytes()
	return file, err
}
