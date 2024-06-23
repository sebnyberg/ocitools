package main

import (
	"io"

	"github.com/spf13/cobra"
)

func newRootCmd(out io.Writer, args []string) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:          "sctl",
		Short:        "Sctl is a collection of tools for managing containers etc.",
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newPullCommand(out),
	)
	return cmd, nil
}
