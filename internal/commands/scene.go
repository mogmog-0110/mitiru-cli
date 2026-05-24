package commands

import (
	"github.com/spf13/cobra"
)

func newSceneCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "scene",
		Short: "Launch the scene subsystem standalone (no game logic)",
		Long: `Runs the engine's scene manager in isolation — push / pop / replace
of scenes driven only by the scene subsystem, with no gameplay logic.
Part of MitiruEngine's per-system isolation (every subsystem boots on
its own), useful for observing transition and lifecycle behaviour.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchSubsystem("scene")
		},
	}
}
