package cmd

import (
	"fmt"
	"strings"

	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // cobra subcommand state
var (
	wipeYes     bool
	wipeForce   bool
	wipePattern string
)

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(wipeCmd)
	wipeCmd.Flags().BoolVar(&wipeYes, "yes", false, flagWipeYesHelp)
	wipeCmd.Flags().BoolVar(&wipeForce, "force", false, flagWipeForceHelp)
	wipeCmd.Flags().StringVar(&wipePattern, "pattern", "FF", flagWipePatternHelp)
}

//nolint:gochecknoglobals // cobra command literal
var wipeCmd = &cobra.Command{
	Use:   useWipe,
	Short: shortWipe,
	Long:  longWipe,
	RunE:  runWipe,
}

func runWipe(cmd *cobra.Command, _ []string) error {
	if !wipeYes {
		return &usageError{err: fmt.Errorf(
			"wipe needs --yes to run. This erases the dongle's configuration; the flag is the confirmation")}
	}

	pattern, err := parseWipePattern(wipePattern)
	if err != nil {
		return &usageError{err: err}
	}

	d, t, err := openDevice()
	if err != nil {
		return err
	}

	defer func() { _ = t.Close() }()

	if rErr := requireOpenVLM(d, wipeForce); rErr != nil {
		return rErr
	}

	if wErr := eeprom.WipeAll(t, pattern); wErr != nil {
		return wErr //nolint:wrapcheck // already prefixed by eeprom
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), msgWiped(displayName(d.SerialNumber, d.Path)))

	return nil
}

// resetWipeFlags clears the wipe subcommand's package-level flag state.
// Cobra retains flag values across rootCmd.Execute() invocations within
// the same process, so tests must reset before each run; production code
// only Execute()s once so this never runs outside tests.
func resetWipeFlags() {
	wipeYes = false
	wipeForce = false
	wipePattern = "FF"
}

// parseWipePattern accepts the two documented byte values. Anything else
// is rejected with a helpful message — wiping to a hand-rolled pattern
// has no use case the project cares about, so we don't widen the surface.
func parseWipePattern(s string) (uint16, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FF", "0XFF":
		return 0xFFFF, nil
	case "00", "0X00":
		return 0x0000, nil
	}

	return 0, fmt.Errorf("--pattern %q isn't one of: FF, 00", s)
}
