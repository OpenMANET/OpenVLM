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

	fmt.Fprintln(w, "Serial\tDevice\tOpenVLM?\tNotes")

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
