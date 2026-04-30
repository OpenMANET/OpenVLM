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
	updateCmd.Flags().BoolVar(&updateForce, "force", false,
		"bypass the GPIO1 strap safety gate (allows programming a fresh dongle)")
}

//nolint:gochecknoglobals // cobra command literal
var updateCmd = &cobra.Command{
	Use:   "update <field> <value>",
	Short: "Read the live EEPROM, change one field, write the result back",
	Long: `update reads the EEPROM, applies one field change, validates the
result, and writes it back. This is read-modify-write in a single command.

Field names match the YAML / 'dump --format yaml' keys:

  ` + eeprom.FieldList() + `

Values are entered in human form (decimal integers, plain strings,
true/false, named enums). Hex is not accepted. Examples:

  openvlm update serial "00001234"
  openvlm update dac-init-volume -6
  openvlm update mic-boost true
  openvlm update boost-mode 22db
  openvlm update dac-output headset

VID, PID, product-string, and manufacturer-string cannot be changed;
trying to update them is an error.
`,
	Args: cobra.ExactArgs(2),
	RunE: runUpdate,
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

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: updated %s\n", d.Path, field)

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
