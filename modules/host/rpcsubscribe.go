package host

import (
	"bytes"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"
	"github.com/uplo-tech/uplomux"
)

type (
	// registrySubscriptions is a helper type that holds all current
	// subscriptions.
	registrySubscriptions struct {
		mu sync.Mutex

		// subscriptions is a mapping of subscriptions to subscription infos.
		// It's a map of maps since the same entry can be subscribed to by
		// multiple peers and we want to be able to look up subscriptions in
		// constant time.
		subscriptions map[subscriptionID]map[subscriptionInfoID]*subscriptionInfo
	}
	// subscriptionInfo holds the information required to respond to a
	// subscriber and to correctly charge it.
	subscriptionInfo struct {
		closed           bool
		notificationCost types.Currency
		latestRevNum     map[subscriptionID]uint64
		subscriptions    map[subscriptionID]struct{}
		mu               sync.Mutex

		staticBudget     *modules.RPCBudget
		staticID         subscriptionInfoID
		staticStream     uplomux.Stream
		staticSubscriber string
	}

	// subscriptionID is a hash derived from the public key and tweak that a
	// renter would like to subscribe to.
	subscriptionID     crypto.Hash
	subscriptionInfoID types.Specifier
)

var (
	// ErrSubscriptionRequestLimitReached is returned if too many subscribe or
	// unsubscribe requests are sent at once.
	ErrSubscriptionRequestLimitReached = errors.New("number of requests exceeds limit")
)

// deriveSubscriptionID is a helper to derive a subscription id.
func deriveSubscriptionID(pubKey types.UploPublicKey, tweak crypto.Hash) subscriptionID {
	return subscriptionID(crypto.HashAll(pubKey, tweak))
}

// newRegistrySubscriptions creates a new registrySubscriptions instance.
func newRegistrySubscriptions() *registrySubscriptions {
	return &registrySubscriptions{
		subscriptions: make(map[subscriptionID]map[subscriptionInfoID]*subscriptionInfo),
	}
}

// newSubscriptionInfo creates a new subscriptionInfo object.
func newSubscriptionInfo(stream uplomux.Stream, budget *modules.RPCBudget, notificationsCost types.Currency, subscriber types.Specifier) *subscriptionInfo {
	info := &subscriptionInfo{
		notificationCost: notificationsCost,
		latestRevNum:     make(map[subscriptionID]uint64),
		subscriptions:    make(map[subscriptionID]struct{}),
		staticBudget:     budget,
		staticStream:     stream,
		staticSubscriber: hex.EncodeToString(subscriber[:]),
	}
	fastrand.Read(info.staticID[:])
	return info
}

// AddSubscriptions adds one or multiple subscriptions.
func (rs *registrySubscriptions) AddSubscriptions(info *subscriptionInfo, entryIDs ...subscriptionID) {
	// Add to the info first.
	info.mu.Lock()
	for _, id := range entryIDs {
		info.subscriptions[id] = struct{}{}
	}
	info.mu.Unlock()

	// Then add to the global subscription map.
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, entryID := range entryIDs {
		if _, exists := rs.subscriptions[entryID]; !exists {
			rs.subscriptions[entryID] = make(map[subscriptionInfoID]*subscriptionInfo)
		}
		rs.subscriptions[entryID][info.staticID] = info
	}
}

// RemoveSubscriptions removes one or multiple subscriptions.
func (rs *registrySubscriptions) RemoveSubscriptions(info *subscriptionInfo, entryIDs []subscriptionID) {
	// Delete from the info first.
	info.mu.Lock()
	for _, entryID := range entryIDs {
		delete(info.subscriptions, entryID)
	}
	info.mu.Unlock()

	// Remove them from the global subscription map.
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, entryID := range entryIDs {
		infos, found := rs.subscriptions[entryID]
		if !found {
			continue
		}
		delete(infos, info.staticID)

		if len(infos) == 0 {
			delete(rs.subscriptions, entryID)
		}
	}
}

