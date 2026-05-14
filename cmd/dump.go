package cmd

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // cobra subcommand state
var dumpFormat string

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(dumpCmd)
	dumpCmd.Flags().StringVar(&dumpFormat, "format", "yaml", flagDumpFormatHelp)
}

//nolint:gochecknoglobals // cobra command literal
var dumpCmd = &cobra.Command{
	Use:   useDump,
	Short: shortDump,
	Long:  longDump,
	RunE:  runDump,
}

func runDump(cmd *cobra.Command, _ []string) error {
	d, t, err := openDevice()
	if err != nil {
		return err
	}

	defer func() { _ = t.Close() }()

	img, err := eeprom.ReadAll(t)
	if err != nil {
		return err //nolint:wrapcheck // already prefixed by eeprom.ReadAll
	}

	switch strings.ToLower(dumpFormat) {
	case "yaml", "":
		return printDumpYAML(cmd, &img)
	case "text":
		return printDumpText(cmd, d, &img)
	case "hex":
		return printDumpHex(cmd, &img)
	default:
		return &usageError{err: fmt.Errorf("unknown --format %q. Use yaml, text, or hex", dumpFormat)}
	}
}

func printDumpText(cmd *cobra.Command, d cm108.Descriptor, img *eeprom.Image) error {
	if !img.IsProgrammed() {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msgChipBlank(flagVerbose, img.VID(), img.PID()))
	}

	view, warnings, decodeErr := img.Decode()
	for _, w := range warnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
	}

	if decodeErr != nil {
		return decodeErr //nolint:wrapcheck // already prefixed by eeprom
	}

	openvlm := "no"
	if d.IsOpenVLM {
		openvlm = "yes"
	}

	out := cmd.OutOrStdout()

	device := displayName(d.SerialNumber, d.Path)
	if flagVerbose {
		device = d.Path
	}

	fmt.Fprintf(out, "DEVICE             %s (serial=%s, openvlm=%s)\n",
		device, displaySerial(d.SerialNumber), openvlm)
	fmt.Fprintf(out, "VID:PID            0x%04X:0x%04X  (read-only)\n", img.VID(), img.PID())
	fmt.Fprintf(out, "PRODUCT-STRING     %s\n", view.ProductString)
	fmt.Fprintf(out, "MANUFACTURER       %s\n", view.ManufacturerString)
	fmt.Fprintf(out, "SERIAL             %s\n", displaySerial(view.Serial))
	fmt.Fprintf(out, "DAC INIT VOLUME    %+d dB\n", view.DACInitVolume)
	fmt.Fprintf(out, "ADC INIT VOLUME    %+d dB\n", view.ADCInitVolume)
	fmt.Fprintf(out, "AA  INIT VOLUME    %+d dB\n", view.AAInitVolume)
	fmt.Fprintf(out, "DAC MIN/MAX        %+d dB / %+d dB\n", view.DACMinVolume, view.DACMaxVolume)
	fmt.Fprintf(out, "ADC MIN/MAX        %+d dB / %+d dB\n", view.ADCMinVolume, view.ADCMaxVolume)
	fmt.Fprintf(out, "AA  MIN/MAX        %+d dB / %+d dB\n", view.AAMinVolume, view.AAMaxVolume)
	fmt.Fprintf(out, "BOOST MODE         %s\n", view.BoostMode)
	fmt.Fprintf(out, "DAC OUTPUT         %s\n", view.DACOutput)
	fmt.Fprintf(out, "MIC BOOST          %t\n", view.MicBoost)
	fmt.Fprintf(out, "MIC HPF            %t\n", view.MicHighPassFilter)
	fmt.Fprintf(out, "MIC PLL ADJUST     %t\n", view.MicPLLAdjust)
	fmt.Fprintf(out, "DAC SHUTDOWN       %t\n", view.DACShutdown)
	fmt.Fprintf(out, "TOTAL POWER CTRL   %t\n", view.TotalPowerControl)
	fmt.Fprintf(out, "HID ENABLE         %t\n", view.HIDEnable)
	fmt.Fprintf(out, "REMOTE WAKEUP      %t\n", view.RemoteWakeup)
	fmt.Fprintf(out, "EXT FIELDS VALID   %t\n", view.ExtendedFieldsValid)
	fmt.Fprintf(out, "SERIAL ENABLE      %t\n", view.SerialEnable)

	return nil
}

func printDumpYAML(cmd *cobra.Command, img *eeprom.Image) error {
	view, _, err := img.Decode()
	if err != nil {
		return err //nolint:wrapcheck // already prefixed by eeprom
	}

	data, err := view.MarshalYAML()
	if err != nil {
		return err //nolint:wrapcheck // already prefixed by eeprom
	}

	if _, err := cmd.OutOrStdout().Write(data); err != nil {
		return fmt.Errorf("couldn't write YAML: %w", err)
	}

	return nil
}

func printDumpHex(cmd *cobra.Command, img *eeprom.Image) error {
	dumper := hex.Dumper(cmd.OutOrStdout())
	if _, err := dumper.Write(img[:]); err != nil {
		return fmt.Errorf("couldn't write hex: %w", err)
	}

	if err := dumper.Close(); err != nil {
		return fmt.Errorf("couldn't finish hex output: %w", err)
	}

	return nil
}

func displaySerial(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
