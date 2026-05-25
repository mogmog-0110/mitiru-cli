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

var (
	runWithInspect bool
	runRecordFile  string
)

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
		"explicit CMake generator (e.g. \"Visual Studio 17 2022\", \"NMake Makefiles\"); default is Ninja")
	cmd.Flags().BoolVar(&runWithInspect, "inspect", false,
		"also launch the sub-window inspector for this game (axis 5)")
	cmd.Flags().StringVar(&runRecordFile, "record", "",
		"record this session's input to <file>.mtrr for `mitiru replay --test --game`")
	return cmd
}

func runRun() error {
	result, err := runBuild()
	if err != nil {
		return err
	}

	art := result.Artifacts
	fmt.Printf("\nRunning %s %s\n", filepath.Base(art.HostExePath), art.DllRel)

	hostArgs := []string{art.DllRel}
	if runRecordFile != "" {
		abs, err := filepath.Abs(runRecordFile)
		if err != nil {
			return fmt.Errorf("run: resolve --record %q: %w", runRecordFile, err)
		}
		hostArgs = append(hostArgs, "--record", abs)
		fmt.Printf("Recording input → %s\n", abs)
	}

	cmd := exec.Command(art.HostExePath, hostArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = art.DeployDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run %s %s: %w", art.HostExePath, art.DllRel, err)
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
			return fmt.Errorf("%s exited with status %d", filepath.Base(art.HostExePath), exitErr.ExitCode())
		}
		return fmt.Errorf("run %s: %w", filepath.Base(art.HostExePath), waitErr)
	}
	return nil
}

// startInspectorChild は engine の pre-built な mitiru_inspector.exe を探し、
// 起動直後のゲームの pid を指す child process として launch する。
// launch 前にゲームの snapshot file を短時間 poll し、inspector が即座に
// "waiting for producer..." を報告しないようにする。
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

	// producer が最初の snapshot file を書くのを短時間待つ。
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
