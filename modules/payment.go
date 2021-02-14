package modules

import (
	"io"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/uplomux"
)

const (
	// WithdrawalNonceSize is the size of the nonce in the WithdralMessage
	WithdrawalNonceSize = 8
)

var (
	// ErrUnknownPaymentMethod occurs when the payment method specified in the
	// PaymentRequest object is unknown. The possible options are outlined below
	// under "Payment identifiers".
	ErrUnknownPaymentMethod = errors.New("unknown payment method")

	// ErrInvalidPaymentMethod occurs when the payment method is not accepted
	// for a specific RPC.
	ErrInvalidPaymentMethod = errors.New("invalid payment method")

	// ErrInsufficientPaymentForRPC is returned when the provided payment was
	// lower than the cost of the RPC.
	ErrInsufficientPaymentForRPC = errors.New("Insufficient payment, the provided payment did not cover the cost of the RPC.")

	// ErrExpiredRPCPriceTable is returned when the renter performs an RPC call
	// and the current block height exceeds the expiry block height of the RPC
	// price table.
	ErrExpiredRPCPriceTable = errors.New("Expired RPC price table, ensure you have the latest prices by calling the updatePriceTable RPC.")

	// ErrWithdrawalsInactive occurs when the host is not synced yet. If that is
	// the case the account manager does not allow trading money from the
	// ephemeral accounts.
	ErrWithdrawalsInactive = errors.New("ephemeral account withdrawals are inactive because the host is not synced")

	// ErrWithdrawalExpired occurs when the withdrawal message's expiry block
	// height is in the past.
	ErrWithdrawalExpired = errors.New("ephemeral account withdrawal message expired")

	// ErrWithdrawalExtremeFuture occurs when the withdrawal message's expiry
	// block height is too far into the future.
	ErrWithdrawalExtremeFuture = errors.New("ephemeral account withdrawal message expires too far into the future")

	// ErrWithdrawalInvalidSignature occurs when the signature provided with the
	// withdrawal message was invalid.
	ErrWithdrawalInvalidSignature = errors.New("ephemeral account withdrawal message signature is invalid")
)

// PaymentProcessor is the interface implemented to handle RPC payments.
type PaymentProcessor interface {
	// ProcessPayment takes a stream and handles the payment request objects
	// sent by the caller. Returns an object that implements the PaymentDetails
	// interface, or an error in case of failure.
	ProcessPayment(stream uplomux.Stream) (PaymentDetails, error)
}

// PaymentProvider is the interface implemented to provide payment for an RPC.
type PaymentProvider interface {
	// ProvidePayment takes a stream and various payment details and handles the
	// payment by sending and processing payment request and response objects.
	// Returns an error in case of failure.
	ProvidePayment(stream io.ReadWriter, host types.UploPublicKey, rpc types.Specifier, amount types.Currency, refundAccount AccountID, blockHeight types.BlockHeight) error
}

// PaymentDetails is an interface that defines method that give more information
// about the details of a processed payment.
type PaymentDetails interface {
	AccountID() AccountID
	Amount() types.Currency
}

// Payment identifiers
var (
	PayByContract         = types.NewSpecifier("PayByContract")
	PayByEphemeralAccount = types.NewSpecifier("PayByEphemAcc")
)

// ZeroAccountID is the only account id that is allowed to be invalid.
var ZeroAccountID = AccountID{""}

type (
	// AccountID is the unique identifier of an ephemeral account on the host.
	// It should always be a valid representation of types.UploPublicKey or an
	// empty string.
	AccountID struct {
		spk string
	}

	// PaymentRequest identifies the payment method. This can be either
	// PayByContract or PayByEphemeralAccount
	PaymentRequest struct {
		Type types.Specifier
	}

	// PayByEphemeralAccountRequest holds all payment details to pay from an
	// ephemeral account.
	PayByEphemeralAccountRequest struct {
		Message   WithdrawalMessage
		Signature crypto.Signature
		Priority  int64
	}

	// PayByContractRequest holds all payment details to pay from a file
	// contract.
	PayByContractRequest struct {
		ContractID           types.FileContractID
		NewRevisionNumber    uint64
		NewValidProofValues  []types.Currency
		NewMissedProofValues []types.Currency
		RefundAccount        AccountID
		Signature            []byte
	}

	// PayByContractResponse is the object sent in response to the
	// PayByContractRequest
	PayByContractResponse struct {
		Signature crypto.Signature
	}

	// WithdrawalMessage contains all details to spend from an ephemeral account
	WithdrawalMessage struct {
		Account AccountID
		Expiry  types.BlockHeight
		Amount  types.Currency
		Nonce   [WithdrawalNonceSize]byte
	}

	// Receipt is returned by the host after a successful deposit into an
	// ephemeral account and can be used as proof of payment.
	Receipt struct {
		Host      types.UploPublicKey
		Account   AccountID
		Amount    types.Currency
		Timestamp int64
	}
)

