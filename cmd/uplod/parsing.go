package main

import (
	"strings"

	"github.com/uplo-tech/uplo/node"
)

// createNodeParams parses the provided config and creates the corresponding
// node params for the server.
func parseModules(config Config) node.NodeParams {
	params := node.NodeParams{}
	// Parse the modules.
	if strings.Contains(config.uplod.Modules, "g") {
		params.CreateGateway = true
	}
	if strings.Contains(config.uplod.Modules, "c") {
		params.CreateConsensusSet = true
	}
	if strings.Contains(config.uplod.Modules, "e") {
		params.CreateExplorer = true
	}
	if strings.Contains(config.uplod.Modules, "f") {
		params.CreateFeeManager = true
	}
	if strings.Contains(config.uplod.Modules, "t") {
		params.CreateTransactionPool = true
	}
	if strings.Contains(config.uplod.Modules, "w") {
		params.CreateWallet = true
	}
	if strings.Contains(config.uplod.Modules, "m") {
		params.CreateMiner = true
	}
	if strings.Contains(config.uplod.Modules, "h") {
		params.CreateHost = true
	}
	if strings.Contains(config.uplod.Modules, "r") {
		params.CreateRenter = true
	}
	// Parse remaining fields.
	params.Bootstrap = !config.uplod.NoBootstrap
	params.HostAddress = config.uplod.HostAddr
	params.RPCAddress = config.uplod.RPCaddr
	params.UploMuxTCPAddress = config.uplod.UploMuxTCPAddr
	params.UploMuxWSAddress = config.uplod.UploMuxWSAddr
	params.Dir = config.uplod.uplodir
	return params
}
