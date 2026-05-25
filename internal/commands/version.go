package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("mitiru %s (%s/%s)\n", cliVersion, runtime.GOOS, runtime.GOARCH)
			fmt.Printf("  scaffolds engine %s by default\n", defaultEngineVersion)
			return nil
		},
	}
}
