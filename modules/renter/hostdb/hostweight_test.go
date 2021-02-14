package hostdb

import (
	"math"
	"testing"
	"time"

	"github.com/uplo-tech/errors"
	"github.com/uplo-tech/fastrand"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/uplo/types"
)

var (
	// Set the default test allowance
	DefaultTestAllowance = modules.Allowance{
		Funds:       types.UplocoinPrecision.Mul64(500),
		Hosts:       uint64(50),
		Period:      3 * types.BlocksPerMonth,
		RenewWindow: types.BlocksPerMonth,

		ExpectedStorage:    1e12,                                         // 1 TB
		ExpectedUpload:     uint64(200e9) / uint64(types.BlocksPerMonth), // 200 GB per month
		ExpectedDownload:   uint64(100e9) / uint64(types.BlocksPerMonth), // 100 GB per month
		ExpectedRedundancy: 3.0,                                          // default is 10/30 erasure coding
	}

	// The default entry to use when performing scoring.
	DefaultHostDBEntry = modules.HostDBEntry{
		HostExternalSettings: modules.HostExternalSettings{
			AcceptingContracts: true,
			MaxDuration:        26e3,
			RemainingStorage:   250e9,
			WindowSize:         144,

			Collateral:    types.NewCurrency64(250).Mul(types.UplocoinPrecision).Div(modules.BlockBytesPerMonthTerabyte),
			MaxCollateral: types.NewCurrency64(750).Mul(types.UplocoinPrecision),

			BaseRPCPrice:           types.UplocoinPrecision.Mul64(100).Div64(1e9),
			ContractPrice:          types.NewCurrency64(5).Mul(types.UplocoinPrecision),
			DownloadBandwidthPrice: types.UplocoinPrecision.Mul64(100).Div64(1e12),
			SectorAccessPrice:      types.UplocoinPrecision.Mul64(2).Div64(1e6),
			StoragePrice:           types.NewCurrency64(100).Mul(types.UplocoinPrecision).Div(modules.BlockBytesPerMonthTerabyte),

			Version: build.Version,
		},
	}
)

// calculateWeightFromUInt64Price will fill out a host entry with a bunch of
// defaults, and then grab the weight of that host using a set price.
func calculateWeightFromUInt64Price(price, collateral uint64) (weight types.Currency, err error) {
	hdb := bareHostDB()
	err = hdb.SetAllowance(DefaultTestAllowance)
	if err != nil {
		return
	}
	hdb.blockHeight = 0

	entry := DefaultHostDBEntry
	entry.StoragePrice = types.NewCurrency64(price).Mul(types.UplocoinPrecision).Div(modules.BlockBytesPerMonthTerabyte)
	entry.Collateral = types.NewCurrency64(collateral).Mul(types.UplocoinPrecision).Div(modules.BlockBytesPerMonthTerabyte)

	return hdb.weightFunc(entry).Score(), nil
}

// TestHostDBBasePriceAdjustment ensures that the basePriceAdjustment is impacted by
// changes to BaseRPCPrice, SectorAccessPrice, and MinDownloadBandwidthPrice.
func TestHostDBBasePriceAdjustment(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	entry := DefaultHostDBEntry

	// Confirm default entry has score of 1
	bpa := hdb.basePriceAdjustments(entry)
	if bpa != 1 {
		t.Error("BasePriceAdjustment for default entry should be 1 but was", bpa)
	}

	// Confirm a higher BaseRPCPrice results in an almost zero score
	entry.BaseRPCPrice = entry.MaxBaseRPCPrice().Mul64(2)
	bpa = hdb.basePriceAdjustments(entry)
	if bpa != math.SmallestNonzeroFloat64 {
		t.Errorf("BasePriceAdjustment should have been %v but was %v", math.SmallestNonzeroFloat64, bpa)
	}
	entry.BaseRPCPrice = DefaultHostDBEntry.BaseRPCPrice

	// Confirm a higher SectorAccessPrice results in an almost zero score
	entry.SectorAccessPrice = entry.MaxSectorAccessPrice().Mul64(2)
	bpa = hdb.basePriceAdjustments(entry)
	if bpa != math.SmallestNonzeroFloat64 {
		t.Errorf("BasePriceAdjustment should have been %v but was %v", math.SmallestNonzeroFloat64, bpa)
	}
	entry.SectorAccessPrice = DefaultHostDBEntry.SectorAccessPrice

	// Confirm a lower DownloadBandwidthPrice results in an almost zero score.
	// Check by adjusting the price with both constants
	entry.DownloadBandwidthPrice = DefaultHostDBEntry.DownloadBandwidthPrice.Div64(modules.MaxBaseRPCPriceVsBandwidth)
	bpa = hdb.basePriceAdjustments(entry)
	if bpa != math.SmallestNonzeroFloat64 {
		t.Errorf("BasePriceAdjustment should have been %v but was %v", math.SmallestNonzeroFloat64, bpa)
	}
	entry.DownloadBandwidthPrice = DefaultHostDBEntry.DownloadBandwidthPrice.Div64(modules.MaxSectorAccessPriceVsBandwidth)
	bpa = hdb.basePriceAdjustments(entry)
	if bpa != math.SmallestNonzeroFloat64 {
		t.Errorf("BasePriceAdjustment should have been %v but was %v", math.SmallestNonzeroFloat64, bpa)
	}
}

