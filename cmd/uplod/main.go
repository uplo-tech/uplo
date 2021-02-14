package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/uplo-tech/uplo/build"
)

var (
	// globalConfig is used by the cobra package to fill out the configuration
	// variables.
	globalConfig Config
)

// exit codes
// inspired by sysexits.h
const (
	exitCodeGeneral = 1  // Not in sysexits.h, but is standard practice.
	exitCodeUsage   = 64 // EX_USAGE in sysexits.h
)

// The Config struct contains all configurable variables for uplod. It is
// compatible with gcfg.
type Config struct {
	// The APIPassword is input by the user after the daemon starts up, if the
	// --authenticate-api flag is set.
	APIPassword string

	// The uplod variables are referenced directly by cobra, and are set
	// according to the flags.
	uplod struct {
		APIaddr       string
		RPCaddr       string
		HostAddr      string
		UploMuxTCPAddr string
		UploMuxWSAddr  string
		AllowAPIBind  bool

		Modules           string
		NoBootstrap       bool
		RequiredUserAgent string
		AuthenticateAPI   bool
		TempPassword      bool

		Profile    string
		ProfileDir string

		// NOTE: uplodir in this case is referencing the directory that uplod is
		// going to be running out of, not the actual uplodir, which is where we
		// put the apipassword file. This variable should not be altered if it
		// is not set by a user flag.
		uplodir string
	}
}

// die prints its arguments to stderr, then exits the program with the default
// error code.
func die(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(exitCodeGeneral)
}

// versionCmd is a cobra command that prints the version of uplod.
func versionCmd(*cobra.Command, []string) {
	version := build.Version
	if build.ReleaseTag != "" {
		version += "-" + build.ReleaseTag
	}
	switch build.Release {
	case "dev":
		fmt.Println("Uplo Daemon v" + version + "-dev")
	case "standard":
		fmt.Println("Uplo Daemon v" + version)
	case "testing":
		fmt.Println("Uplo Daemon v" + version + "-testing")
	default:
		fmt.Println("Uplo Daemon v" + version + "-???")
	}
}

// modulesCmd is a cobra command that prints help info about modules.
func modulesCmd(*cobra.Command, []string) {
	fmt.Println(`Use the -M or --modules flag to only run specific modules. Modules are
independent components of Uplo. This flag should only be used by developers or
people who want to reduce overhead from unused modules. Modules are specified by
their first letter. If the -M or --modules flag is not specified the default
modules are run. The default modules are:
	gateway, consensus set, host, miner, renter, transaction pool, wallet
This is equivalent to:
	uplod -M cghmrtw
Below is a list of all the modules available.

Gateway (g):
	The gateway maintains a peer to peer connection to the network and
	enables other modules to perform RPC calls on peers.
	The gateway is required by all other modules.
	Example:
		uplod -M g
Consensus Set (c):
	The consensus set manages everything related to consensus and keeps the
	blockchain in sync with the rest of the network.
	The consensus set requires the gateway.
	Example:
		uplod -M gc
Transaction Pool (t):
	The transaction pool manages unconfirmed transactions.
	The transaction pool requires the consensus set.
	Example:
		uplod -M gct
Wallet (w):
	The wallet stores and manages Uplocoins and uplofunds.
	The wallet requires the consensus set and transaction pool.
	Example:
		uplod -M gctw
Renter (r):
	The renter manages the user's files on the network.
	The renter requires the consensus set, transaction pool, and wallet.
	Example:
		uplod -M gctwr
Host (h):
	The host provides storage from local disks to the network. The host
	negotiates file contracts with remote renters to earn money for storing
	other users' files.
	The host requires the consensus set, transaction pool, and wallet.
	Example:
		uplod -M gctwh
Miner (m):
	The miner provides a basic CPU mining implementation as well as an API
	for external miners to use.
	The miner requires the consensus set, transaction pool, and wallet.
	Example:
		uplod -M gctwm
FeeManager (f):
	The FeeManager provides a means for application developers to charge
	users for the user of their application.
	The FeeManager requires the consensus set, gateway, transaction pool, and wallet.
	Example:
		uplod -M gctwf
Explorer (e):
	The explorer provides statistics about the blockchain and can be
	queried for information about specific transactions or other objects on
	the blockchain.
	The explorer requires the consenus set.
	Example:
		uplod -M gce`)
}

