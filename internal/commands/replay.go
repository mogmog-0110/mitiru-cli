package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay <file>",
		Short: "Build and run the current project replaying a recorded input file",
		Long: `Build the current project (same as 'mitiru build') and launch the resulting
executable with MITIRU_REPLAY=<file> in its environment.

The engine reads the JSON ReplayData on startup and replays the recorded
key / mouse events frame-by-frame, reproducing the original session
deterministically.

Pair with 'mitiru run' under MITIRU_RECORD to capture a session:

  MITIRU_RECORD=./run.json mitiru run
  mitiru replay ./run.json

The file path is resolved to an absolute path before being passed to the
child so the engine receives a stable location regardless of where the
exe is launched from.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplay(args[0])
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "build + replay with Release configuration")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo); overrides --release")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (default is NMake Makefiles)")
	return cmd
}

func runReplay(replayPath string) error {
	absReplay, err := filepath.Abs(replayPath)
	if err != nil {
		return fmt.Errorf("replay: resolve %q: %w", replayPath, err)
	}
	if _, err := os.Stat(absReplay); err != nil {
		return fmt.Errorf("replay: %s: %w", absReplay, err)
	}

	result, err := runBuild()
	if err != nil {
		return err
	}

	fmt.Printf("\nReplaying %s\nRunning  %s\n", absReplay, result.ExePath)

	cmd := exec.Command(result.ExePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = result.ProjectRoot
	cmd.Env = append(os.Environ(), "MITIRU_REPLAY="+absReplay)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", result.ExePath, exitErr.ExitCode())
		}
		return fmt.Errorf("replay %s: %w", result.ExePath, err)
	}
	return nil
}
