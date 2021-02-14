package uplotest

import (
	"sync"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
)

type (
	// RemoteFile is a helper struct that represents a file uploaded to the Uplo
	// network.
	RemoteFile struct {
		checksum crypto.Hash
		uploPath  modules.UploPath
		root     bool
		mu       sync.Mutex
	}
)

// Checksum returns the checksum of a remote file.
func (rf *RemoteFile) Checksum() crypto.Hash {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.checksum
}

// Root returns whether the uplopath needs to be treated as an absolute path.
func (rf *RemoteFile) Root() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.root
}

// UploPath returns the uploPath of a remote file.
func (rf *RemoteFile) UploPath() modules.UploPath {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.uploPath
}
