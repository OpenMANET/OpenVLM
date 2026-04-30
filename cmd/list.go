package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // cobra subcommand self-registration
	rootCmd.AddCommand(listCmd)
}

//nolint:gochecknoglobals // cobra command literal
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List every CM108-family device matching the OpenVLM VID/PID",
	Long: `list enumerates every USB HID device matching the OpenVLM USB IDs
(VID 0x0D8C, PID 0x0012). For each match it probes the GPIO1 strap and
reports whether the device is positively identified as OpenVLM hardware.

Exit status:
  0  at least one matching device was found
  1  no matching device found
`,
	RunE: runList,
}

func runList(cmd *cobra.Command, _ []string) error {
	descs, err := cm108.List(backend)
	if err != nil {
		return err //nolint:wrapcheck // already prefixed by cm108.List
	}

	if len(descs) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no OpenVLM devices found")

		return fmt.Errorf("no devices found")
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "SERIAL\tPATH\tOPENVLM\tNOTE")

	for _, d := range descs {
		note := ""
		if d.ProbeError != nil {
			note = "probe error: " + d.ProbeError.Error()
		}

		strap := "no"
		if d.IsOpenVLM {
			strap = "yes"
		}

		serial := d.SerialNumber
		if serial == "" {
			serial = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", serial, d.Path, strap, note)
	}

	return w.Flush() //nolint:wrapcheck // tabwriter.Flush errors are passthrough
}