// TestHostWeightBasePrice checks that a host with an unacceptable BaseRPCPrice
// or SectorAccessPrice has a lower score.
func TestHostWeightBasePrice(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()

	entry := DefaultHostDBEntry
	entry2 := DefaultHostDBEntry
	entry2.BaseRPCPrice = entry.MaxBaseRPCPrice().Mul64(2)
	entry3 := DefaultHostDBEntry
	entry3.SectorAccessPrice = entry.MaxSectorAccessPrice().Mul64(2)

	sDefault := hdb.weightFunc(entry).Score()
	sInsaneBRPCPrice := hdb.weightFunc(entry2).Score()
	sInsaneSAPrice := hdb.weightFunc(entry3).Score()
	if sDefault.Cmp(sInsaneBRPCPrice) <= 0 {
		t.Log("Default Score", sDefault)
		t.Log("Bad BaseRPCPrice Score", sInsaneBRPCPrice)
		t.Error("Default host should have higher score")
	}
	if sDefault.Cmp(sInsaneSAPrice) <= 0 {
		t.Log("Default Score", sDefault)
		t.Log("Bad SectorAccess Score", sInsaneSAPrice)
		t.Error("Default host should have higher score")
	}
	if sInsaneBRPCPrice.Cmp(sInsaneSAPrice) != 0 {
		t.Log("Bad BaseRPCPrice Score", sInsaneBRPCPrice)
		t.Log("Bad SectorAccess Score", sInsaneSAPrice)
		t.Error("Hosts should have the same score")
	}
}

// TestHostWeightDistinctPrices ensures that the host weight is different if the
// prices are different, and that a higher price has a lower score.
func TestHostWeightDistinctPrices(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(300, 100)
	weight2, err2 := calculateWeightFromUInt64Price(301, 100)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) <= 0 {
		t.Log(weight1)
		t.Log(weight2)
		t.Error("Weight of expensive host is not the correct value.")
	}
}

// TestHostWeightDistinctCollateral ensures that the host weight is different if
// the collaterals are different, and that a higher collateral has a higher
// score.
func TestHostWeightDistinctCollateral(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(300, 100)
	weight2, err2 := calculateWeightFromUInt64Price(300, 99)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) <= 0 {
		t.Log(weight1)
		t.Log(weight2)
		t.Error("Weight of expensive host is not the correct value.")
	}
}

// When the collateral is below the cutoff, the collateral should be more
// important than the price.
func TestHostWeightCollateralBelowCutoff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(300, 10)
	weight2, err2 := calculateWeightFromUInt64Price(150, 5)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) <= 0 {
		t.Log(weight1)
		t.Log(weight2)
		t.Error("Weight of expensive host is not the correct value.")
	}
}

// When the collateral is below the cutoff, the price should be more important
// than the collateral.
func TestHostWeightCollateralAboveCutoff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(300, 1000)
	weight2, err2 := calculateWeightFromUInt64Price(150, 500)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) >= 0 {
		t.Log(weight1)
		t.Log(weight2)
		t.Error("Weight of expensive host is not the correct value.")
	}
}

