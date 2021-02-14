package types

// block.go defines the Block type for Uplo, and provides some helper functions
// for working with blocks.

import (
	"bytes"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/encoding"
)

const (
	// BlockHeaderSize is the size, in bytes, of a block header.
	// 32 (ParentID) + 8 (Nonce) + 8 (Timestamp) + 32 (MerkleRoot)
	BlockHeaderSize = 80
)

type (
	// A Block is a summary of changes to the state that have occurred since the
	// previous block. Blocks reference the ID of the previous block (their
	// "parent"), creating the linked-list commonly known as the blockchain. Their
	// primary function is to bundle together transactions on the network. Blocks
	// are created by "miners," who collect transactions from other nodes, and
	// then try to pick a Nonce that results in a block whose BlockID is below a
	// given Target.
	Block struct {
		ParentID     BlockID         `json:"parentid"`
		Nonce        BlockNonce      `json:"nonce"`
		Timestamp    Timestamp       `json:"timestamp"`
		MinerPayouts []UplocoinOutput `json:"minerpayouts"`
		Transactions []Transaction   `json:"transactions"`
	}

	// A BlockHeader contains the data that, when hashed, produces the Block's ID.
	BlockHeader struct {
		ParentID   BlockID     `json:"parentid"`
		Nonce      BlockNonce  `json:"nonce"`
		Timestamp  Timestamp   `json:"timestamp"`
		MerkleRoot crypto.Hash `json:"merkleroot"`
	}

	// BlockHeight is the number of blocks that exist after the genesis block.
	BlockHeight uint64
	// A BlockID is the hash of a BlockHeader. A BlockID uniquely
	// identifies a Block, and indicates the amount of work performed
	// to mine that Block. The more leading zeros in the BlockID, the
	// more work was performed.
	BlockID crypto.Hash
	// The BlockNonce is a "scratch space" that miners can freely alter to produce
	// a BlockID that satisfies a given Target.
	BlockNonce [8]byte
)

// CalculateCoinbase calculates the coinbase for a given height. The coinbase
// equation is:
//
//     coinbase := max(InitialCoinbase - height, MinimumCoinbase) * UplocoinPrecision
func CalculateCoinbase(height BlockHeight) Currency {
	base := InitialCoinbase - uint64(height)
	if uint64(height) > InitialCoinbase || base < MinimumCoinbase {
		base = MinimumCoinbase
	}
	return NewCurrency64(base).Mul(UplocoinPrecision)
}

// CalculateNumUplocoins calculates the number of Uplocoins in circulation at a
// given height.
func CalculateNumUplocoins(height BlockHeight) (total Currency) {
	total = numGenesisUplocoins
	deflationBlocks := BlockHeight(InitialCoinbase - MinimumCoinbase)
	avgDeflationUplocoins := CalculateCoinbase(0).Add(CalculateCoinbase(height)).Div64(2)
	if height <= deflationBlocks {
		total = total.Add(avgDeflationUplocoins.Mul64(uint64(height + 1)))
	} else {
		total = total.Add(avgDeflationUplocoins.Mul(NewCurrency64(uint64(deflationBlocks + 1))))
		total = total.Add(CalculateCoinbase(height).Mul64(uint64(height - deflationBlocks)))
	}
	if height >= FoundationHardforkHeight {
		total = total.Add(InitialFoundationSubsidy)
		perSubsidy := FoundationSubsidyPerBlock.Mul64(uint64(FoundationSubsidyFrequency))
		subsidies := (height - FoundationHardforkHeight) / FoundationSubsidyFrequency
		total = total.Add(perSubsidy.Mul64(uint64(subsidies)))
	}
	return
}

var numGenesisUplocoins = func() Currency {
	// Sum all the values for the genesis Uplocoin outputs.
	numGenesisUplocoins := NewCurrency64(0)
	for _, transaction := range GenesisBlock.Transactions {
		for _, UplocoinOutput := range transaction.UplocoinOutputs {
			numGenesisUplocoins = numGenesisUplocoins.Add(UplocoinOutput.Value)
		}
	}
	return numGenesisUplocoins
}()

// ID returns the ID of a Block, which is calculated by hashing the header.
func (h BlockHeader) ID() BlockID {
	return BlockID(crypto.HashObject(h))
}

// CalculateSubsidy takes a block and a height and determines the block
// subsidy.
func (b Block) CalculateSubsidy(height BlockHeight) Currency {
	subsidy := CalculateCoinbase(height)
	for _, txn := range b.Transactions {
		for _, fee := range txn.MinerFees {
			subsidy = subsidy.Add(fee)
		}
	}
	return subsidy
}

// Header returns the header of a block.
func (b Block) Header() BlockHeader {
	return BlockHeader{
		ParentID:   b.ParentID,
		Nonce:      b.Nonce,
		Timestamp:  b.Timestamp,
		MerkleRoot: b.MerkleRoot(),
	}
}

// ID returns the ID of a Block, which is calculated by hashing the
// concatenation of the block's parent's ID, nonce, and the result of the
// b.MerkleRoot(). It is equivalent to calling block.Header().ID()
func (b Block) ID() BlockID {
	return b.Header().ID()
}

// MerkleRoot calculates the Merkle root of a Block. The leaves of the Merkle
// tree are composed of the miner outputs (one leaf per payout), and the
// transactions (one leaf per transaction).
func (b Block) MerkleRoot() crypto.Hash {
	tree := crypto.NewTree()
	var buf bytes.Buffer
	e := encoding.NewEncoder(&buf)
	for _, payout := range b.MinerPayouts {
		payout.MarshalUplo(e)
		tree.Push(buf.Bytes())
		buf.Reset()
	}
	for _, txn := range b.Transactions {
		txn.MarshalUplo(e)
		tree.Push(buf.Bytes())
		buf.Reset()
	}
	return tree.Root()
}

// MinerPayoutID returns the ID of the miner payout at the given index, which
// is calculated by hashing the concatenation of the BlockID and the payout
// index.
func (b Block) MinerPayoutID(i uint64) UplocoinOutputID {
	return UplocoinOutputID(crypto.HashAll(
		b.ID(),
		i,
	))
}

// FoundationSubsidyID returns the ID of the Foundation subsidy, which is
// calculated by hashing the concatenation of the BlockID and
// SpecifierFoundation.
func (bid BlockID) FoundationSubsidyID() UplocoinOutputID {
	return UplocoinOutputID(crypto.HashAll(bid, SpecifierFoundation))
}
