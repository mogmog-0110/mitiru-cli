package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Build (Debug) and run with engine debug helpers enabled",
		Long: `Build the current project in Debug configuration and launch it with
runtime debug helpers turned on via environment flags.

Equivalent to 'mitiru run' plus:

  - --config Debug forced (overrides project default)
  - MITIRU_DEBUG=1 in the environment
  - MITIRU_INSPECTOR=1 in the environment (engine inspector window
    opt-in for the time-travel/scrub UI added in P2; ignored by older
    engine builds)

Standard output, standard error, and exit code are forwarded.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebug()
		},
	}
	return cmd
}

func runDebug() error {
	// Force Debug configuration regardless of --release flag presence.
	buildRelease = false
	buildConfigName = "Debug"

	result, err := runBuild()
	if err != nil {
		return err
	}

	fmt.Printf("\nRunning %s [debug mode]\n", result.ExePath)

	cmd := exec.Command(result.ExePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = result.ProjectRoot
	cmd.Env = append(os.Environ(),
		"MITIRU_DEBUG=1",
		"MITIRU_INSPECTOR=1",
	)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d (see stderr above)",
				result.ExePath, exitErr.ExitCode())
		}
		return fmt.Errorf("debug run %s: %w", result.ExePath, err)
	}
	return nil
}
