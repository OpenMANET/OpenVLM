package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(identifyCmd)
}

//nolint:gochecknoglobals // cobra command literal
var identifyCmd = &cobra.Command{
	Use:   "identify",
	Short: "Confirm a device's OpenVLM strap (exit 0 if confirmed, 3 if not)",
	Long: `identify opens the selected device, probes the GPIO1 strap, and
prints the result. Useful in shell scripts:

  if openvlm identify; then
    openvlm provision
  fi

Exit status:
  0  device confirmed as OpenVLM (GPIO1 strap high)
  1  device or HID transfer error (no devices, permission denied, ...)
  3  device present but GPIO1 strap is low (not OpenVLM hardware)
`,
	RunE: runIdentify,
}

func runIdentify(cmd *cobra.Command, _ []string) error {
	d, err := pickDevice()
	if err != nil {
		return err
	}

	if d.ProbeError != nil {
		return fmt.Errorf("probe %s: %w", d.Path, d.ProbeError)
	}

	if !d.IsOpenVLM {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: NOT confirmed\n", d.Path)

		return &notIdentifiedError{
			err: fmt.Errorf("%s: GPIO1 strap low", d.Path),
		}
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: confirmed OpenVLM\n", d.Path)

	return nil
}
