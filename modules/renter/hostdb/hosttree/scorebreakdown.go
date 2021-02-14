package hosttree

import (
	"math/big"

	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

// ScoreBreakdown is an interface that allows us to mock the hostAdjustments
// during testing.
type ScoreBreakdown interface {
	HostScoreBreakdown(totalScore types.Currency, ignoreAge, ignoreDuration, ignoreUptime bool) modules.HostScoreBreakdown
	Score() types.Currency
}

// HostAdjustments contains all the adjustments relevant to a host's score and
// implements the scoreBreakdown interface.
type HostAdjustments struct {
	AcceptContractAdjustment   float64
	AgeAdjustment              float64
	BasePriceAdjustment        float64
	BurnAdjustment             float64
	CollateralAdjustment       float64
	DurationAdjustment         float64
	InteractionAdjustment      float64
	PriceAdjustment            float64
	StorageRemainingAdjustment float64
	UptimeAdjustment           float64
	VersionAdjustment          float64
}

var (
	// Previous constructions of the hostdb required the baseWeight to be large
	// to prevent fractional results, but the current iteration of the hostdb
	// has no issues, the scores will always be quite large.
	baseWeight = types.NewCurrency64(1)
)

// conversionRate computes the likelihood of a host with 'score' to be drawn
// from the hosttree assuming that all hosts have 'totalScore'.
func conversionRate(score, totalScore types.Currency) float64 {
	if totalScore.IsZero() {
		totalScore = types.NewCurrency64(1)
	}
	conversionRate, _ := big.NewRat(0, 1).SetFrac(score.Mul64(50).Big(), totalScore.Big()).Float64()
	if conversionRate > 100 {
		conversionRate = 100
	}
	return conversionRate
}

// HostScoreBreakdown converts a HostAdjustments object into a
// modules.HostScoreBreakdown.
func (h HostAdjustments) HostScoreBreakdown(totalScore types.Currency, ignoreAge, ignoreDuration, ignoreUptime bool) modules.HostScoreBreakdown {
	// Set the ignored fields to 1.
	if ignoreAge {
		h.AgeAdjustment = 1.0
	}
	if ignoreUptime {
		h.UptimeAdjustment = 1.0
	}
	if ignoreDuration {
		h.DurationAdjustment = 1.0
	}
	// Create the breakdown.
	score := h.Score()
	return modules.HostScoreBreakdown{
		Score:          score,
		ConversionRate: conversionRate(score, totalScore),

		AcceptContractAdjustment:   h.AcceptContractAdjustment,
		AgeAdjustment:              h.AgeAdjustment,
		BasePriceAdjustment:        h.BasePriceAdjustment,
		BurnAdjustment:             h.BurnAdjustment,
		CollateralAdjustment:       h.CollateralAdjustment,
		DurationAdjustment:         h.DurationAdjustment,
		InteractionAdjustment:      h.InteractionAdjustment,
		PriceAdjustment:            h.PriceAdjustment,
		StorageRemainingAdjustment: h.StorageRemainingAdjustment,
		UptimeAdjustment:           h.UptimeAdjustment,
		VersionAdjustment:          h.VersionAdjustment,
	}
}

// Score combines the individual adjustments of the breakdown into a single
// score.
func (h HostAdjustments) Score() types.Currency {
	// Combine the adjustments.
	fullPenalty := h.AgeAdjustment *
		h.AcceptContractAdjustment *
		h.BasePriceAdjustment *
		h.BurnAdjustment *
		h.CollateralAdjustment *
		h.DurationAdjustment *
		h.InteractionAdjustment *
		h.PriceAdjustment *
		h.StorageRemainingAdjustment *
		h.UptimeAdjustment *
		h.VersionAdjustment

	// Return a types.Currency.
	weight := baseWeight.MulFloat(fullPenalty)
	if weight.IsZero() {
		// A weight of zero is problematic for for the host tree.
		return types.NewCurrency64(1)
	}
	return weight
}
