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
	Use:   useIdentify,
	Short: shortIdentify,
	Long:  longIdentify,
	RunE:  runIdentify,
}

func runIdentify(cmd *cobra.Command, _ []string) error {
	d, err := pickDevice()
	if err != nil {
		return err
	}

	name := displayName(d.SerialNumber, d.Path)

	if d.ProbeError != nil {
		return fmt.Errorf("couldn't probe %s: %w", name, d.ProbeError)
	}

	if !d.IsOpenVLM {
		return &notIdentifiedError{
			err: fmt.Errorf("%s: this doesn't look like an OpenVLM device (its identity bit isn't set)",
				name),
		}
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), msgIdentified(name))

	return nil
}
