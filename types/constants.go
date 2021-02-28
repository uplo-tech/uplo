package types

// constants.go contains the Uplo constants. Depending on which build tags are
// used, the constants will be initialized to different values.
//
// CONTRIBUTE: We don't have way to check that the non-test constants are all
// sane, plus we have no coverage for them.

import (
	"math"
	"math/big"
	"time"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/crypto"
)

var (
	// ASICHardforkHeight is the height at which the hardfork targeting
	// selected ASICs was activated.
	ASICHardforkHeight BlockHeight

	// ASICHardforkTotalTarget is the initial target after the ASIC hardfork.
	// The actual target at ASICHardforkHeight is replaced with this value in
	// order to prevent intolerably slow block times post-fork.
	ASICHardforkTotalTarget Target

	// ASICHardforkTotalTime is the initial total time after the ASIC
	// hardfork. The actual total time at ASICHardforkHeight is replaced with
	// this value in order to prevent intolerably slow block times post-fork.
	ASICHardforkTotalTime int64

	// ASICHardforkFactor is the factor by which the hashrate of targeted
	// ASICs will be reduced.
	ASICHardforkFactor = uint64(1009)

	// ASICHardforkReplayProtectionPrefix is a byte that prefixes
	// UplocoinInputs and UplofundInputs when calculating SigHashes to protect
	// against replay attacks.
	ASICHardforkReplayProtectionPrefix = []byte{0}

	// FoundationHardforkReplayProtectionPrefix is a byte that prefixes
	// UplocoinInputs and UplofundInputs when calculating SigHashes to protect
	// against replay attacks.
	FoundationHardforkReplayProtectionPrefix = []byte{1}

	// BlockFrequency is the desired number of seconds that
	// should elapse, on average, between successive Blocks.
	BlockFrequency BlockHeight
	// BlockSizeLimit is the maximum size of a binary-encoded Block
	// that is permitted by the consensus rules.
	BlockSizeLimit = uint64(2e6)
	// BlocksPerHour is the number of blocks expected to be mined per hour.
	BlocksPerHour = BlockHeight(6)
	// BlocksPerDay is the number of blocks expected to be mined per day.
	BlocksPerDay = 24 * BlocksPerHour
	// BlocksPerWeek is the number of blocks expected to be mined per week.
	BlocksPerWeek = 7 * BlocksPerDay
	// BlocksPerMonth is the number of blocks expected to be mined per month.
	BlocksPerMonth = 30 * BlocksPerDay
	// BlocksPerYear is the number of blocks expected to be mined per year.
	BlocksPerYear = 365 * BlocksPerDay

	// EndOfTime is value to be used when a date in the future is needed for
	// validation
	EndOfTime = time.Unix(0, math.MaxInt64)

	// ExtremeFutureThreshold is a temporal limit beyond which Blocks are
	// discarded by the consensus rules. When incoming Blocks are processed, their
	// Timestamp is allowed to exceed the processor's current time by a small amount.
	// But if the Timestamp is further into the future than ExtremeFutureThreshold,
	// the Block is immediately discarded.
	ExtremeFutureThreshold Timestamp

	// FoundationHardforkHeight is the height at which the Foundation subsidy
	// hardfork was activated.
	FoundationHardforkHeight BlockHeight

	// FoundationSubsidyFrequency is the number of blocks between each
	// Foundation subsidy payout. Although the subsidy is calculated on a
	// per-block basis, it "pays out" much less frequently in order to reduce
	// the number of UplocoinOutputs created.
	FoundationSubsidyFrequency BlockHeight

	// FoundationSubsidyPerBlock is the amount allocated to the Foundation
	// subsidy per block.
	FoundationSubsidyPerBlock = UplocoinPrecision.Mul64(30e3)

	// FutureThreshold is a temporal limit beyond which Blocks are
	// discarded by the consensus rules. When incoming Blocks are processed, their
	// Timestamp is allowed to exceed the processor's current time by no more than
	// FutureThreshold. If the excess duration is larger than FutureThreshold, but
	// smaller than ExtremeFutureThreshold, the Block may be held in memory until
	// the Block's Timestamp exceeds the current time by less than FutureThreshold.
	FutureThreshold Timestamp
	// GenesisBlock is the first block of the block chain
	GenesisBlock Block

	// GenesisID is used in many places. Calculating it once saves lots of
	// redundant computation.
	GenesisID BlockID

	// GenesisUplocoinAllocation is the set of UplocoinOutputs created in the Genesis
	// block
	GenesisUplocoinAllocation []UplocoinOutput
	// GenesisUplofundAllocation is the set of UplofundOutputs created in the Genesis
	// block.
	GenesisUplofundAllocation []UplofundOutput
	// GenesisTimestamp is the timestamp when genesis block was mined
	GenesisTimestamp Timestamp
	// InitialCoinbase is the coinbase reward of the Genesis block.
	InitialCoinbase = uint64(300e3)

	// InitialFoundationUnlockHash is the primary Foundation subsidy address. It
	// receives the initial Foundation subsidy. The keys that this address was
	// derived from can also be used to set a new primary and failsafe address.
	InitialFoundationUnlockHash UnlockHash
	// InitialFoundationTestingSalt is the salt used to generate the
	// UnlockConditions and signing keys for the InitialFoundationUnlockHash.
	InitialFoundationTestingSalt = "saltgenprimary"
	// InitialFoundationFailsafeUnlockHash is the "backup" Foundation address.
	// It does not receive the Foundation subsidy, but its keys can be used to
	// set a new primary and failsafe address. These UnlockConditions should
	// also be subject to a timelock that prevents the failsafe from being used
	// immediately.
	InitialFoundationFailsafeUnlockHash UnlockHash
	// InitialFoundationFailsafeTestingSalt is the salt used to generate the
	// UnlockConditions and signing keys for the
	// InitialFoundationFailsafeUnlockHash.
	InitialFoundationFailsafeTestingSalt = "saltgenfailsafe"
	// InitialFoundationSubsidy is the one-time subsidy sent to the Foundation
	// address upon activation of the hardfork, representing one year's worth of
	// block subsidies.
	InitialFoundationSubsidy = UplocoinPrecision.Mul64(30e3).Mul64(uint64(BlocksPerYear))

	// MaturityDelay specifies the number of blocks that a maturity-required output
	// is required to be on hold before it can be spent on the blockchain.
	// Outputs are maturity-required if they are highly likely to be altered or
	// invalidated in the event of a small reorg. One example is the block reward,
	// as a small reorg may invalidate the block reward. Another example is a uplofund
	// payout, as a tiny reorg may change the value of the payout, and thus invalidate
	// any transactions spending the payout. File contract payouts also are subject to
	// a maturity delay.
	MaturityDelay BlockHeight
	// MaxTargetAdjustmentDown restrict how much the block difficulty is allowed to
	// change in a single step, which is important to limit the effect of difficulty
	// raising and lowering attacks.
	MaxTargetAdjustmentDown *big.Rat
	// MaxTargetAdjustmentUp restrict how much the block difficulty is allowed to
	// change in a single step, which is important to limit the effect of difficulty
	// raising and lowering attacks.
	MaxTargetAdjustmentUp *big.Rat
	// MedianTimestampWindow tells us how many blocks to look back when calculating
	// the median timestamp over the previous n blocks. The timestamp of a block is
	// not allowed to be less than or equal to the median timestamp of the previous n
	// blocks, where for Uplo this number is typically 11.
	MedianTimestampWindow = uint64(11)
	// MinimumCoinbase is the minimum coinbase reward for a block.
	// The coinbase decreases in each block after the Genesis block,
	// but it will not decrease past MinimumCoinbase.
	MinimumCoinbase uint64

	// Oak hardfork constants. Oak is the name of the difficulty algorithm for
	// Uplo following a hardfork at block 135e3.

	// OakDecayDenom is the denominator for how much the total timestamp is decayed
	// each step.
	OakDecayDenom int64
	// OakDecayNum is the numerator for how much the total timestamp is decayed each
	// step.
	OakDecayNum int64
	// OakHardforkBlock is the height at which the hardfork to switch to the oak
	// difficulty adjustment algorithm is triggered.
	OakHardforkBlock BlockHeight
	// OakHardforkFixBlock is the height at which the hardfork to switch from the broken
	// oak difficulty adjustment algorithm to the fixed oak difficulty adjustment
	// algorithm is triggered.
	OakHardforkFixBlock BlockHeight
	// OakHardforkTxnSizeLimit is the maximum size allowed for a transaction, a change
	// which I believe was implemented simultaneously with the oak hardfork.
	OakHardforkTxnSizeLimit = uint64(64e3) // 64 KB
	// OakMaxBlockShift is the maximum number of seconds that the oak algorithm will shift
	// the difficulty.
	OakMaxBlockShift int64
	// OakMaxDrop is the drop is the maximum amount that the difficulty will drop each block.
	OakMaxDrop *big.Rat
	// OakMaxRise is the maximum amount that the difficulty will rise each block.
	OakMaxRise *big.Rat

	// RootDepth is the cumulative target of all blocks. The root depth is essentially
	// the maximum possible target, there have been no blocks yet, so there is no
	// cumulated difficulty yet.
	RootDepth = Target{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	// RootTarget is the target for the genesis block - basically how much work needs
	// to be done in order to mine the first block. The difficulty adjustment algorithm
	// takes over from there.
	RootTarget Target
	// UplocoinPrecision is the number of base units in a Uplocoin. The Uplo network has a very
	// large number of base units. We call 10^24 of these a Uplocoin.
	//
	// The base unit for Bitcoin is called a satoshi. We call 10^8 satoshis a bitcoin,
	// even though the code itself only ever works with satoshis.
	UplocoinPrecision = NewCurrency(new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil))
	// UplofundCount is the total number of Uplofunds in existence.
	UplofundCount = NewCurrency64(100000)
	// UplofundPortion is the percentage of Uplocoins that is taxed from FileContracts.
	UplofundPortion = big.NewRat(15, 1000)
	// TargetWindow is the number of blocks to look backwards when determining how much
	// time has passed vs. how many blocks have been created. It's only used in the old,
	// broken difficulty adjustment algorithm.
	TargetWindow BlockHeight
)