// managedHandleSubscribeRequest handles a new subscription.
func (h *Host) managedHandleSubscribeRequest(info *subscriptionInfo, pt *modules.RPCPriceTable) error {
	stream := info.staticStream

	// Read the requests.
	var rsrs []modules.RPCRegistrySubscriptionRequest
	err := modules.RPCRead(stream, &rsrs)
	if err != nil {
		return errors.AddContext(err, "failed to read subscription request")
	}

	// Send initial values.
	ids := make([]subscriptionID, 0, len(rsrs))
	rvs := make([]modules.SignedRegistryValue, 0, len(ids))
	for _, rsr := range rsrs {
		ids = append(ids, deriveSubscriptionID(rsr.PubKey, rsr.Tweak))
		rv, found := h.staticRegistry.Get(rsr.PubKey, rsr.Tweak)
		if !found {
			continue
		}
		rvs = append(rvs, rv)
	}

	// Compute the subscription cost.
	cost := modules.MDMSubscribeCost(pt, uint64(len(rvs)), uint64(len(ids)))

	// Withdraw from the budget.
	if !info.staticBudget.Withdraw(cost) {
		return errors.AddContext(modules.ErrInsufficientPaymentForRPC, "managedHandleSubscribeRequest")
	}

	// Write initial values to the stream.
	err = modules.RPCWrite(stream, rvs)
	if err != nil {
		return errors.AddContext(err, "failed to write initial values to stream")
	}

	// Add the subscriptions.
	h.staticRegistrySubscriptions.AddSubscriptions(info, ids...)
	return nil
}

// managedHandleStopSubscription gracefully disables notifications and waits for
// ongoing notifications to be sent.
func (h *Host) managedHandleStopSubscription(info *subscriptionInfo) error {
	// Flush notifications and prevent new ones.
	info.mu.Lock()
	info.closed = true
	info.mu.Unlock()
	return nil
}

// managedHandleUnsubscribeRequest handles a request to unsubscribe.
func (h *Host) managedHandleUnsubscribeRequest(info *subscriptionInfo, pt *modules.RPCPriceTable) error {
	stream := info.staticStream

	// Read the requests.
	var rsrs []modules.RPCRegistrySubscriptionRequest
	err := modules.RPCRead(stream, &rsrs)
	if err != nil {
		return errors.AddContext(err, "failed to read subscription requests")
	}
	ids := make([]subscriptionID, 0, len(rsrs))
	for _, rsr := range rsrs {
		ids = append(ids, deriveSubscriptionID(rsr.PubKey, rsr.Tweak))
	}

	// Remove the subscription.
	h.staticRegistrySubscriptions.RemoveSubscriptions(info, ids)

	// Respond with "OK".
	err = modules.RPCWrite(stream, modules.RPCRegistrySubscriptionNotificationType{
		Type: modules.SubscriptionResponseUnsubscribeSuccess,
	})
	return errors.AddContext(err, "failed to signal successfully unsubscribing from entries")
}

// managedHandleExtendSubscriptionRequest handles a request to extend the subscription.
func (h *Host) managedHandleExtendSubscriptionRequest(stream uplomux.Stream, oldDeadline time.Time, info *subscriptionInfo, limit *modules.BudgetLimit) (*modules.RPCPriceTable, time.Time, error) {
	// Get new deadline.
	newDeadline := oldDeadline.Add(modules.SubscriptionPeriod)

	// Read the price table
	pt, err := h.staticReadPriceTableID(stream)
	if err != nil {
		return nil, time.Time{}, errors.AddContext(err, "failed to read price table")
	}

	// Make sure the pricetable is valid until the new deadline.
	if !h.managedPriceTableValidFor(pt, time.Until(newDeadline)) {
		return nil, time.Time{}, errors.New("read price table is not valid for long enough")
	}

	// Check payment against the new prices.
	info.mu.Lock()
	defer info.mu.Unlock()
	cost := modules.MDMSubscriptionMemoryCost(pt, uint64(len(info.subscriptions)))
	if !info.staticBudget.Withdraw(cost) {
		return nil, time.Time{}, errors.AddContext(modules.ErrInsufficientPaymentForRPC, "managedHandleExtendSubscriptionRequest")
	}

	// Update the notification cost. Hold a lock while doing so to make sure no
	// notifications are sent in the meantime.
	info.notificationCost = pt.SubscriptionNotificationCost

	// Update the limit.
	limit.UpdateCosts(pt.UploadBandwidthCost, pt.DownloadBandwidthCost)

	// Update deadline.
	err = stream.SetReadDeadline(newDeadline)
	if err != nil {
		return nil, time.Time{}, errors.AddContext(err, "failed to extend stream deadline")
	}

	// Get a response stream.
	responseStream, err := subscriptionResponseStream(info, h.staticMux)
	if err != nil {
		return nil, time.Time{}, errors.AddContext(err, "failed to open stream for notifying subscriber")
	}
	defer responseStream.Close()

	// Respond with "OK".
	err = modules.RPCWrite(responseStream, modules.RPCRegistrySubscriptionNotificationType{
		Type: modules.SubscriptionResponseSubscriptionSuccess,
	})
	if err != nil {
		return nil, time.Time{}, errors.AddContext(err, "failed to signal subscription extension success")
	}
	return pt, newDeadline, nil
}

