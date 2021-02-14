package proto

import (
	"net"
	"sync"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/ratelimit"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

// cachedMerkleRoot calculates the root of a set of existing Merkle roots.
func cachedMerkleRoot(roots []crypto.Hash) crypto.Hash {
	tree := crypto.NewCachedTree(sectorHeight) // NOTE: height is not strictly necessary here
	for _, h := range roots {
		tree.Push(h)
	}
	return tree.Root()
}

// A Editor modifies a Contract by calling the revise RPC on a host. It
// Editors are NOT thread-safe; calls to Upload must happen in serial.
type Editor struct {
	contractID  types.FileContractID
	contractSet *ContractSet
	conn        net.Conn
	closeChan   chan struct{}
	deps        modules.Dependencies
	hdb         hostDB
	host        modules.HostDBEntry
	once        sync.Once

	height types.BlockHeight
}

// shutdown terminates the revision loop and signals the goroutine spawned in
// NewEditor to return.
func (he *Editor) shutdown() {
	extendDeadline(he.conn, modules.NegotiateSettingsTime)
	// don't care about these errors
	_, _ = verifySettings(he.conn, he.host)
	_ = modules.WriteNegotiationStop(he.conn)
	close(he.closeChan)
}

// Close cleanly terminates the revision loop with the host and closes the
// connection.
func (he *Editor) Close() error {
	// using once ensures that Close is idempotent
	he.once.Do(he.shutdown)
	return he.conn.Close()
}

// HostSettings returns the settings that are active in the current session.
func (he *Editor) HostSettings() modules.HostExternalSettings {
	return he.host.HostExternalSettings
}

// Upload negotiates a revision that adds a sector to a file contract.
func (he *Editor) Upload(data []byte) (_ modules.RenterContract, _ crypto.Hash, err error) {
	// Acquire the contract.
	sc, haveContract := he.contractSet.Acquire(he.contractID)
	if !haveContract {
		return modules.RenterContract{}, crypto.Hash{}, errors.New("contract not present in contract set")
	}
	defer he.contractSet.Return(sc)
	contract := sc.header // for convenience

	// calculate price
	// TODO: height is never updated, so we'll wind up overpaying on long-running uploads
	blockBytes := types.NewCurrency64(modules.SectorSize * uint64(contract.LastRevision().NewWindowEnd-he.height))
	sectorStoragePrice := he.host.StoragePrice.Mul(blockBytes)
	sectorBandwidthPrice := he.host.UploadBandwidthPrice.Mul64(modules.SectorSize)
	sectorCollateral := he.host.Collateral.Mul(blockBytes)

	// to mitigate small errors (e.g. differing block heights), fudge the
	// price and collateral by 0.2%. This is only applied to hosts above
	// v1.0.1; older hosts use stricter math.
	if build.VersionCmp(he.host.Version, "1.0.1") > 0 {
		sectorStoragePrice = sectorStoragePrice.MulFloat(1 + hostPriceLeeway)
		sectorBandwidthPrice = sectorBandwidthPrice.MulFloat(1 + hostPriceLeeway)
		sectorCollateral = sectorCollateral.MulFloat(1 - hostPriceLeeway)
	}

	sectorPrice := sectorStoragePrice.Add(sectorBandwidthPrice)
	if contract.RenterFunds().Cmp(sectorPrice) < 0 {
		return modules.RenterContract{}, crypto.Hash{}, errors.New("contract has insufficient funds to support upload")
	}
	if contract.LastRevision().MissedHostOutput().Value.Cmp(sectorCollateral) < 0 {
		sectorCollateral = contract.LastRevision().MissedHostOutput().Value
	}

	// calculate the new Merkle root
	sectorRoot := crypto.MerkleRoot(data)
	merkleRoot := sc.merkleRoots.checkNewRoot(sectorRoot)

	// create the action and revision
	actions := []modules.RevisionAction{{
		Type:        modules.ActionInsert,
		SectorIndex: uint64(sc.merkleRoots.len()),
		Data:        data,
	}}
	rev, err := newUploadRevision(contract.LastRevision(), merkleRoot, sectorPrice, sectorCollateral)
	if err != nil {
		return modules.RenterContract{}, crypto.Hash{}, errors.AddContext(err, "Error creating new upload revision")
	}

	// run the revision iteration
	defer func() {
		// Increase Successful/Failed interactions accordingly
		if err != nil {
			// If the host was OOS, we update the contract utility.
			if modules.IsOOSErr(err) {
				u := sc.Utility()
				u.GoodForUpload = false // Stop uploading to such a host immediately.
				u.LastOOSErr = he.height
				err = errors.Compose(err, sc.UpdateUtility(u))
			}
			he.hdb.IncrementFailedInteractions(he.host.PublicKey)
			err = errors.Extend(err, modules.ErrHostFault)
		} else {
			he.hdb.IncrementSuccessfulInteractions(he.host.PublicKey)
		}

		// reset deadline
		extendDeadline(he.conn, time.Hour)
	}()

	// initiate revision
	extendDeadline(he.conn, modules.NegotiateSettingsTime)
	if err := startRevision(he.conn, he.host); err != nil {
		return modules.RenterContract{}, crypto.Hash{}, err
	}

	// record the change we are about to make to the contract. If we lose power
	// mid-revision, this allows us to restore either the pre-revision or
	// post-revision contract.
	walTxn, err := sc.managedRecordAppendIntent(rev, sectorRoot, sectorStoragePrice, sectorBandwidthPrice)
	if err != nil {
		return modules.RenterContract{}, crypto.Hash{}, err
	}

	// send actions
	extendDeadline(he.conn, modules.NegotiateFileContractRevisionTime)
	if err := encoding.WriteObject(he.conn, actions); err != nil {
		return modules.RenterContract{}, crypto.Hash{}, err
	}

	// Disrupt here before sending the signed revision to the host.
	if he.deps.Disrupt("InterruptUploadBeforeSendingRevision") {
		return modules.RenterContract{}, crypto.Hash{},
			errors.New("InterruptUploadBeforeSendingRevision disrupt")
	}

	// send revision to host and exchange signatures
	extendDeadline(he.conn, connTimeout)
	signedTxn, err := negotiateRevision(he.conn, rev, contract.SecretKey, he.height)
	if errors.Contains(err, modules.ErrStopResponse) {
		// if host gracefully closed, close our connection as well; this will
		// cause the next operation to fail
		he.conn.Close()
	} else if err != nil {
		return modules.RenterContract{}, crypto.Hash{}, err
	}

	// Disrupt here before updating the contract.
	if he.deps.Disrupt("InterruptUploadAfterSendingRevision") {
		return modules.RenterContract{}, crypto.Hash{},
			errors.New("InterruptUploadAfterSendingRevision disrupt")
	}

	// update contract
	err = sc.managedCommitAppend(walTxn, signedTxn, sectorStoragePrice, sectorBandwidthPrice)
	if err != nil {
		return modules.RenterContract{}, crypto.Hash{}, err
	}

	return sc.Metadata(), sectorRoot, nil
}

// NewEditor initiates the contract revision process with a host, and returns
// an Editor.
func (cs *ContractSet) NewEditor(host modules.HostDBEntry, id types.FileContractID, currentHeight types.BlockHeight, hdb hostDB, cancel <-chan struct{}) (_ *Editor, err error) {
	sc, ok := cs.Acquire(id)
	if !ok {
		return nil, errors.New("new editor unable to find contract in contract set")
	}
	defer cs.Return(sc)
	contract := sc.header

	// Increase Successful/Failed interactions accordingly
	defer func() {
		// a revision mismatch is not necessarily the host's fault
		if err != nil && !IsRevisionMismatch(err) {
			hdb.IncrementFailedInteractions(contract.HostPublicKey())
			err = errors.Extend(err, modules.ErrHostFault)
		} else if err == nil {
			hdb.IncrementSuccessfulInteractions(contract.HostPublicKey())
		}
	}()

	conn, closeChan, err := initiateRevisionLoop(host, sc, modules.RPCReviseContract, cancel, cs.staticRL)
	if err != nil {
		return nil, errors.AddContext(err, "failed to initiate revision loop")
	}
	// if we succeeded, we can safely discard the unappliedTxns
	if err := sc.clearUnappliedTxns(); err != nil {
		return nil, errors.AddContext(err, "failed to clear unapplied txns")
	}

	// the host is now ready to accept revisions
	return &Editor{
		host:        host,
		hdb:         hdb,
		contractID:  id,
		contractSet: cs,
		conn:        conn,
		closeChan:   closeChan,
		deps:        cs.staticDeps,

		height: currentHeight,
	}, nil
}

// initiateRevisionLoop initiates either the editor or downloader loop with
// host, depending on which rpc was passed.
func initiateRevisionLoop(host modules.HostDBEntry, contract *SafeContract, rpc types.Specifier, cancel <-chan struct{}, rl *ratelimit.RateLimit) (net.Conn, chan struct{}, error) {
	c, err := (&net.Dialer{
		Cancel:  cancel,
		Timeout: 45 * time.Second, // TODO: Constant
	}).Dial("tcp", string(host.NetAddress))
	if err != nil {
		return nil, nil, err
	}
	// Apply the local ratelimit.
	conn := ratelimit.NewRLConn(c, rl, cancel)
	// Apply the global ratelimit.
	conn = ratelimit.NewRLConn(conn, modules.GlobalRateLimits, cancel)

	closeChan := make(chan struct{})
	go func() {
		select {
		case <-cancel:
			conn.Close()
		case <-closeChan:
		}
	}()

	// allot 2 minutes for RPC request + revision exchange
	extendDeadline(conn, modules.NegotiateRecentRevisionTime)
	defer extendDeadline(conn, time.Hour)
	if err := encoding.WriteObject(conn, rpc); err != nil {
		conn.Close()
		close(closeChan)
		return nil, closeChan, errors.New("couldn't initiate RPC: " + err.Error())
	}
	if err := verifyRecentRevision(conn, contract, host.Version); err != nil {
		conn.Close() // TODO: close gracefully if host has entered revision loop
		close(closeChan)
		return nil, closeChan, errors.AddContext(err, "verifyRecentRevision failed")
	}
	return conn, closeChan, nil
}
