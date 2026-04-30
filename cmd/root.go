// Package cmd hosts every Cobra subcommand for the openvlm CLI.
//
// Every verb (list, identify, read, dump, write, update, provision) lives in
// its own file in this package and registers itself onto rootCmd via init().
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Build-time identity. Set via -ldflags "-X github.com/openmanet/openvlm/cmd.Version=…"
// in goreleaser; the in-repo defaults are used for `go build` from a checkout.
//
//nolint:gochecknoglobals // standard pattern for cobra/goreleaser version injection
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Persistent flag values shared by every subcommand. Set during command
// execution by Cobra's flag parser; read by the device-open helper in
// device.go.
//
//nolint:gochecknoglobals // package-level storage is the cobra idiom for persistent flags
var (
	flagSerial  string
	flagVerbose bool
)

//nolint:gochecknoglobals // rootCmd is the canonical cobra entry point
var rootCmd = &cobra.Command{
	Use:           "openvlm",
	Short:         shortRoot,
	Long:          longRoot,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits the process with the appropriate
// status code. Called from main().
func Execute() {
	rootCmd.Version = formatVersion()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, friendlyError(err, flagVerbose))
		os.Exit(exitCodeFor(err))
	}
}

func init() { //nolint:gochecknoinits // cobra commands self-register here by convention
	rootCmd.PersistentFlags().StringVar(&flagSerial, "serial", "", flagSerialHelp)
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, flagVerboseHelp)
}

func formatVersion() string {
	if Commit == "" && Date == "" {
		return Version
	}

	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
