package commands

import (
	"github.com/spf13/cobra"
)

const (
	cliName    = "mitiru"
	cliVersion = "0.1.0"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   cliName,
		Short: "MitiruEngine project tool",
		Long: `mitiru — MitiruEngine project tool.

Manage MitiruEngine game projects without touching CMakeLists.txt:
  mitiru new <name>      create a new project
  mitiru build           build the current project
  mitiru run             build and run
  mitiru debug           build (Debug) and run with engine debug helpers
  mitiru replay <file>   build and run, replaying recorded input
  mitiru clean           remove build/ (--all also clears engine cache)
  mitiru doctor          check that prerequisites are installed
  mitiru version         print version`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newNewCommand())
	root.AddCommand(newBuildCommand())
	root.AddCommand(newRunCommand())
	root.AddCommand(newDebugCommand())
	root.AddCommand(newReplayCommand())
	root.AddCommand(newCleanCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newVersionCommand())

	return root
}
