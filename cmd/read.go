package cmd

import (
	"fmt"
	"os"

	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // cobra subcommand state
var readOutput string

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(readCmd)
	readCmd.Flags().StringVarP(&readOutput, "output", "o", "", flagReadOutputHelp)
}

//nolint:gochecknoglobals // cobra command literal
var readCmd = &cobra.Command{
	Use:   useRead,
	Short: shortRead,
	Long:  longRead,
	RunE:  runRead,
}

func runRead(cmd *cobra.Command, _ []string) error {
	d, t, err := openDevice()
	if err != nil {
		return err
	}

	defer func() { _ = t.Close() }()

	img, err := eeprom.ReadAll(t)
	if err != nil {
		return err //nolint:wrapcheck // already prefixed by eeprom.ReadAll
	}

	if readOutput == "" {
		if _, err := cmd.OutOrStdout().Write(img[:]); err != nil {
			return fmt.Errorf("couldn't write to stdout: %w", err)
		}

		return nil
	}

	if err := os.WriteFile(readOutput, img[:], 0o644); err != nil { //nolint:gosec // EEPROM image is not a secret
		return fmt.Errorf("couldn't write %s: %w", readOutput, err)
	}

	name := displayName(d.SerialNumber, d.Path)

	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msgReadComplete(name, len(img), readOutput))

	return nil
}
