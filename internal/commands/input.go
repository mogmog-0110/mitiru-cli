package commands

import (
	"github.com/spf13/cobra"
)

func newInputCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "input",
		Short: "Launch the input subsystem standalone (no game logic)",
		Long: `Runs the engine's input subsystem in isolation — captures keyboard
and mouse and visualises the live InputSnapshot, with no renderer
gameplay, no ECS, no scene manager. Part of MitiruEngine's
per-system isolation (every subsystem boots on its own), useful for
inspecting exactly what the engine sees each frame.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchSubsystem("input")
		},
	}
}
