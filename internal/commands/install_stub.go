//go:build !windows

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Non-Windows stub: the installer wraps Windows-only behavior (winget +
// HKCU\Environment\Path + LongPaths registry). Print a helpful message
// instead of silently omitting the subcommand.
func newInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "(Windows-only) Bootstrap a fresh machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("mitiru install is currently Windows-only")
		},
	}
}
