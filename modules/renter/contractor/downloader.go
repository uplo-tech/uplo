package contractor

import (
	"sync"

	"github.com/uplo-tech/errors"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/proto"
	"github.com/uplo-tech/uplo/types"
)

var errInvalidDownloader = errors.New("downloader has been invalidated because its contract is being renewed")

// ErrContractRenewing is returned by operations that can't be completed due to
// the contract being renewed.
var ErrContractRenewing = errors.New("currently renewing that contract")

// An Downloader retrieves sectors from with a host. It requests one sector at
// a time, and revises the file contract to transfer money to the host
// proportional to the data retrieved.
type Downloader interface {
	// Download requests the specified sector data.
	Download(root crypto.Hash, offset, length uint32) ([]byte, error)

	// HostSettings returns the settings that are active in the current
	// downloader session.
	HostSettings() modules.HostExternalSettings

	// Close terminates the connection to the host.
	Close() error
}

// A hostDownloader retrieves sectors by calling the download RPC on a host.
// It implements the Downloader interface. hostDownloaders are safe for use by
// multiple goroutines.
type hostDownloader struct {
	clients      int // safe to Close when 0
	contractID   types.FileContractID
	contractor   *Contractor
	downloader   *proto.Downloader
	hostSettings modules.HostExternalSettings
	invalid      bool // true if invalidate has been called
	mu           sync.Mutex
}

// invalidate sets the invalid flag and closes the underlying
// proto.Downloader. Once invalidate returns, the hostDownloader is guaranteed
// to not further revise its contract. This is used during contract renewal to
// prevent a Downloader from revising a contract mid-renewal.
func (hd *hostDownloader) invalidate() {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	if !hd.invalid {
		hd.downloader.Close()
		hd.invalid = true
	}
	hd.contractor.mu.Lock()
	delete(hd.contractor.downloaders, hd.contractID)
	hd.contractor.mu.Unlock()
}

// Close cleanly terminates the download loop with the host and closes the
// connection.
func (hd *hostDownloader) Close() error {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	hd.clients--
	// Close is a no-op if invalidate has been called, or if there are other
	// clients still using the hostDownloader.
	if hd.invalid || hd.clients > 0 {
		return nil
	}
	hd.invalid = true
	hd.contractor.mu.Lock()
	delete(hd.contractor.downloaders, hd.contractID)
	hd.contractor.mu.Unlock()
	return hd.downloader.Close()
}

// HostSettings returns the settings of the host that the downloader connects
// to.
func (hd *hostDownloader) HostSettings() modules.HostExternalSettings {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	return hd.hostSettings
}

// Download retrieves the requested sector data and revises the underlying
// contract to pay the host proportionally to the data retrieved.
func (hd *hostDownloader) Download(root crypto.Hash, offset, length uint32) ([]byte, error) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	if hd.invalid {
		return nil, errInvalidDownloader
	}
	_, data, err := hd.downloader.Download(root, offset, length)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Downloader returns a Downloader object that can be used to download sectors
// from a host.
func (c *Contractor) Downloader(pk types.UploPublicKey, cancel <-chan struct{}) (_ Downloader, err error) {
	c.mu.RLock()
	id, gotID := c.pubKeysToContractID[pk.String()]
	cachedDownloader, haveDownloader := c.downloaders[id]
	cachedSession, haveSession := c.sessions[id]
	height := c.blockHeight
	renewing := c.renewing[id]
	c.mu.RUnlock()
	if !gotID {
		return nil, errors.New("failed to get filecontract id from key")
	}
	if renewing {
		return nil, ErrContractRenewing
	} else if haveDownloader {
		// increment number of clients and return
		cachedDownloader.mu.Lock()
		cachedDownloader.clients++
		cachedDownloader.mu.Unlock()
		return cachedDownloader, nil
	} else if haveSession {
		cachedSession.mu.Lock()
		cachedSession.clients++
		cachedSession.mu.Unlock()
		return cachedSession, nil
	}

	// Fetch the contract and host.
	contract, haveContract := c.staticContracts.View(id)
	if !haveContract {
		return nil, errors.New("contract not found in renter's contract set")
	}
	host, haveHost, err := c.hdb.Host(contract.HostPublicKey)
	if err != nil {
		return nil, errors.AddContext(err, "error getting host from hostdb:")
	}
	if height > contract.EndHeight {
		return nil, errors.New("contract has already ended")
	} else if !haveHost {
		return nil, errors.New("no record of that host")
	} else if host.DownloadBandwidthPrice.Cmp(maxDownloadPrice) > 0 {
		return nil, errTooExpensive
	}

	// If host is >= 1.4.0, use the new renter-host protocol.
	if build.VersionCmp(host.Version, "1.4.0") >= 0 {
		return c.Session(pk, cancel)
	}

	// create downloader
	d, err := c.staticContracts.NewDownloader(host, contract.ID, height, c.hdb, cancel)
	if err != nil {
		return nil, err
	}

	// cache downloader
	hd := &hostDownloader{
		clients:    1,
		contractor: c,
		downloader: d,
		contractID: id,
	}
	c.mu.Lock()
	c.downloaders[contract.ID] = hd
	c.mu.Unlock()

	return hd, nil
}