// TestHostWeightIdenticalPrices checks that the weight function is
// deterministic for two hosts that have identical settings - each should get
// the same score.
func TestHostWeightIdenticalPrices(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(42, 100)
	weight2, err2 := calculateWeightFromUInt64Price(42, 100)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) != 0 {
		t.Error("Weight of identically priced hosts should be equal.")
	}
}

// TestHostWeightWithOnePricedZero checks that nothing unexpected happens when
// there is a zero price, and also checks that the zero priced host scores
// higher  than the host that charges money.
func TestHostWeightWithOnePricedZero(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(5, 10)
	weight2, err2 := calculateWeightFromUInt64Price(0, 10)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) >= 0 {
		t.Log(weight1)
		t.Log(weight2)
		t.Error("Zero-priced host should have higher weight than nonzero-priced host.")
	}
}

// TestHostWeightBothPricesZero checks that there is nondeterminism in the
// weight function even with zero value prices.
func TestHostWeightWithBothPricesZero(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(0, 100)
	weight2, err2 := calculateWeightFromUInt64Price(0, 100)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) != 0 {
		t.Error("Weight of two zero-priced hosts should be equal.")
	}
}

// TestHostWeightWithNoCollateral checks that nothing bad (like a panic) happens
// when the collateral is set to zero.
func TestHostWeightWithNoCollateral(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	weight1, err1 := calculateWeightFromUInt64Price(300, 1)
	weight2, err2 := calculateWeightFromUInt64Price(300, 0)
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal(err)
	}
	if weight1.Cmp(weight2) <= 0 {
		t.Log(weight1)
		t.Log(weight2)
		t.Error("Weight of lower priced host should be higher")
	}
}

// TestHostWeightMaxDuration checks that the host with an unacceptable duration
// has a lower score.
func TestHostWeightMaxDuration(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	err := hdb.SetAllowance(DefaultTestAllowance)
	if err != nil {
		t.Fatal(err)
	}

	entry := DefaultHostDBEntry
	entry2 := DefaultHostDBEntry
	entry2.MaxDuration = DefaultTestAllowance.Period + DefaultTestAllowance.RenewWindow

	// Entry2 is exactly at the limit. Weights should match.
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()
	if w1.Cmp(w2) != 0 {
		t.Error("Entries should have same weight", w1, w2)
	}

	// Entry2 is just below the limit. Should have smallest weight possible.
	entry2.MaxDuration--
	w2 = hdb.weightFunc(entry2).Score()
	if w1.Cmp(w2) <= 0 {
		t.Error("Entry2 should have smaller weight", w1, w2)
	}
	if w2.Cmp64(1) != 0 {
		t.Error("Entry2 should have smallest weight")
	}
}

// TestHostWeightStorageRemainingDifferences checks that the host with more
// collateral has more weight.
func TestHostWeightCollateralDifferences(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()

	entry := DefaultHostDBEntry
	entry2 := DefaultHostDBEntry
	entry2.Collateral = entry.Collateral.Mul64(2)
	entry2.MaxCollateral = entry.MaxCollateral.Mul64(2)

	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()
	if w1.Cmp(w2) <= 0 {
		t.Log("w1:", w1)
		t.Log("w2:", w2)
		t.Error("Larger collateral should have more weight")
	}
}

// TestHostWeightStorageRemainingDifferences checks that hosts with less storage
// remaining have a lower weight.
func TestHostWeightStorageRemainingDifferences(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()

	// Create two entries with different host keys.
	entry := DefaultHostDBEntry
	entry.PublicKey.Key = fastrand.Bytes(16)
	entry2 := DefaultHostDBEntry
	entry2.PublicKey.Key = fastrand.Bytes(16)

	// The first entry has more storage remaining than the second.
	entry.RemainingStorage = modules.DefaultAllowance.ExpectedStorage // 1e12
	entry2.RemainingStorage = 1e3

	// The entry with more storage should have the higher score.
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()
	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("Larger storage remaining should have more weight")
	}

	// Change both entries to have the same remaining storage but add contractInfo
	// to the HostDB to make it think that we already uploaded some data to one of
	// the entries. This entry should have the higher score.
	entry.RemainingStorage = 1e3
	entry2.RemainingStorage = 1e3
	hdb.knownContracts[entry.PublicKey.String()] = contractInfo{
		HostPublicKey: entry.PublicKey,
		StoredData:    hdb.allowance.ExpectedStorage,
	}
	w1 = hdb.weightFunc(entry).Score()
	w2 = hdb.weightFunc(entry2).Score()
	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("Entry with uploaded data should have higher score")
	}
}

