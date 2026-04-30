package cmd

import (
	"fmt"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // cobra subcommand state
var updateForce bool

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&updateForce, "force", false, flagUpdateForceHelp)
	// Stop pflag scanning for flags once the first positional arg appears so
	// negative numbers like "-10" reach RunE verbatim instead of being
	// rejected as unknown shorthand flags. Side effect: --force must precede
	// the positional args (e.g. `update --force dac-init-volume -10`).
	updateCmd.Flags().SetInterspersed(false)
}

//nolint:gochecknoglobals // cobra command literal
var updateCmd = &cobra.Command{
	Use:   useUpdate,
	Short: shortUpdate,
	Long:  longUpdate,
	Args:  cobra.ExactArgs(2),
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	field, value := args[0], args[1]

	d, t, err := openDevice()
	if err != nil {
		return err
	}

	defer func() { _ = t.Close() }()

	if rErr := requireOpenVLM(d, updateForce); rErr != nil {
		return rErr
	}

	img, err := eeprom.ReadAll(t)
	if err != nil {
		return err //nolint:wrapcheck // already prefixed
	}

	view, warnings, err := img.Decode()
	if err != nil {
		return err //nolint:wrapcheck // already prefixed
	}

	for _, w := range warnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
	}

	updated, err := eeprom.ApplyUpdate(view, field, value)
	if err != nil {
		return &usageError{err: err}
	}

	if vErr := updated.Validate(); vErr != nil {
		return vErr //nolint:wrapcheck // multi-error from Validate is the user-facing message
	}

	tail := tailFrom(&img)
	out := updated.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	if wErr := eeprom.WriteImage(t, out); wErr != nil {
		return wErr //nolint:wrapcheck // already prefixed
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), msgUpdated(displayName(d.SerialNumber, d.Path), field))

	return nil
}

// tailFrom extracts EEPROM words 0x33..0x3F so a read-modify-write preserves
// any undocumented factory data the datasheet does not name.
func tailFrom(img *eeprom.Image) [eeprom.WordCount - 0x33]uint16 {
	var tail [eeprom.WordCount - 0x33]uint16
	for i := range tail {
		tail[i] = img.Word(uint8(0x33 + i))
	}

	return tail
}