// managedHandlePrepayBandwidth is used by the renter to increase the budget for
// this session with the host. Due to the complicated concurrency of how we
// track bandwidth and updating the price table, we lock the subscriptionInfo
// during the whole operation and notify the renter when setting the new limit
// is done.
func (h *Host) managedHandlePrepayBandwidth(stream uplomux.Stream, info *subscriptionInfo) error {
	// Process payment.
	pd, err := h.ProcessPayment(stream)
	if err != nil {
		return errors.AddContext(err, "managedHandlePrepaybandwidth: failed to process payment")
	}

	// Add to budget.
	info.staticBudget.Deposit(pd.Amount())
	return nil
}

// managedPriceTableValidFor returns true if a price table is still valid for
// the provided duration.
func (h *Host) managedPriceTableValidFor(pt *modules.RPCPriceTable, duration time.Duration) bool {
	hpt, found := h.staticPriceTables.managedGet(pt.UID)
	if !found {
		return false
	}
	minExpiry := time.Now().Add(duration)
	return minExpiry.Before(hpt.Expiry())
}

// threadedNotifySubscribers handles notifying all subscribers for a certain
// key/tweak combination.
func (h *Host) threadedNotifySubscribers(pubKey types.UploPublicKey, rv modules.SignedRegistryValue) {
	err := h.tg.Add()
	if err != nil {
		return
	}
	defer h.tg.Done()

	// Look up subscribers.
	h.staticRegistrySubscriptions.mu.Lock()
	defer h.staticRegistrySubscriptions.mu.Unlock()

	id := deriveSubscriptionID(pubKey, rv.Tweak)
	infos, found := h.staticRegistrySubscriptions.subscriptions[id]
	if !found {
		return
	}
	for _, info := range infos {
		go func(info *subscriptionInfo) {
			// Lock the info while notifying the subscriber. We use a readlock
			// to allow for multiple notifications in parallel.
			info.mu.Lock()
			defer info.mu.Unlock()
			if info.closed {
				return
			}

			// Check if we are still subscribed.
			if _, subscribed := info.subscriptions[id]; !subscribed {
				return
			}

			// Check if we have already updated the subscriber with a higher
			// revision number for that entry than the minExpectedRevNum. This
			// might happen due to a race and should be avoided. Otherwise the
			// subscriber might think that we are trying to cheat them.
			latestRevNum, exists := info.latestRevNum[id]
			if exists && rv.Revision <= latestRevNum {
				return
			}
			info.latestRevNum[id] = rv.Revision

			// Withdraw the base notification cost.
			ok := info.staticBudget.Withdraw(info.notificationCost)
			if !ok {
				return
			}

			// Get a response stream.
			stream, err := subscriptionResponseStream(info, h.staticMux)
			if err != nil {
				h.log.Debug("failed to open stream for notifying subscriber", err)
				return
			}
			defer stream.Close()

			// Notify the caller.
			err = sendNotification(stream, pubKey, rv)
			if err != nil {
				h.log.Debug("failed to write notification to buffer", err)
				return
			}
		}(info)
	}
}

// subscriptionResponseStream opens a response stream using the given uplomux to
// a subsriber.
func subscriptionResponseStream(info *subscriptionInfo, sm *uplomux.UploMux) (uplomux.Stream, error) {
	stream, err := sm.NewResponseStream(info.staticSubscriber, uplomux.DefaultNewStreamTimeout, info.staticStream)
	if err != nil {
		return nil, errors.AddContext(err, "failed to open stream for notifying subscriber")
	}
	return stream, stream.SetLimit(info.staticStream.Limit())
}

