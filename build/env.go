package build

var (
	// uploAPIPassword is the environment variable that sets a custom API
	// password if the default is not used
	uploAPIPassword = "UPLO_API_PASSWORD"

	// uplodataDir is the environment variable that tells uplod where to put the
	// general uplo data, e.g. api password, configuration, logs, etc.
	uplodataDir = "UPLO_DATA_DIR"

	// uplodDataDir is the environment variable which tells uplod where to put the
	// uplod-specific data
	uplodDataDir = "uplod_DATA_DIR"

	// uploWalletPassword is the environment variable that can be set to enable
	// auto unlocking the wallet
	uploWalletPassword = "UPLO_WALLET_PASSWORD"

	// uploExchangeRate is the environment variable that can be set to
	// show amounts (additionally) in a different currency
	uploExchangeRate = "UPLO_EXCHANGE_RATE"
)
