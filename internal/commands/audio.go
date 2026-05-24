package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newAudioCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "audio [file]",
		Short: "Launch the audio subsystem standalone (no game logic)",
		Long: `Runs the engine's audio mixer in isolation — miniaudio initialised,
no renderer, no ECS, no scene manager. Part of MitiruEngine's
per-system isolation (every subsystem boots on its own).

Optionally pass a .wav / .mp3 / .flac path to load on startup.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var passthrough []string
			if len(args) == 1 {
				abs, err := filepath.Abs(args[0])
				if err != nil {
					return fmt.Errorf("audio: resolve %q: %w", args[0], err)
				}
				if _, err := os.Stat(abs); err != nil {
					return fmt.Errorf("audio: %s: %w", abs, err)
				}
				passthrough = append(passthrough, abs)
			}
			return launchSubsystem("audio", passthrough...)
		},
	}
}
