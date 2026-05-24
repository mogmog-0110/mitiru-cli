package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	replayRecordFile string
	replayPlayFile   string
)

func newReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Record or play back an input replay (deterministic)",
		Long: `Runs the engine's replay subsystem in isolation. Part of
MitiruEngine's per-system isolation and the deterministic-replay axis:
the recorded InputSnapshot stream reproduces a session bit-for-bit.

Provide exactly one of:
  --record <file>   record this session to <file>
  --replay <file>   play back a previously recorded <file>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplay()
		},
	}
	cmd.Flags().StringVar(&replayRecordFile, "record", "", "record a session to <file>")
	cmd.Flags().StringVar(&replayPlayFile, "replay", "", "play back <file>")
	return cmd
}

func runReplay() error {
	record := replayRecordFile != ""
	play := replayPlayFile != ""

	switch {
	case record && play:
		return fmt.Errorf("replay: --record and --replay are mutually exclusive; pass exactly one")
	case !record && !play:
		return fmt.Errorf("replay: pass exactly one of --record <file> or --replay <file>")
	}

	if record {
		abs, err := filepath.Abs(replayRecordFile)
		if err != nil {
			return fmt.Errorf("replay: resolve %q: %w", replayRecordFile, err)
		}
		return launchSubsystem("replay", "--record", abs)
	}

	abs, err := filepath.Abs(replayPlayFile)
	if err != nil {
		return fmt.Errorf("replay: resolve %q: %w", replayPlayFile, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("replay: %s: %w", abs, err)
	}
	return launchSubsystem("replay", "--replay", abs)
}
