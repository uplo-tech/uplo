package renterhost

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/log"
	"github.com/uplo-tech/ratelimit"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/modules/renter/proto"
	"github.com/uplo-tech/uplo/node/api/client"
	"github.com/uplo-tech/uplo/uplotest"
	"github.com/uplo-tech/uplo/types"
)

type stubHostDB struct{}

func (stubHostDB) IncrementSuccessfulInteractions(types.UploPublicKey) error { return nil }
func (stubHostDB) IncrementFailedInteractions(types.UploPublicKey) error     { return nil }

// TestSession tests the new RPC loop by creating a host and requesting new
// RPCs via the proto.Session type.
func TestSession(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	gp := uplotest.GroupParams{
		Hosts:   1,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterHostTestDir(t.Name()), gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// manually grab a renter contract
	renter := tg.Renters()[0]
	rl := ratelimit.NewRateLimit(0, 0, 0)
	cs, err := proto.NewContractSet(filepath.Join(renter.Dir, "renter", "contracts"), rl, new(modules.ProductionDependencies))
	if err != nil {
		t.Fatal(err)
	}
	contract := cs.ViewAll()[0]

	hhg, err := renter.HostDbHostsGet(contract.HostPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := renter.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}

	// begin the RPC session
	s, err := cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err != nil {
		t.Fatal(err)
	}

	// upload a sector
	sector := fastrand.Bytes(int(modules.SectorSize))
	_, root, err := s.Append(sector)
	if err != nil {
		t.Fatal(err)
	}
	// upload another sector, to test Merkle proofs
	_, _, err = s.Append(sector)
	if err != nil {
		t.Fatal(err)
	}

	// download the sector
	_, dsector, err := s.ReadSection(root, 0, uint32(len(sector)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dsector, sector) {
		t.Fatal("downloaded sector does not match")
	}

	// download less than a full sector
	_, partialSector, err := s.ReadSection(root, crypto.SegmentSize*5, crypto.SegmentSize*12)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(partialSector, sector[crypto.SegmentSize*5:crypto.SegmentSize*17]) {
		t.Fatal("downloaded sector does not match")
	}

	// download the sector root
	_, droots, err := s.SectorRoots(modules.LoopSectorRootsRequest{
		RootOffset: 0,
		NumRoots:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if droots[0] != root {
		t.Fatal("downloaded sector root does not match")
	}

	// perform a more complex modification: append+swap+trim
	sector2 := fastrand.Bytes(int(modules.SectorSize))
	_, err = s.Write([]modules.LoopWriteAction{
		{Type: modules.WriteActionAppend, Data: sector2},
		{Type: modules.WriteActionSwap, A: 0, B: 2},
		{Type: modules.WriteActionTrim, A: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	// check that the write was applied correctly
	_, droots, err = s.SectorRoots(modules.LoopSectorRootsRequest{
		RootOffset: 0,
		NumRoots:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if droots[0] != crypto.MerkleRoot(sector2) {
		t.Fatal("updated sector root does not match")
	}

	// shut down and restart the host to ensure the sectors are durable
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	host := tg.Hosts()[0]
	if err := host.RestartNode(); err != nil {
		t.Fatal(err)
	}
	// restarting changes the host's address
	hg, err := host.HostGet()
	if err != nil {
		t.Fatal(err)
	}
	hhg.Entry.HostDBEntry.NetAddress = hg.ExternalSettings.NetAddress
	// initiate session
	s, err = cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, s2data, err := s.ReadSection(droots[0], 0, uint32(len(sector2)))
	if err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(s2data, sector2) {
		t.Fatal("downloaded data does not match")
	}
}

// TestHostLockTimeout tests that the host respects the requested timeout in the
// Lock RPC.
func TestHostLockTimeout(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	gp := uplotest.GroupParams{
		Hosts:   1,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterHostTestDir(t.Name()), gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// manually grab a renter contract
	renter := tg.Renters()[0]
	rl := ratelimit.NewRateLimit(0, 0, 0)
	cs, err := proto.NewContractSet(filepath.Join(renter.Dir, "renter", "contracts"), rl, new(modules.ProductionDependencies))
	if err != nil {
		t.Fatal(err)
	}
	contract := cs.ViewAll()[0]

	hhg, err := renter.HostDbHostsGet(contract.HostPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := renter.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}

	// Begin an RPC session. This will lock the contract.
	s1, err := cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to begin a separate RPC session. This will block while waiting
	// to acquire the contract lock, and eventually fail.
	_, err = cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err == nil || !strings.Contains(err.Error(), "contract is locked by another party") {
		t.Fatal("expected contract lock error, got", err)
	}

	// Try again, but this time, unlock the contract during the timeout period.
	// The new session should successfully acquire the lock.
	time.AfterFunc(3*time.Second, func() {
		if err := s1.Close(); err != nil {
			panic(err) // can't call t.Fatal from goroutine
		}
	})
	s2, err := cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s2.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Attempt to begin a separate RPC session. This will block while waiting to
	// acquire the contract lock. In the meantime, modify the contract, then
	// unlock, allowing the other session to acquire the lock. When it does, it
	// should see the modified contract.
	errCh := make(chan error)
	var lockedContract modules.RenterContract
	go func() {
		// NOTE: the ContractSet uses a local mutex to serialize RPCs, so this
		// test requires a separate ContractSet.
		cs2, err := proto.NewContractSet(filepath.Join(renter.Dir, "renter", "contracts"), rl, new(modules.ProductionDependencies))
		if err != nil {
			errCh <- err
			return
		}
		defer func() {
			if err := cs2.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		s1, err = cs2.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
		if err != nil {
			errCh <- err
			return
		}
		s1.Close()
		lockedContract, _ = cs2.View(contract.ID)
		errCh <- nil
	}()
	time.Sleep(3 * time.Second) // wait for goroutine to start acquiring lock
	contract, _, err = s2.Append(make([]byte, modules.SectorSize))
	if err != nil {
		t.Fatal(err)
	}
	// Unlock, allowing goroutine to proceed
	if err := s2.Unlock(); err != nil {
		t.Fatal(err)
	}
	// check goroutine error
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	// goroutine should acquire same contract
	rev := contract.Transaction.FileContractRevisions[0]
	lockedRev := lockedContract.Transaction.FileContractRevisions[0]
	if rev.NewRevisionNumber != lockedRev.NewRevisionNumber ||
		rev.NewFileMerkleRoot != lockedRev.NewFileMerkleRoot ||
		!rev.ValidRenterPayout().Equals(lockedRev.ValidRenterPayout()) {
		t.Fatal("acquired wrong contract after lock:", rev.NewRevisionNumber, lockedRev.NewRevisionNumber)
	}
}

// TestHostBaseRPCPrice tests that the host rejects RPCs when its base RPC price
// is not respected.
func TestHostBaseRPCPrice(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	gp := uplotest.GroupParams{
		Hosts:   1,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterHostTestDir(t.Name()), gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// manually grab a renter contract
	renter := tg.Renters()[0]
	rl := ratelimit.NewRateLimit(0, 0, 0)
	cs, err := proto.NewContractSet(filepath.Join(renter.Dir, "renter", "contracts"), rl, new(modules.ProductionDependencies))
	if err != nil {
		t.Fatal(err)
	}
	contract := cs.ViewAll()[0]

	hhg, err := renter.HostDbHostsGet(contract.HostPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := renter.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}

	// Begin an RPC session.
	s, err := cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Upload a sector.
	sector := fastrand.Bytes(int(modules.SectorSize))
	_, _, err = s.Append(sector)
	if err != nil {
		t.Fatal(err)
	}

	// Increase the host's base price.
	host := tg.Hosts()[0]
	hg, err := host.HostGet()
	if err != nil {
		t.Fatal(err)
	}
	minDownloadPrice := hg.InternalSettings.MinDownloadBandwidthPrice
	maxRPCPrice := minDownloadPrice.Mul64(modules.MaxBaseRPCPriceVsBandwidth)
	err = host.HostModifySettingPost(client.HostParamMinBaseRPCPrice, maxRPCPrice)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to upload another sector.
	_, _, err = s.Append(sector)
	if err == nil || !strings.Contains(err.Error(), "rejected for high paying renter valid output") {
		t.Fatal("expected underpayment error, got", err)
	}
}

// TestMultiRead tests the Read RPC.
func TestMultiRead(t *testing.T) {
	t.Skip("Test does not pass online due to timing. Needs to be updated")
	t.Parallel()
	gp := uplotest.GroupParams{
		Hosts:   1,
		Renters: 1,
		Miners:  1,
	}
	tg, err := uplotest.NewGroupFromTemplate(renterHostTestDir(t.Name()), gp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// manually grab a renter contract
	renter := tg.Renters()[0]
	rl := ratelimit.NewRateLimit(0, 0, 0)
	cs, err := proto.NewContractSet(filepath.Join(renter.Dir, "renter", "contracts"), rl, new(modules.ProductionDependencies))
	if err != nil {
		t.Fatal(err)
	}
	contract := cs.ViewAll()[0]

	hhg, err := renter.HostDbHostsGet(contract.HostPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := renter.ConsensusGet()
	if err != nil {
		t.Fatal(err)
	}

	// begin the RPC session
	s, err := cs.NewSession(hhg.Entry.HostDBEntry, contract.ID, cg.Height, stubHostDB{}, log.DiscardLogger, nil)
	if err != nil {
		t.Fatal(err)
	}

	// upload a sector
	sector := fastrand.Bytes(int(modules.SectorSize))
	_, root, err := s.Append(sector)
	if err != nil {
		t.Fatal(err)
	}

	// download a single section without interrupting.
	var buf bytes.Buffer
	req := modules.LoopReadRequest{
		Sections: []modules.LoopReadRequestSection{{
			MerkleRoot: root,
			Offset:     0,
			Length:     uint32(modules.SectorSize),
		}},
		MerkleProof: true,
	}
	_, err = s.Read(&buf, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), sector) {
		t.Fatal("downloaded sector does not match")
	}

	// download multiple sections, but interrupt immediately; we should not
	// receive all the sections
	buf.Reset()
	req.Sections = []modules.LoopReadRequestSection{
		{MerkleRoot: root, Offset: 0, Length: uint32(modules.SectorSize)},
		{MerkleRoot: root, Offset: 0, Length: uint32(modules.SectorSize)},
	}
	cancel := make(chan struct{}, 1)
	cancel <- struct{}{}
	_, err = s.Read(&buf, req, cancel)
	if err != nil {
		t.Fatal(err)
	}
	if len(buf.Bytes()) == len(sector)*len(req.Sections) {
		t.Fatal("read did not quit early")
	}
}
