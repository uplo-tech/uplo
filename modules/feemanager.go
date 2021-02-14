package modules

import (
	"github.com/uplo-tech/uplo/types"
)

const (
	// FeeManagerDir is the name of the directory that is used to store the
	// FeeManager's persistent data
	FeeManagerDir = "feemanager"
)

// AppUID is a unique identifier for an application that had submitted a fee to
// the FeeManager
type AppUID string

// FeeUID is a unique identifier for a fee that is being managed by the
// FeeManager
type FeeUID string

type (
	// AppFee is the struct that contains information about a fee submitted by
	// an application to the FeeManager
	AppFee struct {
		// Address of the developer wallet
		Address types.UnlockHash `json:"address"`

		// Amount of UPLOthat the Fee is for
		Amount types.Currency `json:"amount"`

		// AppUID is a unique Application ID that the fee is for
		AppUID AppUID `json:"appuid"`

		// FeeUID is a unique identifier for the Fee
		FeeUID FeeUID `json:"feeuid"`

		// PaymentCompleted indicates whether the payment for this fee has
		// appeared on-chain.
		PaymentCompleted bool `json:"paymentcompleted"`

		// PayoutHeight is the height at which the fee will be paid out.
		PayoutHeight types.BlockHeight `json:"payoutheight"`

		// Recurring indicates whether or not this fee is a recurring fee and
		// will be charged in the next period as well
		//
		// NOTE: the application is responsible for submitting the fee again,
		// the FeeManager is not responsible for processing this fee on a
		// recurring basis
		Recurring bool `json:"recurring"`

		// Timestamp is the moment that the fee was requested.
		Timestamp int64 `json:"timestamp"`

		// TransactionCreated indicates whether the transaction for this fee has
		// been created and sent to the Uplo network for processing.
		TransactionCreated bool `json:"transactioncreated"`
	}

	// FeeManager manages fees for applications
	FeeManager interface {
		// AddFee adds a fee for the FeeManager to manage, returning the UID of
		// the fee.
		AddFee(address types.UnlockHash, amount types.Currency, appUID AppUID, recurring bool) (FeeUID, error)

		// CancelFee cancels the fee associated with the FeeUID
		CancelFee(feeUID FeeUID) error

		// Close closes the FeeManager
		Close() error

		// PaidFees returns all the paid fees that are being tracked by the
		// FeeManager
		PaidFees() ([]AppFee, error)

		// PayoutHeight returns the nextPayoutHeight of the FeeManager
		PayoutHeight() (types.BlockHeight, error)

		// PendingFees returns all the pending fees that are being tracked by the
		// FeeManager
		PendingFees() ([]AppFee, error)
	}
)

// Implement a ByTimestamp sort for the AppFees.
type (
	// AppFeeByTimestamp is a helper struct for the ByTimestamp sort.
	AppFeeByTimestamp []AppFee
)

func (afbt AppFeeByTimestamp) Len() int {
	return len(afbt)
}

func (afbt AppFeeByTimestamp) Swap(i, j int) {
	afbt[i], afbt[j] = afbt[j], afbt[i]
}

func (afbt AppFeeByTimestamp) Less(i, j int) bool {
	return afbt[i].Timestamp < afbt[j].Timestamp
}
