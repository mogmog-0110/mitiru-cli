package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var (
	inspectEngineTag   = "latest"
	inspectFilePath    string
	inspectInspectable string
	inspectAll         bool
)

// allInspectables is the panel set spawned by `mitiru inspect --all`. Each
// name maps to a named inspectable the engine's inspector knows how to render
// in its own OS-level window — one tool, one concern, one window (axis 5).
var allInspectables = []string{"gameplay", "input", "timetravel"}

func newInspectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect [pid]",
		Short: "Open a sub-window inspector watching a running MitiruEngine game (axis 5)",
		Long: `Launches the engine's standalone inspector as a separate OS-level
window that polls the snapshot a running MitiruEngine process exports
to %TEMP%\mitiru_inspector_<pid>.json.

Usage:
  mitiru inspect                       # auto-pick the most recently-updated game
  mitiru inspect 12345                 # explicit pid
  mitiru inspect 12345 --inspectable timetravel  # one named panel
  mitiru inspect 12345 --all           # gameplay + input + timetravel windows
  mitiru inspect --file <path>         # watch a specific file directly (debug)

This is the axis 5 (modular sub-window architecture) showcase tool —
gameplay stays in its own window, the inspector lives in another window
that can be dragged to a different monitor. With --all, three observer
windows open side by side, each watching the same game process. No CEF
multi-process required.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if inspectAll && inspectInspectable != "" {
				return fmt.Errorf("inspect: --all and --inspectable are mutually exclusive")
			}
			if inspectAll && inspectFilePath != "" {
				return fmt.Errorf("inspect: --all watches a pid and cannot be combined with --file")
			}

			pid := 0
			if inspectFilePath == "" && len(args) == 0 {
				p, err := autoDetectProducerPid()
				if err != nil {
					return fmt.Errorf("inspect: %w", err)
				}
				pid = p
				fmt.Printf("Auto-detected producer pid: %d\n", pid)
			} else if len(args) == 1 {
				v, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("inspect: %q is not a valid pid: %w", args[0], err)
				}
				pid = v
			}

			if inspectAll {
				return runInspectAll(pid)
			}
			return runInspect(pid, inspectFilePath, inspectInspectable)
		},
	}
	cmd.Flags().StringVar(&inspectEngineTag, "engine", "latest",
		"engine version to build against (default 'latest'). Overridable via MITIRU_ENGINE_ROOT.")
	cmd.Flags().StringVar(&inspectFilePath, "file", "",
		"watch a snapshot file directly (instead of a pid). For debugging.")
	cmd.Flags().StringVar(&inspectInspectable, "inspectable", "",
		"open a single named panel (e.g. gameplay, input, timetravel)")
	cmd.Flags().BoolVar(&inspectAll, "all", false,
		"open gameplay + input + timetravel inspector windows side by side (axis 5)")
	return cmd
}

// autoDetectProducerPid scans %TEMP% for mitiru_inspector_*.json files and
// returns the pid encoded in the most-recently-modified one. Lets the user
// type `mitiru inspect` without hunting for a pid in Task Manager.
func autoDetectProducerPid() (int, error) {
	tempDir := os.TempDir()
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return 0, fmt.Errorf("read temp dir %s: %w", tempDir, err)
	}

	type candidate struct {
		pid     int
		modTime time.Time
	}
	var newest *candidate
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "mitiru_inspector_") ||
			!strings.HasSuffix(name, ".json") {
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(name, "mitiru_inspector_"), ".json")
		pid, err := strconv.Atoi(mid)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().After(newest.modTime) {
			newest = &candidate{pid: pid, modTime: info.ModTime()}
		}
	}

	if newest == nil {
		return 0, fmt.Errorf("no running MitiruEngine producer found in %s — start a game with `mitiru run` first, or pass an explicit pid",
			tempDir)
	}
	// Reject snapshots that are way stale (>10s since last write) — that's
	// almost certainly a dead game that left its file behind.
	if time.Since(newest.modTime) > 10*time.Second {
		return 0, fmt.Errorf("the most recent snapshot is %s old (looks dead) — start a fresh game with `mitiru run` first, or pass an explicit pid",
			time.Since(newest.modTime).Round(time.Second))
	}
	return newest.pid, nil
}

// locateInspectorExe finds (and, in a dev tree, builds on demand) the engine's
// mitiru_inspector.exe. It is the single source of truth for both the single
// and --all inspect paths. Returns a clean error on miss — never panics.
func locateInspectorExe() (string, error) {
	engineRoot, err := engine.EnsureSource(inspectEngineTag, os.Stdout)
	if err != nil {
		return "", fmt.Errorf("inspect: fetch engine source: %w", err)
	}

	candidates := []string{
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "mitiru_inspector.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "Debug", "mitiru_inspector.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_inspector", "Release", "mitiru_inspector.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	engineBuildDir := filepath.Join(engineRoot, "build")
	if _, err := os.Stat(filepath.Join(engineBuildDir, "CMakeCache.txt")); err != nil {
		return "", fmt.Errorf("inspect: engine has not been configured yet; expected %s — run `cmake --preset default` from the engine root first",
			engineBuildDir)
	}
	if err := buildEngineTarget(engineBuildDir, "mitiru_inspector"); err != nil {
		return "", err
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("inspect: build succeeded but mitiru_inspector.exe was not found under %s", engineBuildDir)
}

func runInspect(pid int, filePath, inspectable string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru inspect is currently Windows-only (running on %s)",
			runtime.GOOS)
	}

	exePath, err := locateInspectorExe()
	if err != nil {
		return err
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
	if inspectable != "" {
		args = append(args, "--inspectable", inspectable)
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

// runInspectAll spawns one inspector window per name in allInspectables, each
// watching the same game pid. Windows are independent OS processes: the CLI
// starts all three and returns immediately without waiting. Best-effort — a
// failed spawn is a warning, not a hard stop, so the remaining panels still
// open. Requires at least one window to launch to succeed.
func runInspectAll(pid int) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru inspect --all is currently Windows-only (running on %s)",
			runtime.GOOS)
	}
	if pid <= 0 {
		return fmt.Errorf("inspect --all: a valid game pid is required")
	}

	exePath, err := locateInspectorExe()
	if err != nil {
		return err
	}

	launched := 0
	for _, name := range allInspectables {
		args := []string{strconv.Itoa(pid), "--inspectable", name}
		cmd := exec.Command(exePath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = filepath.Dir(exePath)
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: inspect --all: failed to launch %s panel: %v\n", name, err)
			continue
		}
		launched++
		fmt.Printf("Launched %s inspector window (pid %d, watching game %d)\n",
			name, cmd.Process.Pid, pid)
	}

	if launched == 0 {
		return fmt.Errorf("inspect --all: no inspector windows could be launched")
	}
	fmt.Printf("Opened %d/%d sub-windows for game %d (each is an independent OS window — axis 5)\n",
		launched, len(allInspectables), pid)
	return nil
}
