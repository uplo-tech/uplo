package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/uplo-tech/uplo/build"
	"github.com/uplo-tech/uplo/modules"
	"github.com/uplo-tech/errors"
)

var (
	alertsCmd = &cobra.Command{
		Use:   "alerts",
		Short: "view daemon alerts",
		Long:  "view daemon alerts",
		Run:   wrap(alertscmd),
	}

	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the Uplo daemon",
		Long:  "Stop the Uplo daemon.",
		Run:   wrap(stopcmd),
	}

	updateCheckCmd = &cobra.Command{
		Use:   "check",
		Short: "Check for available updates",
		Long:  "Check for available updates.",
		Run:   wrap(updatecheckcmd),
	}

	globalRatelimitCmd = &cobra.Command{
		Use:   "ratelimit [maxdownloadspeed] [maxuploadspeed]",
		Short: "set the global maxdownloadspeed and maxuploadspeed",
		Long: `Set the global maxdownloadspeed and maxuploadspeed in
Bytes per second: B/s, KB/s, MB/s, GB/s, TB/s
or
Bits per second: Bps, Kbps, Mbps, Gbps, Tbps
Set them to 0 for no limit.`,
		Run: wrap(globalratelimitcmd),
	}

	profileCmd = &cobra.Command{
		Use:   "profile",
		Short: "Start and stop profiles for the daemon",
		Long:  "Start and stop CPU, memory, and/or trace profiles for the daemon",
		Run:   profilecmd,
	}

	profileStartCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the profile for the daemon",
		Long: `Start a CPU, memory, and/or trace profile for the daemon by using
the corresponding flag.  Provide a profileDir to save the profiles to.  If no
profileDir is provided the profiles will be saved in the default profile
directory in the uplod data directory.`,
		Run: wrap(profilestartcmd),
	}

	profileStopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop profiles for the daemon",
		Long:  "Stop profiles for the daemon",
		Run:   wrap(profilestopcmd),
	}

	stackCmd = &cobra.Command{
		Use:   "stack",
		Short: "Get current stack trace for the daemon",
		Long:  "Get current stack trace for the daemon",
		Run:   wrap(stackcmd),
	}

	updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Update Uplo",
		Long:  "Check for (and/or download) available updates for Uplo.",
		Run:   wrap(updatecmd),
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print version information.",
		Run:   wrap(versioncmd),
	}
)

// alertscmd prints the alerts from the daemon. This will not print critical
// alerts as critical alerts are printed on every uploc command
func alertscmd() {
	al, err := httpClient.DaemonAlertsGet()
	if err != nil {
		fmt.Println("Could not get daemon alerts:", err)
		return
	}
	if len(al.Alerts) == 0 {
		fmt.Println("There are no alerts registered.")
		return
	}
	if len(al.Alerts) == len(al.CriticalAlerts) {
		// Return since critical alerts are already displayed
		return
	}

	// Print Error alerts
	const maxAlerts = 1000
	remainingAlerts := maxAlerts - len(al.CriticalAlerts)
	if remainingAlerts <= 0 {
		fmt.Println("Only first", maxAlerts, "alerts printed")
		return
	}
	alertsToPrint := remainingAlerts
	if alertsToPrint > len(al.ErrorAlerts) {
		alertsToPrint = len(al.ErrorAlerts)
	}
	printAlerts(al.ErrorAlerts[:alertsToPrint], modules.SeverityError)

	// Print Warning alerts
	remainingAlerts -= len(al.ErrorAlerts)
	if remainingAlerts <= 0 {
		fmt.Println("Only first", maxAlerts, "alerts printed")
		return
	}
	alertsToPrint = remainingAlerts
	if alertsToPrint > len(al.WarningAlerts) {
		alertsToPrint = len(al.WarningAlerts)
	}
	printAlerts(al.WarningAlerts[:alertsToPrint], modules.SeverityWarning)

	// Print max alerts message
	if len(al.CriticalAlerts)+len(al.ErrorAlerts)+len(al.WarningAlerts) > maxAlerts {
		fmt.Println("Only first", maxAlerts, "alerts printed")
	}
}

// profilecmd displays the usage info for the command.
func profilecmd(cmd *cobra.Command, args []string) {
	_ = cmd.UsageFunc()(cmd)
	os.Exit(exitCodeUsage)
}

// profilestartcmd starts the profile for the daemon.
func profilestartcmd() {
	var profileFlags string
	if daemonCPUProfile {
		profileFlags += "c"
	}
	if daemonMemoryProfile {
		profileFlags += "m"
	}
	if daemonTraceProfile {
		profileFlags += "t"
	}
	if profileFlags == "" {
		die("no profiles submitted")
	}
	err := httpClient.DaemonStartProfilePost(profileFlags, daemonProfileDirectory)
	if err != nil {
		die(err)
	}
	fmt.Println("Profile Started!")
}

