package node

// templates.go contains a bunch of sane default templates that you can use to
// create Uplo nodes.

var (
	// AllModulesTemplate is a template for a Uplo node that has all modules
	// enabled.
	AllModulesTemplate = NodeParams{
		CreateConsensusSet:    true,
		CreateExplorer:        false, // TODO: Implement explorer.
		CreateFeeManager:      true,
		CreateGateway:         true,
		CreateHost:            true,
		CreateMiner:           true,
		CreateRenter:          true,
		CreateTransactionPool: true,
		CreateWallet:          true,
	}
	// FeeManagerTemplate is a template for a Uplo node that has a functioning
	// FeeManager. The node has a FeeManager and all dependencies, but no other
	// modules.
	FeeManagerTemplate = NodeParams{
		CreateConsensusSet:    true,
		CreateExplorer:        false,
		CreateFeeManager:      true,
		CreateGateway:         true,
		CreateHost:            false,
		CreateMiner:           false,
		CreateRenter:          false,
		CreateTransactionPool: true,
		CreateWallet:          true,
	}
	// GatewayTemplate is a template for a Uplo node that has a functioning
	// Gateway. The node has a Gateway but no other modules.
	GatewayTemplate = NodeParams{
		CreateConsensusSet:    false,
		CreateExplorer:        false,
		CreateFeeManager:      false,
		CreateGateway:         true,
		CreateHost:            false,
		CreateMiner:           false,
		CreateRenter:          false,
		CreateTransactionPool: false,
		CreateWallet:          false,
	}
	// HostTemplate is a template for a Uplo node that has a functioning host.
	// The node has a host and all dependencies, but no other modules.
	HostTemplate = NodeParams{
		CreateConsensusSet:    true,
		CreateExplorer:        false,
		CreateFeeManager:      false,
		CreateGateway:         true,
		CreateHost:            true,
		CreateMiner:           false,
		CreateRenter:          false,
		CreateTransactionPool: true,
		CreateWallet:          true,
	}
	// MinerTemplate is a template for a Uplo node that has a functioning miner.
	// The node has a miner and all dependencies, but no other modules.
	MinerTemplate = NodeParams{
		CreateConsensusSet:    true,
		CreateExplorer:        false,
		CreateFeeManager:      false,
		CreateGateway:         true,
		CreateHost:            false,
		CreateMiner:           true,
		CreateRenter:          false,
		CreateTransactionPool: true,
		CreateWallet:          true,
	}
	// RenterTemplate is a template for a Uplo node that has a functioning
	// renter. The node has a renter and all dependencies, but no other
	// modules.
	RenterTemplate = NodeParams{
		CreateConsensusSet:    true,
		CreateExplorer:        false,
		CreateFeeManager:      false,
		CreateGateway:         true,
		CreateHost:            false,
		CreateMiner:           false,
		CreateRenter:          true,
		CreateTransactionPool: true,
		CreateWallet:          true,
	}
	// WalletTemplate is a template for a Uplo node that has a functioning
	// wallet. The node has a wallet and all dependencies, but no other
	// modules.
	WalletTemplate = NodeParams{
		CreateConsensusSet:    true,
		CreateExplorer:        false,
		CreateFeeManager:      false,
		CreateGateway:         true,
		CreateHost:            false,
		CreateMiner:           false,
		CreateRenter:          false,
		CreateTransactionPool: true,
		CreateWallet:          true,
	}
)

// AllModules returns an AllModulesTemplate filled out with the provided dir.
func AllModules(dir string) NodeParams {
	template := AllModulesTemplate
	template.Dir = dir
	return template
}

// FeeManager returns a FeeManagerTemplate filled out with the provided dir.
func FeeManager(dir string) NodeParams {
	template := FeeManagerTemplate
	template.Dir = dir
	return template
}

// Gateway returns a GatewayTemplate filled out with the provided dir.
func Gateway(dir string) NodeParams {
	template := GatewayTemplate
	template.Dir = dir
	return template
}

// Host returns a HostTemplate filled out with the provided dir.
func Host(dir string) NodeParams {
	template := HostTemplate
	template.Dir = dir
	return template
}

// Miner returns a MinerTemplate filled out with the provided dir.
func Miner(dir string) NodeParams {
	template := MinerTemplate
	template.Dir = dir
	return template
}

// Renter returns a RenterTemplate filled out with the provided dir.
func Renter(dir string) NodeParams {
	template := RenterTemplate
	template.Dir = dir
	return template
}

// Wallet returns a WalletTemplate filled out with the provided dir.
func Wallet(dir string) NodeParams {
	template := WalletTemplate
	template.Dir = dir
	return template
}
