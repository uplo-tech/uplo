package contractor

import (
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// IsOffline indicates whether a contract's host should be considered offline,
// based on its scan metrics.
func (c *Contractor) IsOffline(pk types.UploPublicKey) bool {
	host, ok, err := c.hdb.Host(pk)
	if !ok || err != nil {
		// No host or error, assume offline.
		return true
	}
	return isOffline(host)
}

// isOffline indicates whether a host should be considered offline, based on
// its scan metrics.
func isOffline(host modules.HostDBEntry) bool {
	// See if the host has a scan history.
	if len(host.ScanHistory) < 1 {
		// No scan history, assume offline.
		return true
	}
	// If we only have one scan in the history we return false if it was
	// successful.
	if len(host.ScanHistory) == 1 {
		return !host.ScanHistory[0].Success
	}
	// Otherwise we use the last 2 scans. This way a short connectivity problem
	// won't mark the host as offline.
	success1 := host.ScanHistory[len(host.ScanHistory)-1].Success
	success2 := host.ScanHistory[len(host.ScanHistory)-2].Success
	return !(success1 || success2)
}
