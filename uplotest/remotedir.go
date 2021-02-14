package uplotest

import (
	"github.com/uplo-tech/uplo/modules"
)

type (
	// RemoteDir is a helper struct that represents a directory on the Uplo
	// network.
	RemoteDir struct {
		uplopath modules.UploPath
	}
)

// UploPath returns the uplopath of a remote directory.
func (rd *RemoteDir) UploPath() modules.UploPath {
	return rd.uplopath
}