var (
	// TaxHardforkHeight is the height at which the tax hardfork occurred.
	TaxHardforkHeight = build.Select(build.Var{
		Dev:      BlockHeight(10),
		Standard: BlockHeight(21e3),
		Testing:  BlockHeight(10),
	}).(BlockHeight)
)

// init checks which build constant is in place and initializes the variables
// accordingly.
func init() {
	if build.Release == "dev" {
		// 'dev' settings are for small developer testnets, usually on the same
		// computer. Settings are slow enough that a small team of developers
		// can coordinate their actions over a the developer testnets, but fast
		// enough that there isn't much time wasted on waiting for things to
		// happen.
		ASICHardforkHeight = 20
		ASICHardforkTotalTarget = Target{0, 0, 0, 8}
		ASICHardforkTotalTime = 800

		FoundationHardforkHeight = 100
		FoundationSubsidyFrequency = 10

		initialFoundationUnlockConditions, _ := GenerateDeterministicMultisig(2, 3, InitialFoundationTestingSalt)
		initialFoundationFailsafeUnlockConditions, _ := GenerateDeterministicMultisig(3, 5, InitialFoundationFailsafeTestingSalt)
		InitialFoundationUnlockHash = initialFoundationUnlockConditions.UnlockHash()
		InitialFoundationFailsafeUnlockHash = initialFoundationFailsafeUnlockConditions.UnlockHash()

		BlockFrequency = 12                      // 12 seconds: slow enough for developers to see ~each block, fast enough that blocks don't waste time.
		MaturityDelay = 10                       // 60 seconds before a delayed output matures.
		GenesisTimestamp = Timestamp(1424139000) // Change as necessary.
		RootTarget = Target{0, 0, 2}             // Standard developer CPUs will be able to mine blocks with the race library activated.

		TargetWindow = 20                              // Difficulty is adjusted based on prior 20 blocks.
		MaxTargetAdjustmentUp = big.NewRat(120, 100)   // Difficulty adjusts quickly.
		MaxTargetAdjustmentDown = big.NewRat(100, 120) // Difficulty adjusts quickly.
		FutureThreshold = 2 * 60                       // 2 minutes.
		ExtremeFutureThreshold = 4 * 60                // 4 minutes.

		MinimumCoinbase = 30e3

		OakHardforkBlock = 100
		OakHardforkFixBlock = 105
		OakDecayNum = 985
		OakDecayDenom = 1000
		OakMaxBlockShift = 3
		OakMaxRise = big.NewRat(102, 100)
		OakMaxDrop = big.NewRat(100, 102)

		// Populate the void address with 1 billion Uplocoins in the genesis block.
		GenesisUplocoinAllocation = []UplocoinOutput{
			{
				Value:      NewCurrency64(1000000000).Mul(UplocoinPrecision),
				UnlockHash: UnlockHash{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
		}

		GenesisUplofundAllocation = []UplofundOutput{
			{
				Value:      NewCurrency64(2000),
				UnlockHash: UnlockHash{214, 166, 197, 164, 29, 201, 53, 236, 106, 239, 10, 158, 127, 131, 20, 138, 63, 221, 230, 16, 98, 247, 32, 77, 210, 68, 116, 12, 241, 89, 27, 223},
			},
			{
				Value:      NewCurrency64(7000),
				UnlockHash: UnlockHash{209, 246, 228, 60, 248, 78, 242, 110, 9, 8, 227, 248, 225, 216, 163, 52, 142, 93, 47, 176, 103, 41, 137, 80, 212, 8, 132, 58, 241, 189, 2, 17},
			},
			{
				Value:      NewCurrency64(1000),
				UnlockHash: UnlockConditions{}.UnlockHash(),
			},
		}
	} else if build.Release == "testing" {
		// 'testing' settings are for automatic testing, and create much faster
		// environments than a human can interact with.
		ASICHardforkHeight = 5
		ASICHardforkTotalTarget = Target{255, 255}
		ASICHardforkTotalTime = 10e3

		FoundationHardforkHeight = 50
		FoundationSubsidyFrequency = 5

		initialFoundationUnlockConditions, _ := GenerateDeterministicMultisig(2, 3, InitialFoundationTestingSalt)
		initialFoundationFailsafeUnlockConditions, _ := GenerateDeterministicMultisig(3, 5, InitialFoundationFailsafeTestingSalt)
		InitialFoundationUnlockHash = initialFoundationUnlockConditions.UnlockHash()
		InitialFoundationFailsafeUnlockHash = initialFoundationFailsafeUnlockConditions.UnlockHash()

		BlockFrequency = 1 // As fast as possible
		MaturityDelay = 3
		GenesisTimestamp = CurrentTimestamp() - 1e6
		RootTarget = Target{128} // Takes an expected 2 hashes; very fast for testing but still probes 'bad hash' code.

		// A restrictive difficulty clamp prevents the difficulty from climbing
		// during testing, as the resolution on the difficulty adjustment is
		// only 1 second and testing mining should be happening substantially
		// faster than that.
		TargetWindow = 200
		MaxTargetAdjustmentUp = big.NewRat(10001, 10000)
		MaxTargetAdjustmentDown = big.NewRat(9999, 10000)
		FutureThreshold = 3        // 3 seconds
		ExtremeFutureThreshold = 6 // 6 seconds

		MinimumCoinbase = 299990 // Minimum coinbase is hit after 10 blocks to make testing minimum-coinbase code easier.

		// Do not let the difficulty change rapidly - blocks will be getting
		// mined far faster than the difficulty can adjust to.
		OakHardforkBlock = 20
		OakHardforkFixBlock = 23
		OakDecayNum = 9999
		OakDecayDenom = 10e3
		OakMaxBlockShift = 3
		OakMaxRise = big.NewRat(10001, 10e3)
		OakMaxDrop = big.NewRat(10e3, 10001)

		// Populate the void address with 1 billion Uplocoins in the genesis block.
		GenesisUplocoinAllocation = []UplocoinOutput{
			{
				Value:      NewCurrency64(1000000000).Mul(UplocoinPrecision),
				UnlockHash: UnlockHash{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
		}

		GenesisUplofundAllocation = []UplofundOutput{
			{
				Value:      NewCurrency64(2000),
				UnlockHash: UnlockHash{214, 166, 197, 164, 29, 201, 53, 236, 106, 239, 10, 158, 127, 131, 20, 138, 63, 221, 230, 16, 98, 247, 32, 77, 210, 68, 116, 12, 241, 89, 27, 223},
			},
			{
				Value:      NewCurrency64(7000),
				UnlockHash: UnlockHash{209, 246, 228, 60, 248, 78, 242, 110, 9, 8, 227, 248, 225, 216, 163, 52, 142, 93, 47, 176, 103, 41, 137, 80, 212, 8, 132, 58, 241, 189, 2, 17},
			},
			{
				Value:      NewCurrency64(1000),
				UnlockHash: UnlockConditions{}.UnlockHash(),
			},
		}
	} else if build.Release == "standard" {
		// 'standard' settings are for the full network. They are slow enough
		// that the network is secure in a real-world byzantine environment.

		// A total time of 120,000 is chosen because that represents the total
		// time elapsed at a perfect equilibrium, indicating a visible average
		// block time that perfectly aligns with what is expected. A total
		// target of 67 leading zeroes is chosen because that aligns with the
		// amount of hashrate that we expect to be on the network after the
		// hardfork.
		ASICHardforkHeight = 1
		ASICHardforkTotalTarget = Target{0, 0, 0, 0, 0, 0, 0, 0, 32}
		ASICHardforkTotalTime = 1e3

		// The Foundation subsidy hardfork activates at approximately 11pm EST
		// on February 3, 2021.
		FoundationHardforkHeight = 2
		// Subsidies are paid out approximately once per month. Since actual
		// months vary in length, we instead divide the total number of blocks
		// per year by 12.
		FoundationSubsidyFrequency = BlocksPerYear / 12

		InitialFoundationUnlockHash = MustParseAddress("12202e08339da0e5071262d2edd478ca7bc44ac55cfae03788723eeae4481af8180d4aa1c1ad")
		InitialFoundationFailsafeUnlockHash = MustParseAddress("27c22a6c6e6645802a3b8fa0e5374657438ef12716d2205d3e866272de1b644dbabd53d6d560")


		// A block time of 1 block per 10 minutes is chosen to follow Bitcoin's
		// example. The security lost by lowering the block time is not
		// insignificant, and the convenience gained by lowering the blocktime
		// even down to 90 seconds is not significant. I do feel that 10
		// minutes could even be too short, but it has worked well for Bitcoin.
		BlockFrequency = 600

		// Payouts take 1 day to mature. This is to prevent a class of double
		// spending attacks parties unintentionally spend coins that will stop
		// existing after a blockchain reorganization. There are multiple
		// classes of payouts in Uplo that depend on a previous block - if that
		// block changes, then the output changes and the previously existing
		// output ceases to exist. This delay stops both unintentional double
		// spending and stops a small set of long-range mining attacks.
		MaturityDelay = 144

		// The genesis timestamp is set to June 6th, because that is when the
		// 100-block developer premine started. The trailing zeroes are a
		// bonus, and make the timestamp easier to memorize.
		GenesisTimestamp = Timestamp(1612828800) // June 6th, 2015 @ 2:13pm UTC.

		// The RootTarget was set such that the developers could reasonable
		// premine 100 blocks in a day. It was known to the developers at launch
		// this this was at least one and perhaps two orders of magnitude too
		// small.
		RootTarget = Target{0, 0, 0, 0, 32}

		// When the difficulty is adjusted, it is adjusted by looking at the
		// timestamp of the 1000th previous block. This minimizes the abilities
		// of miners to attack the network using rogue timestamps.
		TargetWindow = 1e3

		// The difficulty adjustment is clamped to 2.5x every 500 blocks. This
		// corresponds to 6.25x every 2 weeks, which can be compared to
		// Bitcoin's clamp of 4x every 2 weeks. The difficulty clamp is
		// primarily to stop difficulty raising attacks. Uplo's safety margin is
		// similar to Bitcoin's despite the looser clamp because Uplo's
		// difficulty is adjusted four times as often. This does result in
		// greater difficulty oscillation, a tradeoff that was chosen to be
		// acceptable due to Uplo's more vulnerable position as an altcoin.
		MaxTargetAdjustmentUp = big.NewRat(25, 10)
		MaxTargetAdjustmentDown = big.NewRat(10, 25)

		// Blocks will not be accepted if their timestamp is more than 3 hours
		// into the future, but will be accepted as soon as they are no longer
		// 3 hours into the future. Blocks that are greater than 5 hours into
		// the future are rejected outright, as it is assumed that by the time
		// 2 hours have passed, those blocks will no longer be on the longest
		// chain. Blocks cannot be kept forever because this opens a DoS
		// vector.
		FutureThreshold = 3 * 60 * 60        // 3 hours.
		ExtremeFutureThreshold = 5 * 60 * 60 // 5 hours.

		// The minimum coinbase is set to 30,000. Because the coinbase
		// decreases by 1 every time, it means that Uplo's coinbase will have an
		// increasingly potent dropoff for about 5 years, until inflation more
		// or less permanently settles around 2%.
		MinimumCoinbase = 30e3

		// The oak difficulty adjustment hardfork is set to trigger at block
		// 135,000, which is just under 6 months after the hardfork was first
		// released as beta software to the network. This hopefully gives
		// everyone plenty of time to upgrade and adopt the hardfork, while also
		// being earlier than the most optimistic shipping dates for the miners
		// that would otherwise be very disruptive to the network.
		//
		// There was a bug in the original Oak hardfork that had to be quickly
		// followed up with another fix. The height of that fix is the
		// OakHardforkFixBlock.
		OakHardforkBlock = 0
		OakHardforkFixBlock = 0

		// The decay is kept at 995/1000, or a decay of about 0.5% each block.
		// This puts the halflife of a block's relevance at about 1 day. This
		// allows the difficulty to adjust rapidly if the hashrate is adjusting
		// rapidly, while still keeping a relatively strong insulation against
		// random variance.
		OakDecayNum = 995
		OakDecayDenom = 1e3

		// The block shift determines the most that the difficulty adjustment
		// algorithm is allowed to shift the target block time. With a block
		// frequency of 600 seconds, the min target block time is 200 seconds,
		// and the max target block time is 1800 seconds.
		OakMaxBlockShift = 3

		// The max rise and max drop for the difficulty is kept at 0.4% per
		// block, which means that in 1008 blocks the difficulty can move a
		// maximum of about 55x. This is significant, and means that dramatic
		// hashrate changes can be responded to quickly, while still forcing an
		// attacker to do a significant amount of work in order to execute a
		// difficulty raising attack, and minimizing the chance that an attacker
		// can get lucky and fake a ton of work.
		OakMaxRise = big.NewRat(1004, 1e3)
		OakMaxDrop = big.NewRat(1e3, 1004)

		GenesisUplocoinAllocation = []UplocoinOutput{
			{
				Value:      NewCurrency64(10000000),
				UnlockHash: UnlockHash{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
		}

		GenesisUplofundAllocation = []UplofundOutput{
			{
				Value:      NewCurrency64(100000),
				UnlockHash: UnlockHash{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
		}
	}

	// Create the genesis block.
	GenesisBlock = Block{
		Timestamp: GenesisTimestamp,
		Transactions: []Transaction{
			{UplofundOutputs: GenesisUplofundAllocation},
		},
	}
	// Calculate the genesis ID.
	GenesisID = GenesisBlock.ID()
}

// GenerateDeterministicMultisig is a helper function that generates a set of
// multisig UnlockConditions along with their signing keys.
func GenerateDeterministicMultisig(m, n int, salt string) (UnlockConditions, []crypto.SecretKey) {
	uc := UnlockConditions{
		PublicKeys:         make([]UploPublicKey, n),
		SignaturesRequired: uint64(m),
	}
	keys := make([]crypto.SecretKey, n)

	entropy := crypto.HashObject(salt)
	for i := range keys {
		sk, pk := crypto.GenerateKeyPairDeterministic(entropy)
		keys[i], uc.PublicKeys[i] = sk, Ed25519PublicKey(pk)
		entropy = crypto.HashBytes(entropy[:])
	}
	return uc, keys
}
