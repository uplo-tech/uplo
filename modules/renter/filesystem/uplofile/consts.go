package uplofile

import (
	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/crypto"
)

const (
	// pageSize is the size of a physical page on disk.
	pageSize = 4096

	// defaultReservedMDPages is the number of pages we reserve for the
	// metadata when we create a new uploFile. Should the metadata ever grow
	// larger than that, new pages are added on demand.
	defaultReservedMDPages = 1

	// updateInsertName is the name of a uploFile update that inserts data at a specific index.
	updateInsertName = "UploFileInsert"

	// updateDeleteName is the name of a uploFile update that deletes the
	// specified file.
	updateDeleteName = "UploFileDelete"

	// updateDeletePartialName is the name of a wal update that deletes the
	// specified file.
	updateDeletePartialName = "PartialChunkDelete"

	// marshaledPieceSize is the size of a piece on disk. It consists of a 4
	// byte pieceIndex, a 4 byte table offset and a hash.
	marshaledPieceSize = 4 + 4 + crypto.HashSize

	// marshaledChunkOverhead is the size of a marshaled chunk on disk minus the
	// encoded pieces. It consists of the 16 byte extension info, a 2 byte
	// length prefix for the pieces, and a 1 byte length for the Stuck field.
	marshaledChunkOverhead = 16 + 2 + 1

	// pubKeyTablePruneThreshold is the number of unused hosts a UploFile can
	// store in its host key table before it is pruned.
	pubKeyTablePruneThreshold = 50
)

// Constants to indicate which part of the partial upload the combined chunk is
// currently at.
const (
	CombinedChunkStatusInvalid    = iota // status wasn't initialized
	CombinedChunkStatusInComplete        // partial chunk is included in an incomplete combined chunk.
	CombinedChunkStatusCompleted         // partial chunk is included in a completed combined chunk.
)

// marshaledChunkSize is a helper method that returns the size of a chunk on
// disk given the number of pieces the chunk contains.
func marshaledChunkSize(numPieces int) int64 {
	return marshaledChunkOverhead + marshaledPieceSize*int64(numPieces)
}

// IsUploFileUpdate is a helper method that makes sure that a wal update belongs
// to the UploFile package.
func IsUploFileUpdate(update writeaheadlog.Update) bool {
	switch update.Name {
	case updateInsertName, updateDeleteName, updateDeletePartialName:
		return true
	default:
		return false
	}
}