// NewAccountID is a helper function that creates a new account ID from a
// randomly generate key pair
func NewAccountID() (id AccountID, sk crypto.SecretKey) {
	var pk crypto.PublicKey
	sk, pk = crypto.GenerateKeyPair()
	id.FromSPK(types.UploPublicKey{
		Algorithm: types.SignatureEd25519,
		Key:       pk[:],
	})
	return
}

// FromSPK creates an AccountID from a UploPublicKey. This assumes that the
// provided key is valid and won't perform additional checks.
func (aid *AccountID) FromSPK(spk types.UploPublicKey) {
	if spk.Equals(types.UploPublicKey{}) {
		*aid = ZeroAccountID
		return
	}
	*aid = AccountID{spk.String()}
}

// IsZeroAccount returns whether or not the account id matches the empty string.
func (aid AccountID) IsZeroAccount() bool {
	return aid == ZeroAccountID
}

// LoadString loads an account id from a string.
func (aid *AccountID) LoadString(s string) error {
	var spk types.UploPublicKey
	err := spk.LoadString(s)
	if err != nil {
		return errors.AddContext(err, "failed to load account id from string")
	}
	aid.FromSPK(spk)
	return nil
}

// MarshalUplo implements the UploMarshaler interface.
func (aid AccountID) MarshalUplo(w io.Writer) error {
	if aid.IsZeroAccount() {
		return types.UploPublicKey{}.MarshalUplo(w)
	}
	return aid.SPK().MarshalUplo(w)
}

// UnmarshalUplo implements the UploMarshaler interface.
func (aid *AccountID) UnmarshalUplo(r io.Reader) error {
	var spk types.UploPublicKey
	err := spk.UnmarshalUplo(r)
	if err != nil {
		return err
	}
	aid.FromSPK(spk)
	return err
}

// PK returns the id as a crypto.PublicKey.
func (aid AccountID) PK() (pk crypto.PublicKey) {
	spk := aid.SPK()
	if len(spk.Key) != len(pk) {
		panic("key len mismatch between crypto.Publickey and types.UploPublicKey")
	}
	copy(pk[:], spk.Key)
	return
}

// SPK returns the account id as a types.UploPublicKey.
func (aid AccountID) SPK() (spk types.UploPublicKey) {
	if aid.IsZeroAccount() {
		build.Critical("should never use the zero account")
	}
	err := spk.LoadString(aid.spk)
	if err != nil {
		build.Critical("account id should never fail to be loaded as a UploPublicKey")
	}
	return
}

// Validate checks the WithdrawalMessage's expiry and signature. If the
// signature is invalid, or if the WithdrawlMessage is already expired, or it
// expires too far into the future, an error is returned.
func (wm *WithdrawalMessage) Validate(blockHeight, expiry types.BlockHeight, hash crypto.Hash, sig crypto.Signature) error {
	return errors.Compose(
		wm.ValidateExpiry(blockHeight, expiry),
		wm.ValidateSignature(hash, sig),
	)
}

// ValidateExpiry returns an error if the withdrawal message is either already
// expired or if it expires too far into the future
func (wm *WithdrawalMessage) ValidateExpiry(blockHeight, expiry types.BlockHeight) error {
	// Verify the current blockheight does not exceed the expiry
	if blockHeight > wm.Expiry {
		return ErrWithdrawalExpired
	}
	// Verify the withdrawal is not too far into the future
	if wm.Expiry > expiry {
		return ErrWithdrawalExtremeFuture
	}
	return nil
}

// ValidateSignature returns an error if the provided signature is invalid
func (wm *WithdrawalMessage) ValidateSignature(hash crypto.Hash, sig crypto.Signature) error {
	err := crypto.VerifyHash(hash, wm.Account.PK(), sig)
	if err != nil {
		return errors.Compose(err, ErrWithdrawalInvalidSignature)
	}
	return nil
}