// main establishes a set of commands and flags using the cobra package.
func main() {
	if build.DEBUG {
		fmt.Println("Running with debugging enabled")
	}
	root := &cobra.Command{
		Use:   os.Args[0],
		Short: "Uplo Daemon v" + build.Version,
		Long:  "Uplo Daemon v" + build.Version,
		Run:   startDaemonCmd,
	}

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print version information about the Uplo Daemon",
		Run:   versionCmd,
	})

	root.AddCommand(&cobra.Command{
		Use:   "modules",
		Short: "List available modules for use with -M, --modules flag",
		Long:  "List available modules for use with -M, --modules flag and their uses",
		Run:   modulesCmd,
	})

	// Set default values, which have the lowest priority.
	root.Flags().StringVarP(&globalConfig.uplod.RequiredUserAgent, "agent", "", "Uplo-Agent", "required substring for the user agent")
	root.Flags().StringVarP(&globalConfig.uplod.HostAddr, "host-addr", "", ":8482", "which port the host listens on")
	root.Flags().StringVarP(&globalConfig.uplod.ProfileDir, "profile-directory", "", "profiles", "location of the profiling directory")
	root.Flags().StringVarP(&globalConfig.uplod.APIaddr, "api-addr", "", "localhost:8480", "which host:port the API server listens on")
	root.Flags().StringVarP(&globalConfig.uplod.uplodir, "uplo-directory", "d", "", "location of the uplo directory")
	root.Flags().BoolVarP(&globalConfig.uplod.NoBootstrap, "no-bootstrap", "", false, "disable bootstrapping on this run")
	root.Flags().StringVarP(&globalConfig.uplod.Profile, "profile", "", "", "enable profiling with flags 'cmt' for CPU, memory, trace")
	root.Flags().StringVarP(&globalConfig.uplod.RPCaddr, "rpc-addr", "", ":8481", "which port the gateway listens on")
	root.Flags().StringVarP(&globalConfig.uplod.UploMuxTCPAddr, "uplomux-addr", "", ":9983", "which port the UploMux listens on")
	root.Flags().StringVarP(&globalConfig.uplod.UploMuxWSAddr, "uplomux-addr-ws", "", ":9984", "which port the UploMux websocket listens on")
	root.Flags().StringVarP(&globalConfig.uplod.Modules, "modules", "M", "cghrtwf", "enabled modules, see 'uplod modules' for more info")
	root.Flags().BoolVarP(&globalConfig.uplod.AuthenticateAPI, "authenticate-api", "", true, "enable API password protection")
	root.Flags().BoolVarP(&globalConfig.uplod.TempPassword, "temp-password", "", false, "enter a temporary API password during startup")
	root.Flags().BoolVarP(&globalConfig.uplod.AllowAPIBind, "disable-api-security", "", false, "allow uplod to listen on a non-localhost address (DANGEROUS)")

	// If globalConfig.uplod.uplodir is not set, use the environment variable provided.
	if globalConfig.uplod.uplodir == "" {
		globalConfig.uplod.uplodir = build.uplodDataDir()
	}

	// Parse cmdline flags, overwriting both the default values and the config
	// file values.
	if err := root.Execute(); err != nil {
		// Since no commands return errors (all commands set Command.Run instead of
		// Command.RunE), Command.Execute() should only return an error on an
		// invalid command or flag. Therefore Command.Usage() was called (assuming
		// Command.SilenceUsage is false) and we should exit with exitCodeUsage.
		os.Exit(exitCodeUsage)
	}
}