// profilestopcmd stops the profile for the daemon.
func profilestopcmd() {
	err := httpClient.DaemonStopProfilePost()
	if err != nil {
		die(err)
	}
	fmt.Println("Profile Stopped")
}

// version prints the version of uploc and uplod.
func versioncmd() {
	fmt.Println("Uplo Client")
	if build.ReleaseTag == "" {
		fmt.Println("\tVersion " + build.Version)
	} else {
		fmt.Println("\tVersion " + build.Version + "-" + build.ReleaseTag)
	}
	if build.GitRevision != "" {
		fmt.Println("\tGit Revision " + build.GitRevision)
		fmt.Println("\tBuild Time   " + build.BuildTime)
	}
	dvg, err := httpClient.DaemonVersionGet()
	if err != nil {
		fmt.Println("Could not get daemon version:", err)
		return
	}
	fmt.Println("Uplo Daemon")
	fmt.Println("\tVersion " + dvg.Version)
	if build.GitRevision != "" {
		fmt.Println("\tGit Revision " + dvg.GitRevision)
		fmt.Println("\tBuild Time   " + dvg.BuildTime)
	}
}

// stopcmd is the handler for the command `uploc stop`.
// Stops the daemon.
func stopcmd() {
	err := httpClient.DaemonStopGet()
	if err != nil {
		die("Could not stop daemon:", err)
	}
	fmt.Println("Uplo daemon stopped.")
}

// stackcmd is the handler for the command `uploc stack` and writes the current
// stack trace to an output file.
func stackcmd() {
	// Get the stack trace
	dsg, err := httpClient.DaemonStackGet()
	if err != nil {
		die("Could not get the stack:", err)
	}
	fmt.Println(dsg.Stack)
	// Create output file
	f, err := os.Create(daemonStackOutputFile)
	if err != nil {
		die("Unable to create output file:", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			die(err)
		}
	}()

	// Write stack trace to output file
	_, err = f.Write([]byte(dsg.Stack))
	if err != nil {
		die("Unable to write to output file:", err)
	}

	fmt.Println("Current stack trace written to:", daemonStackOutputFile)
}

// updatecmd is the handler for the command `uploc update`.
// Updates the daemon version to latest general release.
func updatecmd() {
	update, err := httpClient.DaemonUpdateGet()
	if err != nil {
		fmt.Println("Could not check for update:", err)
		return
	}
	if !update.Available {
		fmt.Println("Already up to date.")
		return
	}

	err = httpClient.DaemonUpdatePost()
	if err != nil {
		fmt.Println("Could not apply update:", err)
		return
	}
	fmt.Printf("Updated to version %s! Restart uplod now.\n", update.Version)
}

// updatecheckcmd is the handler for the command `uploc check`.
// Checks is there is an newer daemon version available.
func updatecheckcmd() {
	update, err := httpClient.DaemonUpdateGet()
	if err != nil {
		fmt.Println("Could not check for update:", err)
		return
	}
	if update.Available {
		fmt.Printf("A new release (v%s) is available! Run 'uploc update' to install it.\n", update.Version)
	} else {
		fmt.Println("Up to date.")
	}
}

// globalratelimitcmd is the handler for the command `uploc ratelimit`.
// Sets the global maxuploadspeed and maxdownloadspeed the daemon can use.
func globalratelimitcmd(downloadSpeedStr, uploadSpeedStr string) {
	downloadSpeedInt, err := parseRatelimit(downloadSpeedStr)
	if err != nil {
		die(errors.AddContext(err, "unable to parse download speed"))
	}
	uploadSpeedInt, err := parseRatelimit(uploadSpeedStr)
	if err != nil {
		die(errors.AddContext(err, "unable to parse upload speed"))
	}
	err = httpClient.DaemonGlobalRateLimitPost(downloadSpeedInt, uploadSpeedInt)
	if err != nil {
		die("Could not set global ratelimit speed:", err)
	}
	fmt.Println("Set global maxdownloadspeed to ", downloadSpeedInt, " and maxuploadspeed to ", uploadSpeedInt)
}

// printAlerts is a helper function to print details of a slice of alerts
// with given severity description to command line
func printAlerts(alerts []modules.Alert, as modules.AlertSeverity) {
	fmt.Printf("\n  There are %v %s alerts\n", len(alerts), as.String())
	for _, a := range alerts {
		fmt.Printf(`
------------------
  Module:   %s
  Severity: %s
  Message:  %s
  Cause:    %s`, a.Module, a.Severity.String(), a.Msg, a.Cause)
	}
	fmt.Printf("\n------------------\n\n")
}
