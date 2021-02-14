package renter

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"

	"github.com/uplo-tech/errors"
)

const (
	// ethernetMTU is the minimum transferable size for ethernet networks. It's
	// used as the estimated upper limit for the frame size of the UploMux.
	ethernetMTU = 1500

	// jobReadRegistryPerformanceDecay defines how much the average
	// performance is decayed each time a new datapoint is added. The jobs use
	// an exponential weighted average.
	jobReadRegistryPerformanceDecay = 0.9
)

type (
	// jobReadRegistry contains information about a ReadRegistry query.
	jobReadRegistry struct {
		staticUploPublicKey types.UploPublicKey
		staticTweak        crypto.Hash

		staticResponseChan chan *jobReadRegistryResponse // Channel to send a response down

		*jobGeneric
	}

	// jobReadRegistryQueue is a list of ReadRegistry jobs that have been
	// assigned to the worker.
	jobReadRegistryQueue struct {
		// These variables contain an exponential weighted average of the
		// worker's recent performance for jobReadRegistryQueue.
		weightedJobTime float64

		*jobGenericQueue
	}

	// jobReadRegistryResponse contains the result of a ReadRegistry query.
	jobReadRegistryResponse struct {
		staticSignedRegistryValue *modules.SignedRegistryValue
		staticErr                 error
	}
)

// parseSignedRegistryValueResponse is a helper function to parse a response
// containing a signed registry value.
func parseSignedRegistryValueResponse(resp []byte, tweak crypto.Hash) (modules.SignedRegistryValue, error) {
	if len(resp) < crypto.SignatureSize+8 {
		return modules.SignedRegistryValue{}, errors.New("failed to parse response due to invalid size")
	}
	var sig crypto.Signature
	copy(sig[:], resp[:crypto.SignatureSize])
	rev := binary.LittleEndian.Uint64(resp[crypto.SignatureSize:])
	data := resp[crypto.SignatureSize+8:]
	return modules.NewSignedRegistryValue(tweak, data, rev, sig), nil
}

// lookupsRegistry looks up a registry on the host and verifies its signature.
func lookupRegistry(w *worker, spk types.UploPublicKey, tweak crypto.Hash) (*modules.SignedRegistryValue, error) {
	// Create the program.
	pt := w.staticPriceTable().staticPriceTable
	pb := modules.NewProgramBuilder(&pt, 0) // 0 duration since ReadRegistry doesn't depend on it.
	var refund types.Currency
	var err error
	if build.VersionCmp(w.staticCache().staticHostVersion, "1.0.1") < 0 {
		refund, err = pb.V154AddReadRegistryInstruction(spk, tweak)
	} else {
		refund, err = pb.AddReadRegistryInstruction(spk, tweak)
	}
	if err != nil {
		return nil, errors.AddContext(err, "Unable to add read registry instruction")
	}
	program, programData := pb.Program()
	cost, _, _ := pb.Cost(true)

	// take into account bandwidth costs
	ulBandwidth, dlBandwidth := readRegistryJobExpectedBandwidth()
	bandwidthCost := modules.MDMBandwidthCost(pt, ulBandwidth, dlBandwidth)
	cost = cost.Add(bandwidthCost)

	// Execute the program and parse the responses.
	responses, _, err := w.managedExecuteProgram(program, programData, types.FileContractID{}, cost)
	if err != nil {
		return nil, errors.AddContext(err, "Unable to execute program")
	}
	for _, resp := range responses {
		if resp.Error != nil {
			return nil, errors.AddContext(resp.Error, "Output error")
		}
		break
	}
	if len(responses) != len(program) {
		return nil, errors.New("received invalid number of responses but no error")
	}

	// Check if entry was found.
	resp := responses[0]
	if resp.OutputLength == 0 {
		// If the entry wasn't found, we are issued a refund.
		w.staticAccount.managedTrackDeposit(refund)
		w.staticAccount.managedCommitDeposit(refund, true)
		return nil, nil
	}

	// Parse response.
	rv, err := parseSignedRegistryValueResponse(resp.Output, tweak)
	if err != nil {
		return nil, errors.AddContext(err, "failed to parse signed revision response")
	}

	// Verify signature.
	if rv.Verify(spk.ToPublicKey()) != nil {
		return nil, errors.New("failed to verify returned registry value's signature")
	}
	return &rv, nil
}

// newJobReadRegistry is a helper method to create a new ReadRegistry job.
func (w *worker) newJobReadRegistry(ctx context.Context, responseChan chan *jobReadRegistryResponse, spk types.UploPublicKey, tweak crypto.Hash) *jobReadRegistry {
	return &jobReadRegistry{
		staticUploPublicKey: spk,
		staticTweak:        tweak,
		staticResponseChan: responseChan,
		jobGeneric:         newJobGeneric(ctx, w.staticJobReadRegistryQueue, nil),
	}
}

