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
	Use:   useList,
	Short: shortList,
	Long:  longList,
	RunE:  runList,
}

func runList(cmd *cobra.Command, _ []string) error {
	descs, err := cm108.List(backend)
	if err != nil {
		return err //nolint:wrapcheck // already prefixed by cm108.List
	}

	if len(descs) == 0 {
		return fmt.Errorf("%w", cm108.ErrNoDevice)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	// Default mode keeps the table compact and free of platform-specific
	// device-path noise. The OS path is only useful for disambiguation when
	// multiple devices have no serial number — surface it via --verbose so
	// it doesn't pollute the common single-device case.
	if flagVerbose {
		fmt.Fprintln(w, "Serial\tOpenVLM?\tPath\tNotes")
	} else {
		fmt.Fprintln(w, "Serial\tOpenVLM?\tNotes")
	}

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

		if flagVerbose {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", serial, strap, d.Path, note)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\n", serial, strap, note)
		}
	}

	return w.Flush() //nolint:wrapcheck // tabwriter.Flush errors are passthrough
}
