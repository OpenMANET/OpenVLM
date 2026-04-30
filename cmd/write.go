package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // cobra subcommand state
var (
	writeInput string
	writeForce bool
)

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(writeCmd)
	writeCmd.Flags().StringVarP(&writeInput, "input", "i", "",
		"input file: 128-byte raw .bin (matches `read` output) or YAML "+
			"(matches `dump --format yaml`); use - for stdin")
	writeCmd.Flags().BoolVar(&writeForce, "force", false,
		"bypass the GPIO1 strap safety gate (allows programming a fresh dongle)")
	_ = writeCmd.MarkFlagRequired("input")
}

//nolint:gochecknoglobals // cobra command literal
var writeCmd = &cobra.Command{
	Use:   "write",
	Short: "Write a 128-byte image (raw .bin or YAML) to the device",
	Long: `write programs the EEPROM from a complete image.

Input formats are auto-detected:
  - exactly 128 bytes long → raw image (matches 'read' output)
  - anything else          → YAML overrides (parsed as PartialView and
                              merged onto OpenVLMDefaults so partial files
                              are accepted)

Behavior:
  - Decodes the input, runs the validator, refuses on any error.
  - VID/PID in raw images must equal the OpenVLM constants. Mismatch is
    a hard error even with --force.
  - Writes word-by-word and reads back to verify each one.

Examples:
  openvlm write -i image.bin
  openvlm write -i config.yaml
  openvlm write -i config.yaml --force
  openvlm dump --format yaml | openvlm write -i -
`,
	RunE: runWrite,
}

func runWrite(cmd *cobra.Command, _ []string) error {
	data, err := readWriteInput(cmd, writeInput)
	if err != nil {
		return err
	}

	img, err := imageFromInput(data)
	if err != nil {
		return err
	}

	view, _, decodeErr := img.Decode()
	if decodeErr != nil {
		return decodeErr //nolint:wrapcheck // already prefixed
	}

	if vErr := view.Validate(); vErr != nil {
		return vErr //nolint:wrapcheck // user-facing multi-error
	}

	d, t, err := openDevice()
	if err != nil {
		return err
	}

	defer func() { _ = t.Close() }()

	if rErr := requireOpenVLM(d, writeForce); rErr != nil {
		return rErr
	}

	if wErr := eeprom.WriteImage(t, img); wErr != nil {
		return wErr //nolint:wrapcheck // already prefixed
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: wrote %d bytes (verified)\n", d.Path, len(img))

	return nil
}

func readWriteInput(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}

		return data, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return data, nil
}

// imageFromInput dispatches on length: exactly 128 bytes → raw image; else
// YAML overrides on top of OpenVLMDefaults. The YAML branch is what makes
// `dump --format yaml | write -i -` round-trip and what lets users hand-edit
// a partial config file.
func imageFromInput(data []byte) (eeprom.Image, error) {
	if len(data) == eeprom.ByteCount {
		var img eeprom.Image

		copy(img[:], data)

		return img, nil
	}
	// Heuristic: if it looks like YAML (starts with text, contains a colon
	// before any binary byte), treat it as such.
	if !looksLikeYAML(data) {
		return eeprom.Image{}, &usageError{err: fmt.Errorf(
			"input is %d bytes, expected exactly %d for a raw image, "+
				"and does not look like YAML",
			len(data), eeprom.ByteCount)}
	}

	yp, err := eeprom.UnmarshalPartial(data)
	if err != nil {
		return eeprom.Image{}, &usageError{err: err}
	}

	merged := eeprom.ApplyOverrides(eeprom.OpenVLMDefaults, yp)

	if err := merged.Validate(); err != nil {
		return eeprom.Image{}, err //nolint:wrapcheck // user-facing multi-error
	}

	var tail [eeprom.WordCount - 0x33]uint16

	return merged.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail), nil
}

func looksLikeYAML(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return false
		}

		if b > 127 {
			return false
		}
	}
	// Require at least one ':' on a non-comment line.
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.Contains(trimmed, ":") {
			return true
		}
	}

	return false
}