// callDiscard will discard a job, sending the provided error.
func (j *jobReadRegistry) callDiscard(err error) {
	w := j.staticQueue.staticWorker()
	errLaunch := w.renter.tg.Launch(func() {
		response := &jobReadRegistryResponse{
			staticErr: errors.Extend(err, ErrJobDiscarded),
		}
		select {
		case j.staticResponseChan <- response:
		case <-j.staticCtx.Done():
		case <-w.renter.tg.StopChan():
		}
	})
	if errLaunch != nil {
		w.renter.log.Debugln("callDiscard: launch failed", err)
	}
}

// callExecute will run the ReadRegistry job.
func (j *jobReadRegistry) callExecute() {
	start := time.Now()
	w := j.staticQueue.staticWorker()

	// Prepare a method to send a response asynchronously.
	sendResponse := func(srv *modules.SignedRegistryValue, err error) {
		errLaunch := w.renter.tg.Launch(func() {
			response := &jobReadRegistryResponse{
				staticSignedRegistryValue: srv,
				staticErr:                 err,
			}
			select {
			case j.staticResponseChan <- response:
			case <-j.staticCtx.Done():
			case <-w.renter.tg.StopChan():
			}
		})
		if errLaunch != nil {
			w.renter.log.Debugln("callExececute: launch failed", err)
		}
	}

	// Read the value.
	srv, err := lookupRegistry(w, j.staticUploPublicKey, j.staticTweak)
	if err != nil {
		sendResponse(nil, err)
		j.staticQueue.callReportFailure(err)
		return
	}

	// Check if we have a cached version of the looked up entry. If the new entry
	// has a higher revision number we update it. If it has a lower one we know that
	// the host should be punished for losing it or trying to cheat us.
	if srv != nil {
		cachedRevision, cached := w.staticRegistryCache.Get(j.staticUploPublicKey, j.staticTweak)
		if cached && cachedRevision > srv.Revision {
			sendResponse(nil, errHostLowerRevisionThanCache)
			j.staticQueue.callReportFailure(errHostLowerRevisionThanCache)
			w.staticRegistryCache.Set(j.staticUploPublicKey, *srv, true) // adjust the cache
			return
		} else if !cached || srv.Revision > cachedRevision {
			w.staticRegistryCache.Set(j.staticUploPublicKey, *srv, false) // adjust the cache
		}
	}

	// Success.
	jobTime := time.Since(start)

	// Send the response and report success.
	sendResponse(srv, nil)
	j.staticQueue.callReportSuccess()

	// Update the performance stats on the queue.
	jq := j.staticQueue.(*jobReadRegistryQueue)
	jq.mu.Lock()
	jq.weightedJobTime = expMovingAvg(jq.weightedJobTime, float64(jobTime), jobReadRegistryPerformanceDecay)
	jq.mu.Unlock()
}

// callExpectedBandwidth returns the bandwidth that is expected to be consumed
// by the job.
func (j *jobReadRegistry) callExpectedBandwidth() (ul, dl uint64) {
	return readRegistryJobExpectedBandwidth()
}

// initJobReadRegistryQueue will init the queue for the ReadRegistry jobs.
func (w *worker) initJobReadRegistryQueue() {
	// Sanity check that there is no existing job queue.
	if w.staticJobReadRegistryQueue != nil {
		w.renter.log.Critical("incorret call on initJobReadRegistryQueue")
		return
	}

	w.staticJobReadRegistryQueue = &jobReadRegistryQueue{
		jobGenericQueue: newJobGenericQueue(w),
	}
}

// ReadRegistry is a helper method to run a ReadRegistry job on a worker.
func (w *worker) ReadRegistry(ctx context.Context, spk types.UploPublicKey, tweak crypto.Hash) (*modules.SignedRegistryValue, error) {
	readRegistryRespChan := make(chan *jobReadRegistryResponse)
	jur := w.newJobReadRegistry(ctx, readRegistryRespChan, spk, tweak)

	// Add the job to the queue.
	if !w.staticJobReadRegistryQueue.callAdd(jur) {
		return nil, errors.New("worker unavailable")
	}

	// Wait for the response.
	var resp *jobReadRegistryResponse
	select {
	case <-ctx.Done():
		return nil, errors.New("ReadRegistry interrupted")
	case resp = <-readRegistryRespChan:
	}
	return resp.staticSignedRegistryValue, resp.staticErr
}

// readRegistryJobExpectedBandwidth is a helper function that returns the
// expected bandwidth consumption of a ReadRegistry job. This helper function
// enables getting at the expected bandwidth without having to instantiate a
// job.
func readRegistryJobExpectedBandwidth() (ul, dl uint64) {
	return ethernetMTU, ethernetMTU // a single frame each for upload and download
}
