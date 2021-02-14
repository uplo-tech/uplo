package modules

import (
	"github.com/uplo-tech/uplo/types"
)

const (
	// ExplorerDir is the name of the directory that is typically used for the
	// explorer.
	ExplorerDir = "explorer"
)

type (
	// BlockFacts returns a bunch of statistics about the consensus set as they
	// were at a specific block.
	BlockFacts struct {
		BlockID           types.BlockID     `json:"blockid"`
		Difficulty        types.Currency    `json:"difficulty"`
		EstimatedHashrate types.Currency    `json:"estimatedhashrate"`
		Height            types.BlockHeight `json:"height"`
		MaturityTimestamp types.Timestamp   `json:"maturitytimestamp"`
		Target            types.Target      `json:"target"`
		TotalCoins        types.Currency    `json:"totalcoins"`

		// Transaction type counts.
		MinerPayoutCount          uint64 `json:"minerpayoutcount"`
		TransactionCount          uint64 `json:"transactioncount"`
		UplocoinInputCount         uint64 `json:"Uplocoininputcount"`
		UplocoinOutputCount        uint64 `json:"Uplocoinoutputcount"`
		FileContractCount         uint64 `json:"filecontractcount"`
		FileContractRevisionCount uint64 `json:"filecontractrevisioncount"`
		StorageProofCount         uint64 `json:"storageproofcount"`
		UplofundInputCount         uint64 `json:"uplofundinputcount"`
		UplofundOutputCount        uint64 `json:"uplofundoutputcount"`
		MinerFeeCount             uint64 `json:"minerfeecount"`
		ArbitraryDataCount        uint64 `json:"arbitrarydatacount"`
		TransactionSignatureCount uint64 `json:"transactionsignaturecount"`

		// Factoids about file contracts.
		ActiveContractCost  types.Currency `json:"activecontractcost"`
		ActiveContractCount uint64         `json:"activecontractcount"`
		ActiveContractSize  types.Currency `json:"activecontractsize"`
		TotalContractCost   types.Currency `json:"totalcontractcost"`
		TotalContractSize   types.Currency `json:"totalcontractsize"`
		TotalRevisionVolume types.Currency `json:"totalrevisionvolume"`
	}

	// Explorer tracks the blockchain and provides tools for gathering
	// statistics and finding objects or patterns within the blockchain.
	Explorer interface {
		Alerter

		// Block returns the block that matches the input block id. The bool
		// indicates whether the block appears in the blockchain.
		Block(types.BlockID) (types.Block, types.BlockHeight, bool)

		// BlockFacts returns a set of statistics about the blockchain as they
		// appeared at a given block.
		BlockFacts(types.BlockHeight) (BlockFacts, bool)

		// LatestBlockFacts returns the block facts of the last block
		// in the explorer's database.
		LatestBlockFacts() BlockFacts

		// Transaction returns the block that contains the input transaction
		// id. The transaction itself is either the block (indicating the miner
		// payouts are somehow involved), or it is a transaction inside of the
		// block. The bool indicates whether the transaction is found in the
		// consensus set.
		Transaction(types.TransactionID) (types.Block, types.BlockHeight, bool)

		// UnlockHash returns all of the transaction ids associated with the
		// provided unlock hash.
		UnlockHash(types.UnlockHash) []types.TransactionID

		// UplocoinOutput will return the Uplocoin output associated with the
		// input id.
		UplocoinOutput(types.UplocoinOutputID) (types.UplocoinOutput, bool)

		// UplocoinOutputID returns all of the transaction ids associated with
		// the provided Uplocoin output id.
		UplocoinOutputID(types.UplocoinOutputID) []types.TransactionID

		// FileContractHistory returns the history associated with a file
		// contract, which includes the file contract itself and all of the
		// revisions that have been submitted to the blockchain. The first bool
		// indicates whether the file contract exists, and the second bool
		// indicates whether a storage proof was successfully submitted for the
		// file contract.
		FileContractHistory(types.FileContractID) (fc types.FileContract, fcrs []types.FileContractRevision, fcExists bool, storageProofExists bool)

		// FileContractID returns all of the transaction ids associated with
		// the provided file contract id.
		FileContractID(types.FileContractID) []types.TransactionID

		// UplofundOutput will return the uplofund output associated with the
		// input id.
		UplofundOutput(types.UplofundOutputID) (types.UplofundOutput, bool)

		// UplofundOutputID returns all of the transaction ids associated with
		// the provided uplofund output id.
		UplofundOutputID(types.UplofundOutputID) []types.TransactionID

		Close() error
	}
)
