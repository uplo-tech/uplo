package consensus

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/persist"
	"github.com/uplo-tech/uplo/types"
)

var (
	// ErrBadMinerPayouts is returned when the miner payout does not equal the
	// block subsidy
	ErrBadMinerPayouts = errors.New("miner payout sum does not equal block subsidy")
	// ErrEarlyTimestamp is returned when the block's timestamp is too early
	ErrEarlyTimestamp = errors.New("block timestamp is too early")
	// ErrExtremeFutureTimestamp is returned when the block's timestamp is too
	// far in the future
	ErrExtremeFutureTimestamp = errors.New("block timestamp too far in future, discarded")
	// ErrFutureTimestamp is returned when the block's timestamp is too far in
	// the future to be used now but it's saved for future use
	ErrFutureTimestamp = errors.New("block timestamp too far in future, but saved for later use")
	// ErrLargeBlock is returned when the block is too large to be accepted
	ErrLargeBlock = errors.New("block is too large to be accepted")
)

// blockValidator validates a Block against a set of block validity rules.
type blockValidator interface {
	// ValidateBlock validates a block against a minimum timestamp, a block
	// target, and a block height.
	ValidateBlock(types.Block, types.BlockID, types.Timestamp, types.Target, types.BlockHeight, *persist.Logger) error
}

// stdBlockValidator is the standard implementation of blockValidator.
type stdBlockValidator struct {
	// clock is a Clock interface that indicates the current system time.
	clock types.Clock

	// marshaler encodes and decodes between objects and byte slices.
	marshaler marshaler
}

// NewBlockValidator creates a new stdBlockValidator with default settings.
func NewBlockValidator() stdBlockValidator {
	return stdBlockValidator{
		clock:     types.StdClock{},
		marshaler: stdMarshaler{},
	}
}

// checkMinerPayouts compares a block's miner payouts to the block's subsidy and
// returns true if they are equal.
func checkMinerPayouts(b types.Block, height types.BlockHeight) bool {
	// Add up the payouts and check that all values are legal.
	var payoutSum types.Currency
	for _, payout := range b.MinerPayouts {
		if payout.Value.IsZero() {
			return false
		}
		payoutSum = payoutSum.Add(payout.Value)
	}
	return b.CalculateSubsidy(height).Equals(payoutSum)
}

// checkTarget returns true if the block's ID meets the given target.
func checkTarget(b types.Block, id types.BlockID, target types.Target) bool {
	return bytes.Compare(target[:], id[:]) >= 0
}

// ValidateBlock validates a block against a minimum timestamp, a block target,
// and a block height. Returns nil if the block is valid and an appropriate
// error otherwise.
func (bv stdBlockValidator) ValidateBlock(b types.Block, id types.BlockID, minTimestamp types.Timestamp, target types.Target, height types.BlockHeight, log *persist.Logger) error {
	// Check that the timestamp is not too far in the past to be acceptable.
	if minTimestamp > b.Timestamp {
		return ErrEarlyTimestamp
	}

	// Check that the nonce is a legal nonce.
	if height >= types.ASICHardforkHeight && binary.LittleEndian.Uint64(b.Nonce[:])%types.ASICHardforkFactor != 0 {
		return errors.New("block does not meet nonce requirements")
	}
	// Check that the target of the new block is sufficient.
	if !checkTarget(b, id, target) {
		return modules.ErrBlockUnsolved
	}

	// Check that the block is below the size limit.
	blockSize := len(bv.marshaler.Marshal(b))
	if uint64(blockSize) > types.BlockSizeLimit {
		return ErrLargeBlock
	}

	// Check if the block is in the extreme future. We make a distinction between
	// future and extreme future because there is an assumption that by the time
	// the extreme future arrives, this block will no longer be a part of the
	// longest fork because it will have been ignored by all of the miners.
	if b.Timestamp > bv.clock.Now()+types.ExtremeFutureThreshold {
		return ErrExtremeFutureTimestamp
	}

	// Verify that the miner payouts are valid.
	if !checkMinerPayouts(b, height) {
		return ErrBadMinerPayouts
	}

	// Check if the block is in the near future, but too far to be acceptable.
	// This is the last check because it's an expensive check, and not worth
	// performing if the payouts are incorrect.
	if b.Timestamp > bv.clock.Now()+types.FutureThreshold {
		return ErrFutureTimestamp
	}

	if log != nil {
		log.Debugf("validated block at height %v, block size: %vB", height, blockSize)
	}
	return nil
}
