package uplodir

import (
	"os"
)

// DirReader is a helper type that allows reading a raw .uplodir from disk while
// keeping the file in memory locked.
type DirReader struct {
	f  *os.File
	sd *uplodir
}

// Close closes the underlying file.
func (sdr *DirReader) Close() error {
	defer sdr.sd.mu.Unlock()
	return sdr.f.Close()
}

// Read calls Read on the underlying file.
func (sdr *DirReader) Read(b []byte) (int, error) {
	return sdr.f.Read(b)
}

// Stat returns the FileInfo of the underlying file.
func (sdr *DirReader) Stat() (os.FileInfo, error) {
	return sdr.f.Stat()
}

// DirReader creates a io.ReadCloser that can be used to read the raw uplodir
// from disk.
func (sd *uplodir) DirReader() (*DirReader, error) {
	sd.mu.Lock()
	if sd.deleted {
		sd.mu.Unlock()
		return nil, ErrDeleted
	}
	// Open file.
	f, err := os.Open(sd.mdPath())
	if err != nil {
		sd.mu.Unlock()
		return nil, err
	}
	return &DirReader{
		sd: sd,
		f:  f,
	}, nil
}
