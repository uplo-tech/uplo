package renter

import (
	"context"
	"testing"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
)

// TestJobExpectedJobTime is a small unit test that verifies the result of
// 'callExpectedJobTime' on the jobReadQueue
func TestJobExpectedJobTime(t *testing.T) {
	t.Parallel()

	dur80MS := 80 * time.Millisecond
	dur120MS := 120 * time.Millisecond

	// randTimeMS returns a random duration between 40 and 80ms
	randTimeMS := func() time.Duration {
		return time.Duration(fastrand.Intn(40)+80) * time.Millisecond
	}

	w := new(worker)
	w.initJobReadQueue()
	jrq := w.staticJobReadQueue
	for _, readLength := range []uint64{1 << 16, 1 << 20, 1 << 24} {
		// update metrics couple of times, due to the decay the estimate might
		// dip below the 80ms threshold after one or two jobs.
		for i := 0; i < 10; i++ {
			jrq.callUpdateJobTimeMetrics(readLength, randTimeMS())
		}
		// update the jobqueue a bunch of times with random read times between
		// 80 and 120ms and assert the expected job time keeps returning a value
		// between those boundaries
		for i := 0; i < 1000; i++ {
			randJobTime := time.Duration(fastrand.Intn(40)+80) * time.Millisecond
			jrq.callUpdateJobTimeMetrics(readLength, randJobTime)
			ejt := jrq.callExpectedJobTime(readLength)
			if ejt < dur80MS || ejt > dur120MS {
				t.Fatal("unexpected", ejt)
			}
		}
	}
}

// TestJobReadMetadata verifies the job metadata is set on the job read response
func TestJobReadMetadata(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	wt, err := newWorkerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	w := wt.worker

	// allow the worker some time to fetch a PT and fund its EA
	err = build.Retry(600, 100*time.Millisecond, func() error {
		if w.staticAccount.managedMinExpectedBalance().IsZero() {
			return errors.New("account not funded yet")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// add sector data to the host
	sectorData := fastrand.Bytes(int(modules.SectorSize))
	sectorRoot := crypto.MerkleRoot(sectorData)
	err = wt.host.AddSector(sectorRoot, sectorData)
	if err != nil {
		t.Fatal(err)
	}

	// add job to the worker
	ctx := context.Background()
	responseChan := make(chan *jobReadResponse)

	jhs := &jobReadSector{
		jobRead: jobRead{
			staticResponseChan: responseChan,
			staticLength:       modules.SectorSize,

			// set metadata, set it to something different than the sector root
			// to ensure the response contains the sector given in the metadata
			staticSector: crypto.Hash{1, 2, 3},

			jobGeneric: &jobGeneric{
				staticCtx:   ctx,
				staticQueue: w.staticJobReadQueue,
			},
		},
		staticSector: sectorRoot,
		staticOffset: 0,
	}
	if !w.staticJobReadQueue.callAdd(jhs) {
		t.Fatal("Could not add job to queue")
	}

	// receive response and verify if metadata is set
	jrr := <-responseChan
	if jrr.staticSectorRoot != (crypto.Hash{1, 2, 3}) {
		t.Fatal("unexpected", jrr.staticSectorRoot, sectorRoot)
	}
	if jrr.staticWorker == nil || jrr.staticWorker.staticHostPubKeyStr != wt.host.PublicKey().String() {
		t.Fatal("unexpected")
	}
}
