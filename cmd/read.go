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
	readCmd.Flags().StringVarP(&readOutput, "output", "o", "",
		"file to write the 128-byte image to (default: stdout)")
}

//nolint:gochecknoglobals // cobra command literal
var readCmd = &cobra.Command{
	Use:   "read",
	Short: "Read the 128-byte EEPROM image to a file or stdout",
	Long: `read fetches the entire 93C46 EEPROM (64 words × 16 bits = 128
bytes) from the selected device and writes it as raw binary. The output
round-trips bit-for-bit through 'openvlm write -i <file>'.

Examples:
  openvlm read -o image.bin
  openvlm read > image.bin
`,
	RunE: runRead,
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
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	if err := os.WriteFile(readOutput, img[:], 0o644); err != nil { //nolint:gosec // EEPROM image is not a secret
		return fmt.Errorf("write %s: %w", readOutput, err)
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "read %d bytes from %s -> %s\n",
		len(img), d.Path, readOutput)

	return nil
}
