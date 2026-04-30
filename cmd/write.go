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
	writeCmd.Flags().StringVarP(&writeInput, "input", "i", "", flagWriteInputHelp)
	writeCmd.Flags().BoolVar(&writeForce, "force", false, flagWriteForceHelp)
	_ = writeCmd.MarkFlagRequired("input")
}

//nolint:gochecknoglobals // cobra command literal
var writeCmd = &cobra.Command{
	Use:   useWrite,
	Short: shortWrite,
	Long:  longWrite,
	RunE:  runWrite,
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

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), msgWritten(displayName(d.SerialNumber, d.Path), len(img)))

	return nil
}

func readWriteInput(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("couldn't read from stdin: %w", err)
		}

		return data, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read %s: %w", path, err)
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
			"that file is %d bytes — expected exactly %d for a binary backup, "+
				"and it doesn't look like a YAML config either",
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
