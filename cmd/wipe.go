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
	wipeCmd.Flags().BoolVar(&wipeYes, "yes", false,
		"required confirmation flag — wipe will refuse to run without this")
	wipeCmd.Flags().BoolVar(&wipeForce, "force", false,
		"bypass the GPIO1 strap safety gate (allows wiping a fresh dongle)")
	wipeCmd.Flags().StringVar(&wipePattern, "pattern", "FF",
		"byte pattern to write to every word: FF (default, virgin-chip state) or 00")
}

//nolint:gochecknoglobals // cobra command literal
var wipeCmd = &cobra.Command{
	Use:   "wipe",
	Short: "Erase the EEPROM by writing 0xFFFF (or 0x0000) to every word",
	Long: `wipe writes a uniform pattern to every byte of the 93C46 EEPROM,
returning the chip to a state indistinguishable from factory blank.

After a successful wipe:
  - The magic word at 0x00 is invalid (no longer 0x670X), so the CM108B
    falls back to its internal-ROM USB descriptors on the next enumeration.
  - The GPIO1 strap probe will start failing, since 'IsOpenVLM' relies on
    the strap reading high after the chip has been programmed at least
    once. Re-provisioning a wiped device requires --force.

Safety:
  - --yes is REQUIRED. Wipe will refuse to run otherwise. There is no
    interactive prompt; the flag is the prompt.
  - --force is REQUIRED if the GPIO1 strap is not confirmed (typical for
    bench-clearing or recovery work).
  - VID/PID write-lock is intentionally bypassed for this verb — wiping
    the identity bytes is the whole point.

Examples:
  openvlm wipe --yes
  openvlm wipe --yes --pattern 00
  openvlm wipe --yes --force        # post-provision strap-low devices
`,
	RunE: runWipe,
}

func runWipe(cmd *cobra.Command, _ []string) error {
	if !wipeYes {
		return &usageError{err: fmt.Errorf(
			"wipe requires --yes (this is a destructive operation; the flag is the confirmation)")}
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

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"%s: wiped all %d words to 0x%04X (re-enumerate to apply)\n",
		d.Path, eeprom.WordCount, pattern)

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

	return 0, fmt.Errorf("--pattern %q is not one of: FF, 00", s)
}
