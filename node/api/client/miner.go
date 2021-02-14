package client

import (
	"github.com/uplo-tech/uplo/node/api"
	"github.com/uplo-tech/uplo/types"
	"github.com/uplo-tech/encoding"
)

// MinerGet requests the /miner endpoint's resources.
func (c *Client) MinerGet() (mg api.MinerGET, err error) {
	err = c.get("/miner", &mg)
	return
}

// MinerBlockPost uses the /miner/block endpoint to submit a solved block.
func (c *Client) MinerBlockPost(b types.Block) (err error) {
	err = c.post("/miner/block", string(encoding.Marshal(b)), nil)
	return
}

// MinerHeaderGet uses the /miner/header endpoint to get a header for work.
func (c *Client) MinerHeaderGet() (target types.Target, bh types.BlockHeader, err error) {
	_, targetAndHeader, err := c.getRawResponse("/miner/header")
	if err != nil {
		return types.Target{}, types.BlockHeader{}, err
	}
	err = encoding.UnmarshalAll(targetAndHeader, &target, &bh)
	return
}

// MinerHeaderPost uses the /miner/header endpoint to submit a solved block
// header that was previously received from the same endpoint
func (c *Client) MinerHeaderPost(bh types.BlockHeader) (err error) {
	err = c.post("/miner/header", string(encoding.Marshal(bh)), nil)
	return
}

// MinerStartGet uses the /miner/start endpoint to start the cpu miner.
func (c *Client) MinerStartGet() (err error) {
	err = c.get("/miner/start", nil)
	return
}

// MinerStopGet uses the /miner/stop endpoint to stop the cpu miner.
func (c *Client) MinerStopGet() (err error) {
	err = c.get("/miner/stop", nil)
	return
}
