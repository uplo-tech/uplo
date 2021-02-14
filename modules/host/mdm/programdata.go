package mdm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

// programData is a buffer for the program data. It will read packets from r and
// append them to data.
type programData struct {
	// data contains the already received data.
	data modules.ProgramData

	// staticLength is the expected length of the program data. This is the
	// amount of data that was paid for and not more than that will be read from
	// the reader. Less data will be considered an unexpected EOF.
	staticLength uint64

	// readErr contains the first error encountered by threadedFetchData.
	readErr error

	// requests are queued up calls to 'bytes' waiting for the requested data to
	// arrive.
	requests []dataRequest

	// cancel is used to cancel the background thread.
	cancel chan struct{}

	// wg is used to wait for the background thread to finish.
	wg sync.WaitGroup

	mu sync.Mutex
}

type dataRequest struct {
	requiredLength uint64
	c              chan struct{}
}

// openProgramData creates a new programData object from the specified reader. It
// will read from the reader until dataLength is reached.
func openProgramData(r io.Reader, dataLength uint64) *programData {
	pd := &programData{
		cancel:       make(chan struct{}),
		staticLength: dataLength,
	}
	pd.wg.Add(1)
	go func() {
		defer pd.wg.Done()
		pd.threadedFetchData(r)
	}()
	return pd
}

// threadedFetchData fetches the program's data from the underlying reader of
// the ProgramData. It will read from the reader until io.EOF is reached or
// until the maximum number of packets are read.
func (pd *programData) threadedFetchData(r io.Reader) {
	var packet [1024]byte // 1kib
	remainingData := int64(pd.staticLength)
	quit := func(err error) {
		pd.mu.Lock()
		defer pd.mu.Unlock()
		// Remember the error and close all open requests before stopping
		// the loop.
		pd.readErr = err
		for _, r := range pd.requests {
			close(r.c)
		}
	}
	for remainingData > 0 {
		pd.mu.Lock()
		select {
		case <-pd.cancel:
			pd.mu.Unlock()
			quit(errors.New("stop called"))
			return
		default:
		}
		pd.mu.Unlock()
		// Adjust the length of the packet according to the remaining data.
		d := packet[:]
		if remainingData <= int64(cap(d)) {
			d = d[:remainingData]
		}
		n, err := r.Read(d)
		if err != nil {
			quit(err)
			return
		}
		remainingData -= int64(n)
		pd.mu.Lock()
		pd.data = append(pd.data, packet[:n]...)

		// Sort the request and unlock the ones that are ready to be unlocked.
		sort.Slice(pd.requests, func(i, j int) bool {
			return pd.requests[i].requiredLength < pd.requests[j].requiredLength
		})
		for len(pd.requests) > 0 {
			r := pd.requests[0]
			if r.requiredLength > uint64(len(pd.data)) {
				break
			}
			close(r.c)
			pd.requests = pd.requests[1:]
		}
		pd.mu.Unlock()
	}
}

// managedBytes tries to fetch length bytes at offset from the underlying data
// slice of the programData. If the data is not available yet, a request will be
// queued up and the method will block for the data to be read.
func (pd *programData) managedBytes(offset, length uint64) ([]byte, error) {
	// Check if request is valid.
	if offset+length > pd.staticLength {
		return nil, fmt.Errorf("offset+length out of bounds: %v > %v", offset+length, pd.staticLength)
	}
	pd.mu.Lock()
	// Check if data is available already.
	if uint64(len(pd.data)) >= offset+length {
		defer pd.mu.Unlock()
		return pd.data[offset:][:length], nil
	}
	// Check for previous error.
	if pd.readErr != nil {
		defer pd.mu.Unlock()
		return nil, pd.readErr
	}
	// If not, queue up a request.
	c := make(chan struct{})
	pd.requests = append(pd.requests, dataRequest{
		requiredLength: offset + length,
		c:              c,
	})
	pd.mu.Unlock()
	<-c
	pd.mu.Lock()
	defer pd.mu.Unlock()
	// Check if the data is available again. It should be unless there was a
	// reading error.
	outOfBounds := uint64(len(pd.data)) < offset+length
	if outOfBounds && pd.readErr == nil {
		err := errors.New("requested data was out of bounds even though there was no readErr")
		build.Critical(err)
		return nil, err
	} else if outOfBounds && pd.readErr != nil {
		return nil, pd.readErr
	}
	return pd.data[offset:][:length], nil
}

// Uint64 returns the next 8 bytes at the specified offset within the program
// data as an uint64. This call will block if the data at the specified offset
// hasn't been fetched yet.
func (pd *programData) Uint64(offset uint64) (uint64, error) {
	d, err := pd.managedBytes(offset, 8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(d), nil
}

// Hash returns the next crypto.HashSize bytes at the specified offset within
// the program data as a crypto.Hash. This call will block if the data at the
// specified offset hasn't been fetched yet.
func (pd *programData) Hash(offset uint64) (crypto.Hash, error) {
	d, err := pd.managedBytes(offset, crypto.HashSize)
	if err != nil {
		return crypto.Hash{}, err
	}
	var h crypto.Hash
	copy(h[:], d)
	return h, nil
}

// UploPublicKey reads a types.UploPublicKey from the programData. Given an offset
// and a key length. The length includes the specifier size.
func (pd *programData) UploPublicKey(offset, length uint64) (types.UploPublicKey, error) {
	d, err := pd.managedBytes(offset, length)
	if err != nil {
		return types.UploPublicKey{}, err
	}
	var spk types.UploPublicKey
	err = encoding.Unmarshal(d, &spk)
	return spk, err
}

// Signature returns the next crypto.SignatureSize bytes at the specified offset
// within the program data as a crypto.Signature. This call will block if the
// data at the specified offset hasn't been fetched yet.
func (pd *programData) Signature(offset uint64) (crypto.Signature, error) {
	d, err := pd.managedBytes(offset, crypto.SignatureSize)
	if err != nil {
		return crypto.Signature{}, err
	}
	var sig crypto.Signature
	copy(sig[:], d)
	return sig, nil
}

// Bytes returns 'length' bytes from offset 'offset' from the programData.
func (pd *programData) Bytes(offset, length uint64) ([]byte, error) {
	return pd.managedBytes(offset, length)
}

// Len returns the length of the program data.
func (pd *programData) Len() uint64 {
	return pd.staticLength
}

// Close will stop the background thread and wait for it to return.
func (pd *programData) Close() error {
	pd.mu.Lock()
	close(pd.cancel)
	pd.mu.Unlock()
	pd.wg.Wait()
	return nil
}
