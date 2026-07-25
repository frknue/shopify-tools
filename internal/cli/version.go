package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/frknue/shopify-tools/internal/app"
	"github.com/frknue/shopify-tools/internal/buildinfo"
	"github.com/frknue/shopify-tools/internal/output"
)

func newVersionCommand(f *app.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Current()

			printer, err := f.Printer()
			if err != nil {
				return err
			}
			if printer.Format() == output.FormatTable {
				fmt.Fprintln(f.IOStreams.Out, info.String())
				return nil
			}
			return printer.Print(info)
		},
	}
}
