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

	provisionCmd.Flags().StringVar(&provisionOverridesPath, "overrides", "", flagProvisionOverridesHelp)
	provisionCmd.Flags().BoolVar(&provisionDryRun, "dry-run", false, flagProvisionDryRunHelp)
	provisionCmd.Flags().BoolVar(&provisionForce, "force", false, flagProvisionForceHelp)

	registerOverrideFlags(provisionCmd)
}

//nolint:gochecknoglobals // cobra command literal
var provisionCmd = &cobra.Command{
	Use:   useProvision,
	Short: shortProvision,
	Long:  longProvision,
	RunE:  runProvision,
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
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msgDryRun(len(img)))

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

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), msgProvisioned(displayName(d.SerialNumber, d.Path)))

	return nil
}

// resetProvisionFlags clears the package-level flag state for provision.
// Cobra retains flag values across rootCmd.Execute() calls within the same
// process, so tests must reset before each run; production code only
// Execute()s once so this never runs outside tests.
func resetProvisionFlags() {
	provisionOverridesPath = ""
	provisionDryRun = false
	provisionForce = false
}

func buildMergedView(cmd *cobra.Command) (eeprom.View, error) {
	merged := eeprom.OpenVLMDefaults

	if provisionOverridesPath != "" {
		data, err := os.ReadFile(provisionOverridesPath)
		if err != nil {
			return merged, fmt.Errorf("couldn't read --overrides file: %w", err)
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