// TestHostWeightVersionDifferences checks that a host with an out of date
// version has a lower score than a host with a more recent version.
func TestHostWeightVersionDifferences(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()

	entry := DefaultHostDBEntry
	entry2 := DefaultHostDBEntry
	entry2.Version = "v1.3.2"
	w1 := hdb.weightFunc(entry)
	w2 := hdb.weightFunc(entry2)

	if w1.Score().Cmp(w2.Score()) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("Higher version should have more weight")
	}
}

// TestHostWeightLifetimeDifferences checks that a host that has been on the
// chain for more time has a higher weight than a host that is newer.
func TestHostWeightLifetimeDifferences(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	hdb.blockHeight = 10000

	entry := DefaultHostDBEntry
	entry2 := DefaultHostDBEntry
	entry2.FirstSeen = 8100
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()

	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("Been around longer should have more weight")
	}
}

// TestHostWeightUptimeDifferences checks that hosts with poorer uptimes have
// lower weights.
func TestHostWeightUptimeDifferences(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	hdb.blockHeight = 10000

	entry := DefaultHostDBEntry
	entry.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: true},
	}

	entry2 := entry
	entry2.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: false},
	}
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()

	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("A host with recorded downtime should have a lower score")
	}
}

// TestHostWeightUptimeDifferences2 checks that hosts with poorer uptimes have
// lower weights.
func TestHostWeightUptimeDifferences2(t *testing.T) {
	t.Skip("Hostdb is not currently doing exponentiation on uptime")
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	hdb.blockHeight = 10000

	entry := DefaultHostDBEntry
	entry.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -200), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -180), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -160), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -140), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -120), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: false},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: true},
	}

	entry2 := entry
	entry2.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -200), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -180), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -160), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -140), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -120), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: false},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: true},
	}
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()

	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Errorf("Downtime that's further in the past should be penalized less")
	}
}

// TestHostWeightUptimeDifferences3 checks that hosts with poorer uptimes have
// lower weights.
func TestHostWeightUptimeDifferences3(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	hdb.blockHeight = 10000

	entry := DefaultHostDBEntry
	entry.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -200), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -180), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -160), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -140), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -120), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: false},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: true},
	}

	entry2 := entry
	entry2.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -200), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -180), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -160), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -140), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -120), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: false},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: false},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: true},
	}
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()

	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("A host with longer downtime should have a lower score")
	}
}

// TestHostWeightUptimeDifferences4 checks that hosts with poorer uptimes have
// lower weights.
func TestHostWeightUptimeDifferences4(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	hdb.blockHeight = 10000

	entry := DefaultHostDBEntry
	entry.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -200), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -180), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -160), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -140), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -120), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: false},
	}

	entry2 := entry
	entry2.ScanHistory = modules.HostDBScans{
		{Timestamp: time.Now().Add(time.Hour * -200), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -180), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -160), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -140), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -120), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -100), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -80), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -60), Success: true},
		{Timestamp: time.Now().Add(time.Hour * -40), Success: false},
		{Timestamp: time.Now().Add(time.Hour * -20), Success: false},
	}
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()

	if w1.Cmp(w2) <= 0 {
		t.Log(w1)
		t.Log(w2)
		t.Error("longer tail downtime should have a lower score")
	}
}

