package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/uplo-tech/uplo/build"
)

// TestRootuplocCmd tests root uploc command for expected outputs. The test
// runs its own node and requires no service running at port 5555.
func TestRootuplocCmd(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Create a test node for this test group
	groupDir := uplocTestDir(t.Name())
	n, err := newTestNode(groupDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := n.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Initialize uploc root command with its subcommands and flags
	root := getRootCmdForuplocCmdsTests(groupDir)

	// define test constants:
	// Regular expressions to check uploc output

	begin := "^"
	nl := `
` // platform agnostic new line
	end := "$"

	// Capture root command usage for test comparison
	// catch stdout and stderr
	rootCmdUsagePattern := getCmdUsage(t, root)

	IPv6addr := n.Address
	IPv4Addr := strings.ReplaceAll(n.Address, "[::]", "localhost")

	rootCmdOutPattern := `Consensus:
  Synced: (No|Yes)
  Height: \d+

Wallet:
(  Status: Locked|  Status:          unlocked
  Uplocoin Balance: \d+(\.\d*|) (SC|KS|MS))

Renter:
  Files:                   \d+
  Total Stored:            \d+(\.\d+|) ( B|kB|MB|GB|TB)
  Total Renewing Data:     \d+(\.\d+|) ( B|kB|MB|GB|TB)
  Repair Data Remaining:   \d+(\.\d+|) ( B|kB|MB|GB|TB)
  Stuck Repair Remaining:  \d+(\.\d+|) ( B|kB|MB|GB|TB)
  Min Redundancy:          (\d+.\d{2}|-)
  Active Contracts:        \d+
  Passive Contracts:       \d+
  Disabled Contracts:      \d+`

	rootCmdVerbosePartPattern := `Global Rate limits: 
  Download Speed: (no limit|\d+(\.\d+)? (B/s|KB/s|MB/s|GB/s|TB/s))
  Upload Speed:   (no limit|\d+(\.\d+)? (B/s|KB/s|MB/s|GB/s|TB/s))

Gateway Rate limits: 
  Download Speed: (no limit|\d+(\.\d+)? (B/s|KB/s|MB/s|GB/s|TB/s))
  Upload Speed:   (no limit|\d+(\.\d+)? (B/s|KB/s|MB/s|GB/s|TB/s))

Renter Rate limits: 
  Download Speed: (no limit|\d+(\.\d+)? (B/s|KB/s|MB/s|GB/s|TB/s))
  Upload Speed:   (no limit|\d+(\.\d+)? (B/s|KB/s|MB/s|GB/s|TB/s))`

	connectionRefusedPattern := `Could not get consensus status: \[failed to get reader response; GET request failed; Get "?http://localhost:5555/consensus"?: dial tcp \[::1\]:5555: connect: connection refused\]`
	uploclientVersionPattern := "Uplo Client v" + strings.ReplaceAll(build.Version, ".", `\.`)

	// Define subtests
	// We can't test uplod on default address (port) when test node has
	// dynamically allocated port, we have to use node address.
	subTests := []uplocCmdSubTest{
		{
			name:               "TestRootCmdWithShortAddressFlagIPv6",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"-a", IPv6addr},
			expectedOutPattern: begin + rootCmdOutPattern + nl + nl + end,
		},
		{
			name:               "TestRootCmdWithShortAddressFlagIPv4",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"-a", IPv4Addr},
			expectedOutPattern: begin + rootCmdOutPattern + nl + nl + end,
		},
		{
			name:               "TestRootCmdWithLongAddressFlagIPv6",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"--addr", IPv6addr},
			expectedOutPattern: begin + rootCmdOutPattern + nl + nl + end,
		},
		{
			name:               "TestRootCmdWithLongAddressFlagIPv4",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"--addr", IPv4Addr},
			expectedOutPattern: begin + rootCmdOutPattern + nl + nl + end,
		},
		{
			name:               "TestRootCmdWithVerboseFlag",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"--addr", IPv4Addr, "-v"},
			expectedOutPattern: begin + rootCmdOutPattern + nl + nl + rootCmdVerbosePartPattern + nl + nl + end,
		},
		{
			name:               "TestRootCmdWithInvalidFlag",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"-x"},
			expectedOutPattern: begin + "Error: unknown shorthand flag: 'x' in -x" + nl + rootCmdUsagePattern + nl + end,
		},
		{
			name:               "TestRootCmdWithInvalidAddress",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"-a", "localhost:5555"},
			expectedOutPattern: begin + connectionRefusedPattern + nl + nl + end,
		},
		{
			name:               "TestRootCmdWithHelpFlag",
			test:               testGenericuplocCmd,
			cmd:                root,
			cmdStrs:            []string{"-h"},
			expectedOutPattern: begin + uploclientVersionPattern + nl + nl + rootCmdUsagePattern + end,
		},
	}

	// run tests
	err = runuplocCmdSubTests(t, subTests)
	if err != nil {
		t.Fatal(err)
	}
}

// getCmdUsage gets root command usage regex pattern by calling usage function
func getCmdUsage(t *testing.T, cmd *cobra.Command) string {
	// Capture usage by calling a usage function
	c, err := newOutputCatcher()
	if err != nil {
		t.Fatal("Error starting catching stdout/stderr", err)
	}
	usageFunc := cmd.UsageFunc()
	err = usageFunc(cmd)
	if err != nil {
		t.Fatal("Error getting reference root uploc usage", err)
	}
	baseUsage, err := c.stop()

	// Escape regex special chars
	usage := escapeRegexChars(baseUsage)

	// Inject 2 missing rows
	beforeHelpCommand := "Perform gateway actions"
	helpCommand := "  help        Help about any command"
	nl := `
`
	usage = strings.ReplaceAll(usage, beforeHelpCommand, beforeHelpCommand+nl+helpCommand)
	beforeHelpFlag := "the password for the API's http authentication"
	helpFlag := `  -h, --help                   help for .*uploc(\.test|)`
	cmdUsagePattern := strings.ReplaceAll(usage, beforeHelpFlag, beforeHelpFlag+nl+helpFlag)

	return cmdUsagePattern
}