// managedRPCRegistrySubscribe handles the RegistrySubscribe rpc.
func (h *Host) managedRPCRegistrySubscribe(stream uplomux.Stream) (_ afterCloseFn, err error) {
	// Read the price table
	pt, err := h.staticReadPriceTableID(stream)
	if err != nil {
		return nil, errors.AddContext(err, "failed to read price table")
	}

	// Make sure the price table is valid.
	if !h.managedPriceTableValidFor(pt, modules.SubscriptionPeriod) {
		return nil, errors.New("can't begin subscription due to price table expiring soon")
	}

	// Process bandwidth payment.
	pd, err := h.ProcessPayment(stream)
	if err != nil {
		return nil, errors.AddContext(err, "failed to process payment")
	}

	// Fetch the subscriber. This will later allow us to open a stream to the
	// renter.
	var subscriber types.Specifier
	err = modules.RPCRead(stream, &subscriber)
	if err != nil {
		return nil, errors.AddContext(err, "failed to read subscriber")
	}

	// Add limit to the stream. The readCost is the UploadBandwidthCost since
	// reading from the stream means uploading from the host's perspective. That
	// makes the writeCost the DownloadBandwidthCost.
	budget := modules.NewBudget(pd.Amount())
	bandwidthLimit := modules.NewBudgetLimit(budget, pt.UploadBandwidthCost, pt.DownloadBandwidthCost)
	// Prepare a refund method which is called at the end of the rpc.
	refund := func() {
		// Refund the unused budget
		if !budget.Remaining().IsZero() {
			err = errors.Compose(err, h.staticAccountManager.callRefund(pd.AccountID(), budget.Remaining()))
		}
	}
	err = stream.SetLimit(bandwidthLimit)
	if err != nil {
		return refund, errors.AddContext(err, "failed to set budget limit on stream")
	}

	// Set the stream deadline.
	deadline := time.Now().Add(modules.SubscriptionPeriod)
	err = stream.SetReadDeadline(deadline)
	if err != nil {
		return refund, errors.AddContext(err, "failed to set intitial subscription deadline")
	}

	// Keep count of the unique subscriptions to be able to charge accordingly.
	info := newSubscriptionInfo(stream, budget, pt.SubscriptionNotificationCost, subscriber)

	// Clean up the subscriptions at the end.
	defer func() {
		info.mu.Lock()
		var entryIDs []subscriptionID
		for entryID := range info.subscriptions {
			entryIDs = append(entryIDs, entryID)
		}
		info.mu.Unlock()
		h.staticRegistrySubscriptions.RemoveSubscriptions(info, entryIDs)
	}()

	// The subscription RPC is a request/response loop that continues for as
	// long as the renter keeps paying for it.
	for {
		// Read subscription request.
		var requestType uint8
		err = modules.RPCRead(stream, &requestType)
		if err != nil {
			return refund, errors.AddContext(err, "failed to read request type")
		}

		// Handle requests.
		switch requestType {
		case modules.SubscriptionRequestSubscribe:
			err = h.managedHandleSubscribeRequest(info, pt)
		case modules.SubscriptionRequestUnsubscribe:
			err = h.managedHandleUnsubscribeRequest(info, pt)
		case modules.SubscriptionRequestExtend:
			pt, deadline, err = h.managedHandleExtendSubscriptionRequest(stream, deadline, info, bandwidthLimit)
		case modules.SubscriptionRequestPrepay:
			err = h.managedHandlePrepayBandwidth(stream, info)
		case modules.SubscriptionRequestStop:
			err = h.managedHandleStopSubscription(info)
			return refund, err
		default:
			return refund, errors.New("unknown request type")
		}
		// Check the errors.
		if err != nil {
			return refund, errors.AddContext(err, "failed to handle request")
		}
	}
}

// sendNotification marshals an entry notification and writes it to the provided
// writer.
func sendNotification(stream io.Writer, spk types.UploPublicKey, rv modules.SignedRegistryValue) error {
	buf := new(bytes.Buffer)
	err := modules.RPCWrite(buf, modules.RPCRegistrySubscriptionNotificationType{
		Type: modules.SubscriptionResponseRegistryValue,
	})
	if err != nil {
		return errors.AddContext(err, "failed to write notification header to buffer")
	}
	err = modules.RPCWrite(buf, modules.RPCRegistrySubscriptionNotificationEntryUpdate{
		Entry:  rv,
		PubKey: spk,
	})
	if err != nil {
		return errors.AddContext(err, "failed to write entry to buffer")
	}
	_, err = buf.WriteTo(stream)
	if err != nil {
		return errors.AddContext(err, "failed to write notification to stream")
	}
	return nil
}