// TestHostWeightConstants checks a few relationships between the constants in
// the hostdb.
func TestHostWeightConstants(t *testing.T) {
	// Becaues we no longer use a large base weight, we require that the
	// collateral floor be higher than the price floor, and also that the
	// collateralExponentiationSmall be larger than the
	// priceExponentiationSmall. This protects most hosts from going anywhere
	// near a 0 score.
	if collateralFloor < priceFloor {
		t.Error("Collateral floor should be greater than or equal to price floor")
	}
	if collateralExponentiationSmall < priceExponentiationSmall {
		t.Error("small collateral exponentiation should be larger than small price exponentiation")
	}

	// Try a few hosts and make sure we always end up with a score that is
	// greater than 1 million.
	weight, err := calculateWeightFromUInt64Price(300, 100)
	if weight.Cmp(types.NewCurrency64(1e9)) < 0 {
		t.Error("weight is not sufficiently high for hosts")
	}
	if err != nil {
		t.Fatal(err)
	}
	weight, err = calculateWeightFromUInt64Price(1000, 1)
	if weight.Cmp(types.NewCurrency64(1e9)) < 0 {
		t.Error("weight is not sufficiently high for hosts")
	}
	if err != nil {
		t.Fatal(err)
	}

	hdb := bareHostDB()
	err = hdb.SetAllowance(DefaultTestAllowance)
	if err != nil {
		t.Fatal(err)
	}
	hdb.blockHeight = 0

	entry := DefaultHostDBEntry
	weight = hdb.weightFunc(entry).Score()
	if weight.Cmp(types.NewCurrency64(1e9)) < 0 {
		t.Error("weight is not sufficiently high for hosts")
	}
}

// TestHostWeightExtraPriceAdjustment tests the affects of changing
// BaseRPCPrice and SectorAccessPrice on the score.
func TestHostWeightExtraPriceAdjustments(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()

	allowance := DefaultTestAllowance
	err := hdb.SetAllowance(allowance)
	if err != nil {
		t.Fatal(err)
	}
	entry := DefaultHostDBEntry
	defaultScore := hdb.weightFunc(entry).Score()

	// Increasing Base RPC Price should decrease the score.
	entry.BaseRPCPrice = DefaultHostDBEntry.BaseRPCPrice.Mul64(2)
	higherBasePrice := hdb.weightFunc(entry).Score()
	if defaultScore.Cmp(higherBasePrice) <= 0 {
		t.Fatal("Expected score decrease with higher base price.")
	}

	// Increasing Base RPC Price should decrease the score.
	entry.BaseRPCPrice = DefaultHostDBEntry.BaseRPCPrice.Mul64(10)
	highestBasePrice := hdb.weightFunc(entry).Score()
	if higherBasePrice.Cmp(highestBasePrice) <= 0 {
		t.Fatal("Expected score decrease with higher base price.")
	}

	// Increasing SectorAccessPrice should decrease the score.
	entry = DefaultHostDBEntry // reset entry
	entry.SectorAccessPrice = DefaultHostDBEntry.SectorAccessPrice.Mul64(2)
	higherSectorPrice := hdb.weightFunc(entry).Score()
	if defaultScore.Cmp(higherSectorPrice) <= 0 {
		t.Fatal("Expected score decrease with higher sector access price")
	}

	// Increasing SectorAccessPrice should decrease the score.
	entry.SectorAccessPrice = DefaultHostDBEntry.SectorAccessPrice.Mul64(10)
	highestSectorPrice := hdb.weightFunc(entry).Score()
	if higherSectorPrice.Cmp(highestSectorPrice) <= 0 {
		t.Fatal("Expected score decrease with higher sector access price")
	}
}

// TestHostWeightAcceptContract checks that the host that doesn't accept
// contracts has a worse score than the one that does.
func TestHostWeightAcceptContract(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	hdb := bareHostDB()
	err := hdb.SetAllowance(DefaultTestAllowance)
	if err != nil {
		t.Fatal(err)
	}

	entry := DefaultHostDBEntry
	entry2 := DefaultHostDBEntry
	entry2.AcceptingContracts = false

	// Entry2 is not accepting contracts. Should have smallest weight possible.
	entry2.MaxDuration--
	w1 := hdb.weightFunc(entry).Score()
	w2 := hdb.weightFunc(entry2).Score()
	if w1.Cmp(w2) <= 0 {
		t.Error("Entry2 should have smaller weight", w1, w2)
	}
	if w2.Cmp64(1) != 0 {
		t.Error("Entry2 should have smallest weight")
	}
}
