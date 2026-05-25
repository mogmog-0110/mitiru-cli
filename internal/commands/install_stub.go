//go:build !windows

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// 非 Windows 向け stub: installer は Windows 専用の挙動 (winget +
// HKCU\Environment\Path + LongPaths registry) を wrap する。subcommand を黙って
// 省くのではなく、役立つ message を出力する。
func newInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "(Windows-only) Bootstrap a fresh machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("mitiru install is currently Windows-only")
		},
	}
}
