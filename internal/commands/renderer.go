package commands

import (
	"github.com/spf13/cobra"
)

func newRendererCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "renderer",
		Short: "Launch the renderer subsystem standalone (no game logic)",
		Long: `Runs the engine's rendering pipeline in isolation — a test pattern
with grid + animated rect. Part of MitiruEngine's per-system
isolation (every subsystem boots on its own), useful for shader /
pipeline iteration and for bisecting "is the renderer broken or is
gameplay broken" questions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchSubsystem("renderer")
		},
	}
}
