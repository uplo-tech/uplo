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

var errInvalidEditor = errors.New("editor has been invalidated because its contract is being renewed")

// An Editor modifies a Contract by communicating with a host. It uses the
// contract revision protocol to send modification requests to the host.
// Editors are the means by which the renter uploads data to hosts.
type Editor interface {
	// Upload revises the underlying contract to store the new data. It
	// returns the Merkle root of the data.
	Upload(data []byte) (root crypto.Hash, err error)

	// Address returns the address of the host.
	Address() modules.NetAddress

	// ContractID returns the FileContractID of the contract.
	ContractID() types.FileContractID

	// EndHeight returns the height at which the contract ends.
	EndHeight() types.BlockHeight

	// Close terminates the connection to the host.
	Close() error

	// HostSettings returns the host settings that are currently active for the
	// underlying session.
	HostSettings() modules.HostExternalSettings
}

// A hostEditor modifies a Contract by calling the revise RPC on a host. It
// implements the Editor interface. hostEditors are safe for use by
// multiple goroutines.
type hostEditor struct {
	clients    int // safe to Close when 0
	contractor *Contractor
	editor     *proto.Editor
	endHeight  types.BlockHeight
	id         types.FileContractID
	invalid    bool // true if invalidate has been called
	netAddress modules.NetAddress

	mu sync.Mutex
}

// invalidate sets the invalid flag and closes the underlying proto.Editor.
// Once invalidate returns, the hostEditor is guaranteed to not further revise
// its contract. This is used during contract renewal to prevent an Editor
// from revising a contract mid-renewal.
func (he *hostEditor) invalidate() {
	he.mu.Lock()
	defer he.mu.Unlock()
	if !he.invalid {
		he.editor.Close()
		he.invalid = true
	}
	he.contractor.mu.Lock()
	delete(he.contractor.editors, he.id)
	he.contractor.mu.Unlock()
}

// Address returns the NetAddress of the host.
func (he *hostEditor) Address() modules.NetAddress { return he.netAddress }

// ContractID returns the id of the contract being revised.
func (he *hostEditor) ContractID() types.FileContractID { return he.id }

// EndHeight returns the height at which the host is no longer obligated to
// store the file.
func (he *hostEditor) EndHeight() types.BlockHeight { return he.endHeight }

// Close cleanly terminates the revision loop with the host and closes the
// connection.
func (he *hostEditor) Close() error {
	he.mu.Lock()
	defer he.mu.Unlock()
	he.clients--
	// Close is a no-op if invalidate has been called, or if there are other
	// clients still using the hostEditor.
	if he.invalid || he.clients > 0 {
		return nil
	}
	he.invalid = true
	he.contractor.mu.Lock()
	delete(he.contractor.editors, he.id)
	he.contractor.mu.Unlock()
	return he.editor.Close()
}

// HostSettings returns the host settings that are currently active for the
// underlying session.
func (he *hostEditor) HostSettings() modules.HostExternalSettings {
	return he.editor.HostSettings()
}

// Upload negotiates a revision that adds a sector to a file contract.
func (he *hostEditor) Upload(data []byte) (_ crypto.Hash, err error) {
	he.mu.Lock()
	defer he.mu.Unlock()
	if he.invalid {
		return crypto.Hash{}, errInvalidEditor
	}

	// Perform the upload.
	_, sectorRoot, err := he.editor.Upload(data)
	if err != nil {
		return crypto.Hash{}, err
	}
	return sectorRoot, nil
}

// Editor returns a Editor object that can be used to upload, modify, and
// delete sectors on a host.
func (c *Contractor) Editor(pk types.UploPublicKey, cancel <-chan struct{}) (_ Editor, err error) {
	c.mu.RLock()
	id, gotID := c.pubKeysToContractID[pk.String()]
	cachedEditor, haveEditor := c.editors[id]
	cachedSession, haveSession := c.sessions[id]
	height := c.blockHeight
	renewing := c.renewing[id]
	c.mu.RUnlock()
	if !gotID {
		return nil, errors.New("failed to get filecontract id from key")
	}
	if renewing {
		// Cannot use the editor if the contract is being renewed.
		return nil, ErrContractRenewing
	} else if haveEditor {
		// This editor already exists. Mark that there are now two routines
		// using the editor, and then return the editor that already exists.
		cachedEditor.mu.Lock()
		cachedEditor.clients++
		cachedEditor.mu.Unlock()
		return cachedEditor, nil
	} else if haveSession {
		// This session already exists.
		cachedSession.mu.Lock()
		cachedSession.clients++
		cachedSession.mu.Unlock()
		return cachedSession, nil
	}

	// Check that the contract and host are both available, and run some brief
	// sanity checks to see that the host is not swindling us.
	contract, haveContract := c.staticContracts.View(id)
	if !haveContract {
		return nil, errors.New("contract was not found in the renter's contract set")
	}
	host, haveHost, err := c.hdb.Host(contract.HostPublicKey)
	if err != nil {
		return nil, errors.AddContext(err, "error getting host from hostdb:")
	} else if height > contract.EndHeight {
		return nil, errors.New("contract has already ended")
	} else if !haveHost {
		return nil, errors.New("no record of that host")
	} else if host.Filtered {
		return nil, errors.New("host is blacklisted")
	} else if host.StoragePrice.Cmp(maxStoragePrice) > 0 {
		return nil, errTooExpensive
	} else if host.UploadBandwidthPrice.Cmp(maxUploadPrice) > 0 {
		return nil, errTooExpensive
	}

	// If host is >= 1.4.0, use the new renter-host protocol.
	if build.VersionCmp(host.Version, "1.4.0") >= 0 {
		return c.Session(pk, cancel)
	}

	// Create the editor.
	e, err := c.staticContracts.NewEditor(host, contract.ID, height, c.hdb, cancel)
	if err != nil {
		return nil, err
	}

	// cache editor
	he := &hostEditor{
		clients:    1,
		contractor: c,
		editor:     e,
		endHeight:  contract.EndHeight,
		id:         id,
		netAddress: host.NetAddress,
	}
	c.mu.Lock()
	c.editors[contract.ID] = he
	c.mu.Unlock()

	return he, nil
}
