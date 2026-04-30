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
	Use:   "openvlm",
	Short: "Read, write, and validate the EEPROM on OpenVLM USB dongles",
	Long: `openvlm is a cross-platform CLI for the OpenVLM USB audio dongle
(C-Media CM108B with a GPIO1 hardware strap).

It can:
  - enumerate attached OpenVLM devices                 (openvlm list)
  - confirm a device is OpenVLM-strapped               (openvlm identify)
  - dump the live EEPROM as bytes, YAML, or hex        (openvlm read | dump)
  - program the EEPROM from a file or YAML overrides   (openvlm write)
  - change a single EEPROM field                       (openvlm update)
  - apply the compiled-in factory defaults             (openvlm provision)

Run 'openvlm <verb> --help' for per-verb usage.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits the process with the appropriate
// status code. Called from main().
func Execute() {
	rootCmd.Version = formatVersion()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "openvlm:", err)
		os.Exit(exitCodeFor(err))
	}
}

func init() { //nolint:gochecknoinits // cobra commands self-register here by convention
	rootCmd.PersistentFlags().StringVar(&flagSerial, "serial", "",
		"select the device whose USB serial-number string matches this value")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false,
		"log each HID transfer for diagnostics")
}

func formatVersion() string {
	if Commit == "" && Date == "" {
		return Version
	}

	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
