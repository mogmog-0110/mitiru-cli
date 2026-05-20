package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var (
	inspectEngineTag = "latest"
	inspectFilePath  string
)

func newInspectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <pid>",
		Short: "Open a sub-window inspector watching a running MitiruEngine game (axis 5)",
		Long: `Launches the engine's standalone inspector as a separate OS-level
window that polls the snapshot a running MitiruEngine process exports
to %TEMP%\mitiru_inspector_<pid>.json.

Usage:
  mitiru inspect 12345        # watch process id 12345
  mitiru inspect --file <path>  # watch a specific file directly (debug)

This is the axis 5 (modular sub-window architecture) showcase tool —
gameplay stays in its own window, the inspector lives in another window
that can be dragged to a different monitor. No CEF multi-process required.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if inspectFilePath == "" && len(args) != 1 {
				return fmt.Errorf("inspect: provide a pid argument or --file <path>")
			}
			pid := 0
			if len(args) == 1 {
				v, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("inspect: %q is not a valid pid: %w", args[0], err)
				}
				pid = v
			}
			return runInspect(pid, inspectFilePath)
		},
	}
	cmd.Flags().StringVar(&inspectEngineTag, "engine", "latest",
		"engine version to build against (default 'latest'). Overridable via MITIRU_ENGINE_ROOT.")
	cmd.Flags().StringVar(&inspectFilePath, "file", "",
		"watch a snapshot file directly (instead of a pid). For debugging.")
	return cmd
}

func runInspect(pid int, filePath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru inspect is currently Windows-only (running on %s)",
			runtime.GOOS)
	}

	engineRoot, err := engine.EnsureSource(inspectEngineTag, os.Stdout)
	if err != nil {
		return fmt.Errorf("inspect: fetch engine source: %w", err)
	}

	candidates := []string{
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "mitiru_inspector.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "Debug", "mitiru_inspector.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "Release", "mitiru_inspector.exe"),
	}
	exePath := ""
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			exePath = c
			break
		}
	}
	if exePath == "" {
		engineBuildDir := filepath.Join(engineRoot, "build")
		if _, err := os.Stat(filepath.Join(engineBuildDir, "CMakeCache.txt")); err != nil {
			return fmt.Errorf("inspect: engine has not been configured yet; expected %s — run `cmake --preset default` from the engine root first",
				engineBuildDir)
		}
		if err := buildEngineTarget(engineBuildDir, "mitiru_inspector"); err != nil {
			return err
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				exePath = c
				break
			}
		}
		if exePath == "" {
			return fmt.Errorf("inspect: build succeeded but mitiru_inspector.exe was not found under %s", engineBuildDir)
		}
	}

	args := []string{}
	if filePath != "" {
		absFile, err := filepath.Abs(filePath)
		if err != nil {
			return fmt.Errorf("inspect: resolve --file %q: %w", filePath, err)
		}
		args = append(args, "--file", absFile)
	} else {
		args = append(args, strconv.Itoa(pid))
	}

	fmt.Printf("Running %s %v\n", exePath, args)
	cmd := exec.Command(exePath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = filepath.Dir(exePath)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", exePath, exitErr.ExitCode())
		}
		return fmt.Errorf("inspect: %w", err)
	}
	return nil
}
