package uplodir

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uplo-tech/writeaheadlog"

	"github.com/uplo-tech/uplo/modules"
)

type (
	// uplodir contains the metadata information about a renter directory
	uplodir struct {
		metadata Metadata

		// path is the path of the uplodir folder.
		path string

		// Utility fields
		deleted bool
		deps    modules.Dependencies
		mu      sync.Mutex
		wal     *writeaheadlog.WAL
	}

	// Metadata is the metadata that is saved to disk as a .uplodir file
	Metadata struct {
		// For each field in the metadata there is an aggregate value and a
		// uplodir specific value. If a field has the aggregate prefix it means
		// that the value takes into account all the uplofiles and uplodirs in the
		// sub tree. The definition of aggregate and uplodir specific values is
		// otherwise the same.
		//
		// Health is the health of the most in need uplofile that is not stuck
		//
		// LastHealthCheckTime is the oldest LastHealthCheckTime of any of the
		// uplofiles in the uplodir and is the last time the health was calculated
		// by the health loop
		//
		// MinRedundancy is the minimum redundancy of any of the uplofiles in the
		// uplodir
		//
		// ModTime is the last time any of the uplofiles in the uplodir was
		// updated
		//
		// NumFiles is the total number of uplofiles in a uplodir
		//
		// NumStuckChunks is the sum of all the Stuck Chunks of any of the
		// uplofiles in the uplodir
		//
		// NumSubDirs is the number of sub-uplodirs in a uplodir
		//
		// Size is the total amount of data stored in the uplofiles of the uplodir
		//
		// StuckHealth is the health of the most in need uplofile in the uplodir,
		// stuck or not stuck

		// The following fields are aggregate values of the uplodir. These values are
		// the totals of the uplodir and any sub uplodirs, or are calculated based on
		// all the values in the subtree
		AggregateHealth              float64   `json:"aggregatehealth"`
		AggregateLastHealthCheckTime time.Time `json:"aggregatelasthealthchecktime"`
		AggregateMinRedundancy       float64   `json:"aggregateminredundancy"`
		AggregateModTime             time.Time `json:"aggregatemodtime"`
		AggregateNumFiles            uint64    `json:"aggregatenumfiles"`
		AggregateNumStuckChunks      uint64    `json:"aggregatenumstuckchunks"`
		AggregateNumSubDirs          uint64    `json:"aggregatenumsubdirs"`
		AggregateRemoteHealth        float64   `json:"aggregateremotehealth"`
		AggregateRepairSize          uint64    `json:"aggregaterepairsize"`
		AggregateSize                uint64    `json:"aggregatesize"`
		AggregateStuckHealth         float64   `json:"aggregatestuckhealth"`
		AggregateStuckSize           uint64    `json:"aggregatestucksize"`

		// Aggregate Skynet Specific Stats
		AggregateSkynetFiles uint64 `json:"aggregateskynetfiles"`
		AggregateSkynetSize  uint64 `json:"aggregateskynetsize"`

		// The following fields are information specific to the uplodir that is not
		// an aggregate of the entire sub directory tree
		Health              float64     `json:"health"`
		LastHealthCheckTime time.Time   `json:"lasthealthchecktime"`
		MinRedundancy       float64     `json:"minredundancy"`
		Mode                os.FileMode `json:"mode"`
		ModTime             time.Time   `json:"modtime"`
		NumFiles            uint64      `json:"numfiles"`
		NumStuckChunks      uint64      `json:"numstuckchunks"`
		NumSubDirs          uint64      `json:"numsubdirs"`
		RemoteHealth        float64     `json:"remotehealth"`
		RepairSize          uint64      `json:"repairsize"`
		Size                uint64      `json:"size"`
		StuckHealth         float64     `json:"stuckhealth"`
		StuckSize           uint64      `json:"stucksize"`

		// Skynet Specific Stats
		SkynetFiles uint64 `json:"skynetfiles"`
		SkynetSize  uint64 `json:"skynetsize"`

		// Version is the used version of the header file.
		Version string `json:"version"`
	}
)

// mdPath returns the path of the uplodir's metadata on disk.
func (sd *uplodir) mdPath() string {
	return filepath.Join(sd.path, modules.uplodirExtension)
}

// Deleted returns the deleted field of the uplodir
func (sd *uplodir) Deleted() bool {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.deleted
}

// Metadata returns the metadata of the uplodir
func (sd *uplodir) Metadata() Metadata {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.metadata
}

// Path returns the path of the uplodir on disk.
func (sd *uplodir) Path() string {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.path
}

// MDPath returns the path of the uplodir's metadata on disk.
func (sd *uplodir) MDPath() string {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.mdPath()
}
