package renter

import (
	"context"
	"time"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/errors"
)

type (
	// jobReadSector contains information about a readSector query.
	jobReadSector struct {
		jobRead

		staticOffset uint64
		staticSector crypto.Hash
	}

	// jobReadSectorMetadata contains meta information about a readSector job.
	jobReadSectorMetadata struct {
		staticSector crypto.Hash
	}
)

// callExecute executes the jobReadSector.
func (j *jobReadSector) callExecute() {
	// Track how long the job takes.
	start := time.Now()
	data, err := j.managedReadSector()
	jobTime := time.Since(start)

	// Finish the execution.
	j.jobRead.managedFinishExecute(data, err, jobTime)
}

// managedReadSector returns the sector data for given root.
func (j *jobReadSector) managedReadSector() ([]byte, error) {
	// create the program
	w := j.staticQueue.staticWorker()
	pt := w.staticPriceTable().staticPriceTable
	pb := modules.NewProgramBuilder(&pt, 0) // 0 duration since ReadSector doesn't depend on it.
	pb.AddReadSectorInstruction(j.staticLength, j.staticOffset, j.staticSector, true)
	program, programData := pb.Program()
	cost, _, _ := pb.Cost(true)

	// take into account bandwidth costs
	ulBandwidth, dlBandwidth := j.callExpectedBandwidth()
	bandwidthCost := modules.MDMBandwidthCost(pt, ulBandwidth, dlBandwidth)
	cost = cost.Add(bandwidthCost)

	responses, err := j.jobRead.managedRead(w, program, programData, cost)
	if err != nil {
		return nil, errors.AddContext(err, "jobReadSector: failed to execute managedRead")
	}
	data := responses[0].Output
	proof := responses[0].Proof

	// verify proof
	proofStart := int(j.staticOffset) / crypto.SegmentSize
	proofEnd := int(j.staticOffset+j.staticLength) / crypto.SegmentSize
	if !crypto.VerifyRangeProof(data, proof, proofStart, proofEnd, j.staticSector) {
		return nil, errors.New("proof verification failed")
	}
	return data, nil
}

// newJobReadSector creates a new read sector job.
func (w *worker) newJobReadSector(ctx context.Context, queue *jobReadQueue, respChan chan *jobReadResponse, root crypto.Hash, offset, length uint64) *jobReadSector {
	return &jobReadSector{
		jobRead: jobRead{
			staticResponseChan: respChan,
			staticLength:       length,

			jobGeneric: newJobGeneric(ctx, queue, &jobReadSectorMetadata{staticSector: root}),
		},
		staticOffset: offset,
		staticSector: root,
	}
}

// ReadSector is a helper method to run a ReadSector job with low priority on a
// worker.
func (w *worker) ReadSectorLowPrio(ctx context.Context, root crypto.Hash, offset, length uint64) ([]byte, error) {
	readSectorRespChan := make(chan *jobReadResponse)
	jro := w.newJobReadSector(ctx, w.staticJobLowPrioReadQueue, readSectorRespChan, root, offset, length)

	// Add the job to the queue.
	if !w.staticJobReadQueue.callAdd(jro) {
		return nil, errors.New("worker unavailable")
	}

	// Wait for the response.
	var resp *jobReadResponse
	select {
	case <-ctx.Done():
		return nil, errors.New("Read interrupted")
	case resp = <-readSectorRespChan:
	}
	return resp.staticData, resp.staticErr
}

// ReadSector is a helper method to run a ReadSector job on a worker.
func (w *worker) ReadSector(ctx context.Context, root crypto.Hash, offset, length uint64) ([]byte, error) {
	readSectorRespChan := make(chan *jobReadResponse)
	jro := w.newJobReadSector(ctx, w.staticJobReadQueue, readSectorRespChan, root, offset, length)

	// Add the job to the queue.
	if !w.staticJobReadQueue.callAdd(jro) {
		return nil, errors.New("worker unavailable")
	}

	// Wait for the response.
	var resp *jobReadResponse
	select {
	case <-ctx.Done():
		return nil, errors.New("Read interrupted")
	case resp = <-readSectorRespChan:
	}
	return resp.staticData, resp.staticErr
}

// readSectorJobExpectedBandwidth is a helper function that returns the expected
// bandwidth consumption of a read sector job. This helper function takes a
// length parameter and is used to get the expected bandwidth without having to
// instantiate a job.
func readSectorJobExpectedBandwidth(length uint64) (ul, dl uint64) {
	ul = 1 << 15                              // 32 KiB
	dl = uint64(float64(length)*1.01) + 1<<14 // (readSize * 1.01 + 16 KiB)
	return
}
