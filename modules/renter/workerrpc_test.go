package renter

import (
	"testing"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// TestExecuteProgramUsedBandwidth verifies the bandwidth used by executing
// various MDM programs on the host
func TestExecuteProgramUsedBandwidth(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create a new worker tester
	wt, err := newWorkerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := wt.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	// wait until we have a valid pricetable
	err = build.Retry(100, 100*time.Millisecond, func() error {
		if !wt.worker.staticPriceTable().staticValid() {
			return errors.New("price table not updated yet")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("HasSector", func(t *testing.T) {
		testExecuteProgramUsedBandwidthHasSector(t, wt)
	})

	t.Run("ReadSector", func(t *testing.T) {
		testExecuteProgramUsedBandwidthReadSector(t, wt)
	})
}

// testExecuteProgramUsedBandwidthHasSector verifies the bandwidth consumed by a
// HasSector program
func testExecuteProgramUsedBandwidthHasSector(t *testing.T, wt *workerTester) {
	w := wt.worker

	// create a dummy program
	pt := wt.staticPriceTable().staticPriceTable
	pb := modules.NewProgramBuilder(&pt, 0)
	pb.AddHasSectorInstruction(crypto.Hash{})
	p, data := pb.Program()
	cost, _, _ := pb.Cost(true)
	ulBandwidth, dlBandwidth := new(jobHasSector).callExpectedBandwidth()
	bandwidthCost := modules.MDMBandwidthCost(pt, ulBandwidth, dlBandwidth)
	cost = cost.Add(bandwidthCost)

	// execute it
	_, limit, err := w.managedExecuteProgram(p, data, types.FileContractID{}, cost)
	if err != nil {
		t.Fatal(err)
	}

	// ensure bandwidth is as we expected
	expectedDownload := uint64(1460)
	if limit.Downloaded() != expectedDownload {
		t.Errorf("Expected HasSector program to consume %v download bandwidth, instead it consumed %v", expectedDownload, limit.Downloaded())
	}

	expectedUpload := uint64(1460)
	if limit.Uploaded() != expectedUpload {
		t.Errorf("Expected HasSector program to consume %v upload bandwidth, instead it consumed %v", expectedUpload, limit.Uploaded())
	}

	// log the bandwidth used
	t.Logf("Used bandwidth (has sector program): %v down, %v up", limit.Downloaded(), limit.Uploaded())
}

// testExecuteProgramUsedBandwidthReadSector verifies the bandwidth consumed by
// a ReadSector program
func testExecuteProgramUsedBandwidthReadSector(t *testing.T, wt *workerTester) {
	w := wt.worker

	sectorData := fastrand.Bytes(int(modules.SectorSize))
	sectorRoot := crypto.MerkleRoot(sectorData)
	err := wt.host.AddSector(sectorRoot, sectorData)
	if err != nil {
		t.Fatal("could not add sector to host")
	}

	// create a dummy program
	pt := wt.staticPriceTable().staticPriceTable
	pb := modules.NewProgramBuilder(&pt, 0)
	pb.AddReadSectorInstruction(modules.SectorSize, 0, sectorRoot, true)
	p, data := pb.Program()
	cost, _, _ := pb.Cost(true)

	jrs := new(jobReadSector)
	jrs.staticLength = modules.SectorSize
	ulBandwidth, dlBandwidth := jrs.callExpectedBandwidth()
	bandwidthCost := modules.MDMBandwidthCost(pt, ulBandwidth, dlBandwidth)
	cost = cost.Add(bandwidthCost)

	// execute it
	_, limit, err := w.managedExecuteProgram(p, data, types.FileContractID{}, cost)
	if err != nil {
		t.Fatal(err)
	}

	// ensure bandwidth is as we expected
	expectedDownload := uint64(4380)
	if limit.Downloaded() != expectedDownload {
		t.Errorf("Expected ReadSector program to consume %v download bandwidth, instead it consumed %v", expectedDownload, limit.Downloaded())
	}

	expectedUpload := uint64(1460)
	if limit.Uploaded() != expectedUpload {
		t.Errorf("Expected ReadSector program to consume %v upload bandwidth, instead it consumed %v", expectedUpload, limit.Uploaded())
	}

	// log the bandwidth used
	t.Logf("Used bandwidth (read sector program): %v down, %v up", limit.Downloaded(), limit.Uploaded())
}
