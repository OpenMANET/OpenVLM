package cmd

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // cobra subcommand state
var (
	provisionOverridesPath string
	provisionDryRun        bool
	provisionForce         bool
)

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(provisionCmd)

	provisionCmd.Flags().StringVar(&provisionOverridesPath, "overrides", "",
		"YAML file whose keys override the compiled-in defaults")
	provisionCmd.Flags().BoolVar(&provisionDryRun, "dry-run", false,
		"print the encoded image and exit without touching the device")
	provisionCmd.Flags().BoolVar(&provisionForce, "force", false,
		"bypass the GPIO1 strap safety gate (allows programming a fresh dongle)")

	registerOverrideFlags(provisionCmd)
}

//nolint:gochecknoglobals // cobra command literal
var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Write the compiled-in OpenVLM defaults, with optional overrides",
	Long: `provision writes the compiled-in OpenVLMDefaults image to the
device. Defaults can be overridden in two layered channels (CLI flags win
over YAML, YAML wins over compiled defaults):

  --overrides factory.yaml     YAML file with any subset of fields
  --<field-name> <value>       Per-field flag (one per documented field)

Examples:
  openvlm provision
  openvlm provision --serial "00001234"
  openvlm provision --overrides factory.yaml
  openvlm provision --overrides factory.yaml --dac-init-volume -6
  openvlm provision --dry-run
  openvlm provision --force                    # fresh dongle, no GPIO1 strap

VID, PID, product-string, and manufacturer-string cannot be set; they are
sourced from the compiled-in OpenVLM defaults. The validator runs before
any HID transfer; bad values exit non-zero with no device side-effects.
`,
	RunE: runProvision,
}

func runProvision(cmd *cobra.Command, _ []string) error {
	merged, err := buildMergedView(cmd)
	if err != nil {
		return err
	}

	if vErr := merged.Validate(); vErr != nil {
		return vErr //nolint:wrapcheck // multi-error from Validate is the user-facing message
	}

	var tail [eeprom.WordCount - 0x33]uint16

	img := merged.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	if provisionDryRun {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "dry-run: would write %d bytes\n", len(img))

		dumper := hex.Dumper(cmd.OutOrStdout())
		_, _ = dumper.Write(img[:])

		return dumper.Close() //nolint:wrapcheck // hex dumper Close errors are passthrough
	}

	d, t, err := openDevice()
	if err != nil {
		return err
	}

	defer func() { _ = t.Close() }()

	if rErr := requireOpenVLM(d, provisionForce); rErr != nil {
		return rErr
	}

	if wErr := eeprom.WriteImage(t, img); wErr != nil {
		return wErr //nolint:wrapcheck // already prefixed
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: provisioned\n", d.Path)

	return nil
}

func buildMergedView(cmd *cobra.Command) (eeprom.View, error) {
	merged := eeprom.OpenVLMDefaults

	if provisionOverridesPath != "" {
		data, err := os.ReadFile(provisionOverridesPath)
		if err != nil {
			return merged, fmt.Errorf("read --overrides: %w", err)
		}

		yp, err := eeprom.UnmarshalPartial(data)
		if err != nil {
			return merged, &usageError{err: err}
		}

		merged = eeprom.ApplyOverrides(merged, yp)
	}
	// CLI flags win over YAML. partialOverrides was populated by the
	// per-field --<name> flags during cobra parsing.
	merged = eeprom.ApplyOverrides(merged, &partialOverrides)

	_ = cmd

	return merged, nil
}
