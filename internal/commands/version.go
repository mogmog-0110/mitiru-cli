package commands

import (
	"fmt"
	"os"
	"runtime"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("mitiru %s (%s/%s)\n", cliVersion, runtime.GOOS, runtime.GOARCH)
			fmt.Printf("mitiru new で作るプロジェクトは engine %s を使います。\n", defaultEngineVersion)
			// build / run の成功時には更新を知らせないので、版を尋ねられたここで知らせる。
			maybeNotifyUpdates(projectEnginePin(), os.Stdout)
			return nil
		},
	}
}

// projectEnginePin はプロジェクトの外や mitiru.toml が読めないときは空を返し、
// その場合は CLI の更新だけを知らせる。
func projectEnginePin() string {
	mp, _, err := config.FindManifest(".")
	if err != nil {
		return ""
	}
	pc, err := config.Load(mp)
	if err != nil {
		return ""
	}
	return pc.Project.Engine
}
