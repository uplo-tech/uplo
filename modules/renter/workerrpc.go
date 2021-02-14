package renter

import (
	"bytes"
	"io"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/ratelimit"
	"github.com/uplo-tech/uplomux"
	"github.com/uplo-tech/uplomux/mux"

	"github.com/uplo-tech/errors"
)

// defaultNewStreamTimeout is a default timeout for creating a new stream.
var defaultNewStreamTimeout = build.Select(build.Var{
	Standard: 5 * time.Minute,
	Testing:  10 * time.Second,
	Dev:      time.Minute,
}).(time.Duration)

// defaultRPCDeadline is a default timeout for executing an RPC.
var defaultRPCDeadline = build.Select(build.Var{
	Standard: 5 * time.Minute,
	Testing:  10 * time.Second,
	Dev:      time.Minute,
}).(time.Duration)

// programResponse is a helper struct that wraps the RPCExecuteProgramResponse
// alongside the data output
type programResponse struct {
	modules.RPCExecuteProgramResponse
	Output []byte
}

// managedExecuteProgram performs the ExecuteProgramRPC on the host
func (w *worker) managedExecuteProgram(p modules.Program, data []byte, fcid types.FileContractID, cost types.Currency) (responses []programResponse, limit mux.BandwidthLimit, err error) {
	// check host version
	if !w.staticSupportsRHP3() {
		build.Critical("Executing new RHP RPC on host with version", w.staticCache().staticHostVersion)
	}

	// track the withdrawal
	// TODO: this is very naive and does not consider refunds at all
	w.staticAccount.managedTrackWithdrawal(cost)
	defer func() {
		w.staticAccount.managedCommitWithdrawal(cost, err == nil)
	}()

	// create a new stream
	stream, err := w.staticNewStream()
	if err != nil {
		err = errors.AddContext(err, "Unable to create a new stream")
		return
	}
	defer func() {
		if err := stream.Close(); err != nil {
			w.renter.log.Println("ERROR: failed to close stream", err)
		}
	}()

	// set the limit return var.
	limit = stream.Limit()

	// prepare a buffer so we can optimize our writes
	buffer := bytes.NewBuffer(nil)

	// write the specifier
	err = modules.RPCWrite(buffer, modules.RPCExecuteProgram)
	if err != nil {
		return
	}

	// send price table uid
	pt := w.staticPriceTable().staticPriceTable
	err = modules.RPCWrite(buffer, pt.UID)
	if err != nil {
		return
	}

	// provide payment
	err = w.staticAccount.ProvidePayment(buffer, w.staticHostPubKey, modules.RPCUpdatePriceTable, cost, w.staticAccount.staticID, w.staticCache().staticBlockHeight)
	if err != nil {
		return
	}

	// prepare the request.
	epr := modules.RPCExecuteProgramRequest{
		FileContractID:    fcid,
		Program:           p,
		ProgramDataLength: uint64(len(data)),
	}

	// send the execute program request.
	err = modules.RPCWrite(buffer, epr)
	if err != nil {
		return
	}

	// send the programData.
	_, err = buffer.Write(data)
	if err != nil {
		return
	}

	// write contents of the buffer to the stream
	_, err = stream.Write(buffer.Bytes())
	if err != nil {
		return
	}

	// read the cancellation token.
	var ct modules.MDMCancellationToken
	err = modules.RPCRead(stream, &ct)
	if err != nil {
		return
	}

	// read the responses.
	responses = make([]programResponse, 0, len(epr.Program))
	for i := 0; i < len(epr.Program); i++ {
		var response programResponse
		err = modules.RPCRead(stream, &response)
		if err != nil {
			return
		}

		// Read the output data.
		outputLen := response.OutputLength
		response.Output = make([]byte, outputLen)
		_, err = io.ReadFull(stream, response.Output)
		if err != nil {
			return
		}

		// We received a valid response. Append it.
		responses = append(responses, response)

		// If the response contains an error we are done.
		if response.Error != nil {
			break
		}
	}
	return
}

// staticNewStream returns a new stream to the worker's host
func (w *worker) staticNewStream() (uplomux.Stream, error) {
	if !w.staticSupportsRHP3() {
		w.renter.log.Critical("calling staticNewStream on a host that doesn't support the new protocol")
		return nil, errors.New("host doesn't support this")
	}

	// If disrupt is called we sleep for the specified 'defaultNewStreamTimeout'
	// simulating how an unreachable host would behave in production.
	timeout := defaultNewStreamTimeout
	if w.renter.deps.Disrupt("InterruptNewStreamTimeout") {
		time.Sleep(timeout)
		return nil, errors.New("InterruptNewStreamTimeout")
	}

	// Create a stream with a reasonable dial up timeout.
	stream, err := w.renter.staticMux.NewStreamTimeout(modules.HostUploMuxSubscriberName, w.staticCache().staticHostMuxAddress, timeout, modules.UploPKToMuxPK(w.staticHostPubKey))
	if err != nil {
		return nil, err
	}
	// Set deadline on the stream.
	err = stream.SetDeadline(time.Now().Add(defaultRPCDeadline))
	if err != nil {
		return nil, err
	}

	// Wrap the stream in the renter's ratelimit
	//
	// NOTE: this only ratelimits the data going over the stream and not the raw
	// bytes going over the wire, so the ratelimit might be off by a few bytes.
	rlStream := ratelimit.NewRLStream(stream, w.renter.rl, w.renter.tg.StopChan())

	// Wrap the stream in global ratelimit.
	return ratelimit.NewRLStream(rlStream, modules.GlobalRateLimits, w.renter.tg.StopChan()), nil
}

// managedRenew renews the contract with the worker's host.
func (w *worker) managedRenew(fcid types.FileContractID, params modules.ContractParams, txnBuilder modules.TransactionBuilder) (modules.RenterContract, []types.Transaction, error) {
	// create a new stream
	stream, err := w.staticNewStream()
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "managedRenew: unable to create a new stream")
	}
	defer func() {
		if err := stream.Close(); err != nil {
			w.renter.log.Println("managedRenew: failed to close stream", err)
		}
	}()

	// write the specifier.
	err = modules.RPCWrite(stream, modules.RPCRenewContract)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "managedRenew: failed to write RPC specifier")
	}

	// send price table uid
	pt := w.staticPriceTable().staticPriceTable
	err = modules.RPCWrite(stream, pt.UID)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "managedRenew: failed to write price table uid")
	}

	// have the contractset handle the renewal.
	r := w.renter
	newContract, txnSet, err := w.renter.hostContractor.RenewContract(stream, fcid, params, txnBuilder, r.tpool, r.hostDB, &pt)
	if err != nil {
		return modules.RenterContract{}, nil, errors.AddContext(err, "managedRenew: call to RenewContract failed")
	}

	return newContract, txnSet, nil
}
