package renter

import (
	"bytes"
	"sync"
	"time"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/errors"
)

// fetchChunkState is a helper struct for coordinating goroutines that are
// attempting to download a chunk for a fanout streamer.
type fetchChunkState struct {
	staticChunkIndex uint64
	staticChunkSize  uint64
	staticDataPieces uint64
	staticMasterKey  crypto.CipherKey

	// staticTimeout defines a timeout that is applied to every chunk download
	staticTimeout time.Duration

	pieces          [][]byte
	piecesCompleted uint64
	piecesFailed    uint64

	doneChan     chan struct{}
	mu           sync.Mutex
	staticRenter *Renter
}

// completed returns whether enough data pieces were retrieved for the chunk to
// be recovered successfully.
func (fcs *fetchChunkState) completed() bool {
	return fcs.piecesCompleted >= fcs.staticDataPieces
}

// managedFailPiece will update the fcs to indicate that an additional piece has
// failed. Nothing will be logged, if the cause of failure is worth logging, the
// log message should be handled by the calling function.
func (fcs *fetchChunkState) managedFailPiece() {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()

	fcs.piecesFailed++
	allTried := fcs.piecesCompleted+fcs.piecesFailed == uint64(len(fcs.pieces))
	if !fcs.completed() && allTried {
		close(fcs.doneChan)
	}
}

// threadedFetchPiece is intended to run as a separate thread which fetches a
// particular piece of a chunk in the fanout.
func (fcs *fetchChunkState) threadedFetchPiece(pieceIndex uint64, pieceRoot crypto.Hash) {
	err := fcs.staticRenter.tg.Add()
	if err != nil {
		return
	}
	defer fcs.staticRenter.tg.Done()

	// Fetch the piece.
	//
	// TODO: This is fetching from 0 to modules.SectorSize, for the final chunk
	// we don't need to fetch the whole piece. Fine for now as it only impacts
	// larger files.
	//
	// TODO: Ideally would be able to insert 'doneChan' as a cancelChan on the
	// DownloadByRoot call.
	pieceData, err := fcs.staticRenter.DownloadByRoot(pieceRoot, 0, modules.SectorSize, fcs.staticTimeout)
	if err != nil {
		fcs.managedFailPiece()
		fcs.staticRenter.log.Debugf("fanout piece download failed for chunk %v, piece %v, root %v of a fanout download file: %v", fcs.staticChunkIndex, pieceIndex, pieceRoot, err)
		return
	}

	// Decrypt the piece.
	key := fcs.staticMasterKey.Derive(fcs.staticChunkIndex, pieceIndex)
	_, err = key.DecryptBytesInPlace(pieceData, 0)
	if err != nil {
		fcs.managedFailPiece()
		fcs.staticRenter.log.Printf("fanout piece decryption failed for chunk %v, piece %v, root %v of a fanout download file: %v", fcs.staticChunkIndex, pieceIndex, pieceRoot, err)
		return
	}

	// Update the fetchChunkState to reflect that the piece has been recovered.
	fcs.mu.Lock()
	defer fcs.mu.Unlock()
	// If the chunk is already completed, the data should be discarded.
	if fcs.completed() {
		pieceData = nil
		return
	}
	// Add the piece to the fcs.
	fcs.pieces[pieceIndex] = pieceData
	fcs.piecesCompleted++
	// Close out the chunk download if this was the final piece needed to
	// complete the download.
	if fcs.completed() {
		close(fcs.doneChan)
	}
}

// managedFetchChunk will grab the data of a specific chunk index from the Uplo
// network.
func (fs *fanoutStreamBufferDataSource) managedFetchChunk(chunkIndex uint64) ([]byte, error) {
	// Input verification.
	if chunkIndex*fs.staticChunkSize >= fs.staticLayout.Filesize {
		return nil, errors.New("requesting a chunk index that does not exist within the file")
	}
	if int(fs.staticLayout.FanoutDataPieces) > len(fs.staticChunks[chunkIndex]) {
		return nil, errors.New("not enough pieces in the chunk to recover the chunk")
	}

	// Build the state that is used to coordinate the goroutines fetching
	// various pieces.
	fcs := fetchChunkState{
		staticChunkIndex: chunkIndex,
		staticChunkSize:  fs.staticChunkSize,
		staticDataPieces: uint64(fs.staticLayout.FanoutDataPieces),
		staticMasterKey:  fs.staticMasterKey,

		staticTimeout: fs.staticTimeout,

		pieces: make([][]byte, len(fs.staticChunks[chunkIndex])),

		doneChan:     make(chan struct{}),
		staticRenter: fs.staticRenter,
	}

	// Spin up one goroutine per piece to fetch the pieces.
	//
	// TODO: Currently this means that if there are 30 pieces for a chunk, all
	// 30 pieces will be requested. This is wasteful, much better would be to
	// attempt to fetch some fraction with some amount of overdrive.
	var blankHash crypto.Hash
	for i := uint64(0); i < uint64(len(fcs.pieces)); i++ {
		// Skip pieces where the Merkle root is not supplied.
		if fs.staticChunks[chunkIndex][i] == blankHash {
			fcs.managedFailPiece()
			fcs.staticRenter.log.Debugf("skipping piece %v of chunk %v in fanout download because the merkle root is blank", i, chunkIndex)
			continue
		}

		// Spin up a thread to fetch this piece.
		go fcs.threadedFetchPiece(i, fs.staticChunks[chunkIndex][i])
	}
	<-fcs.doneChan

	// Check how many pieces came back.
	fcs.mu.Lock()
	completed := fcs.completed()
	pieces := fcs.pieces
	fcs.mu.Unlock()
	if !completed {
		fcs.mu.Lock()
		for i := 0; i < len(fcs.pieces); i++ {
			fcs.pieces[i] = nil
		}
		fcs.pieces = nil
		pieces = nil
		fcs.mu.Unlock()
		return nil, errors.New("not enough pieces could be recovered to fetch chunk")
	}

	// Special case: if there is only 1 piece, there is no need to run erasure
	// coding.
	if len(pieces) == 1 {
		return pieces[0], nil
	}

	// Recover the data.
	buf := bytes.NewBuffer(nil)
	chunkSize := (modules.SectorSize - fs.staticLayout.CipherType.Overhead()) * uint64(fs.staticLayout.FanoutDataPieces)
	err := fs.staticErasureCoder.Recover(pieces, chunkSize, buf)
	if err != nil {
		return nil, errors.New("erasure decoding of chunk failed.")
	}
	return buf.Bytes(), nil
}
