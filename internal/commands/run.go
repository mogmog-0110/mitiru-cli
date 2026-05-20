package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var runWithInspect bool

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Build and run the current project",
		Long: `Build the current project (same as 'mitiru build') and then launch
the resulting executable with the project root as its working directory.

Standard output, standard error, and exit code are forwarded.

With --inspect, also opens the engine's sub-window inspector (axis 5)
alongside the game and shuts it down when the game exits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun()
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "build + run with Release configuration")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo); overrides --release")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (e.g. \"Visual Studio 17 2022\", \"Ninja\"); default is NMake Makefiles")
	cmd.Flags().BoolVar(&runWithInspect, "inspect", false,
		"also launch the sub-window inspector for this game (axis 5)")
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
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run %s: %w", result.ExePath, err)
	}

	var inspectorCmd *exec.Cmd
	if runWithInspect {
		ic, err := startInspectorChild(cmd.Process.Pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: --inspect failed: %v\n", err)
		} else {
			inspectorCmd = ic
			fmt.Printf("Inspector pid: %d (will close when game exits)\n", ic.Process.Pid)
		}
	}

	waitErr := cmd.Wait()

	if inspectorCmd != nil && inspectorCmd.Process != nil {
		_ = inspectorCmd.Process.Kill()
		_, _ = inspectorCmd.Process.Wait()
	}

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", result.ExePath, exitErr.ExitCode())
		}
		return fmt.Errorf("run %s: %w", result.ExePath, waitErr)
	}
	return nil
}

// startInspectorChild locates the engine's pre-built mitiru_inspector.exe
// and launches it as a child process pointed at the just-started game's pid.
// Polls briefly for the game's snapshot file before launching, so the
// inspector doesn't immediately report "waiting for producer..."
func startInspectorChild(gamePid int) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("--inspect is Windows-only for now")
	}
	engineRoot, err := engine.EnsureSource("latest", os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("locate engine source: %w", err)
	}
	exePath := ""
	for _, c := range []string{
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "mitiru_inspector.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "Debug", "mitiru_inspector.exe"),
	} {
		if _, err := os.Stat(c); err == nil {
			exePath = c
			break
		}
	}
	if exePath == "" {
		return nil, fmt.Errorf("mitiru_inspector.exe not found — run `cmake --build <engine>/build --target mitiru_inspector` once")
	}

	// Wait briefly for the producer to write its first snapshot file.
	snap := filepath.Join(os.TempDir(),
		fmt.Sprintf("mitiru_inspector_%d.json", gamePid))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(snap); err == nil {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}

	c := exec.Command(exePath, fmt.Sprintf("%d", gamePid))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = filepath.Dir(exePath)
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("start inspector: %w", err)
	}
	return c, nil
}
