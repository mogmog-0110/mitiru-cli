package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Build and run the current project",
		Long: `Build the current project (same as 'mitiru build') and then launch
the resulting executable with the project root as its working directory.

Standard output, standard error, and exit code are forwarded.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun()
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "build + run with Release configuration")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo); overrides --release")
	return cmd
}

func runRun() error {
	result, err := runBuild()
	if err != nil {
		return err
	}

	fmt.Printf("\nRunning %s\n", result.ExePath)

	cmd := exec.Command(result.ExePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = result.ProjectRoot

	if err := cmd.Run(); err != nil {
		// If the child exited non-zero, surface its exit code unchanged.
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", result.ExePath, exitErr.ExitCode())
		}
		return fmt.Errorf("run %s: %w", result.ExePath, err)
	}
	return nil
}
