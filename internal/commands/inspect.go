package commands

import (
	"bufio"
	"encoding/json"
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
	inspectJSON        bool
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
			if inspectJSON {
				return runInspectJSON(pid, inspectInspectable)
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
	cmd.Flags().BoolVar(&inspectJSON, "json", false,
		"print live game state, recent events, and invariant status as a single JSON document to stdout")
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

// artifactPaths returns the %TEMP% file paths the engine writes for a given pid.
// snapshotPath: mitiru_inspector_<pid>.json  (SharedSnapshot)
// eventsPath:   mitiru_events_<pid>.jsonl    (EventLog)
func artifactPaths(pid int) (snapshotPath, eventsPath string) {
	tmp := os.TempDir()
	snapshotPath = filepath.Join(tmp, fmt.Sprintf("mitiru_inspector_%d.json", pid))
	eventsPath = filepath.Join(tmp, fmt.Sprintf("mitiru_events_%d.jsonl", pid))
	return
}

// jsonInspectEvent is a single entry from the JSONL event log.
type jsonInspectEvent struct {
	Frame uint32          `json:"frame"`
	T     float64         `json:"t"`
	Type  string          `json:"type"`
	Data  json.RawMessage `json:"data"`
}

// jsonInspectViolation is a single invariant violation extracted from events.
type jsonInspectViolation struct {
	Frame  uint32 `json:"frame"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// jsonInspectInvariants summarises invariant health from the event log.
type jsonInspectInvariants struct {
	OK         bool                   `json:"ok"`
	Violations []jsonInspectViolation `json:"violations"`
}

// jsonInspectOutput is the top-level shape emitted by --json.
type jsonInspectOutput struct {
	PID        int                    `json:"pid"`
	State      json.RawMessage        `json:"state,omitempty"`
	Events     []jsonInspectEvent     `json:"events,omitempty"`
	Invariants *jsonInspectInvariants `json:"invariants,omitempty"`
}

// readSnapshotJSON reads %TEMP%/mitiru_inspector_<pid>.json and returns raw JSON.
// Returns a clear error if the file is absent (game not running).
func readSnapshotJSON(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("snapshot file not found at %s — is a game running for that pid?", path)
		}
		return nil, fmt.Errorf("read snapshot %s: %w", path, err)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("snapshot at %s is not valid JSON (mid-write race?) — retry", path)
	}
	return json.RawMessage(data), nil
}

// readEventsJSONL reads %TEMP%/mitiru_events_<pid>.jsonl, returns up to tailN
// most recent events. Torn/invalid lines are silently skipped (mid-write).
func readEventsJSONL(path string, tailN int) ([]jsonInspectEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("event log not found at %s — has the game emitted any events?", path)
		}
		return nil, fmt.Errorf("open event log %s: %w", path, err)
	}
	defer f.Close()

	var all []jsonInspectEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev jsonInspectEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // torn last line; skip
		}
		all = append(all, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan event log %s: %w", path, err)
	}

	if tailN > 0 && len(all) > tailN {
		all = all[len(all)-tailN:]
	}
	return all, nil
}

// extractInvariants scans events for invariant_violation entries and builds a
// summary. "ok" is true iff no violations appear in the tail window.
func extractInvariants(events []jsonInspectEvent) jsonInspectInvariants {
	inv := jsonInspectInvariants{OK: true, Violations: []jsonInspectViolation{}}
	for _, ev := range events {
		if ev.Type != "invariant_violation" {
			continue
		}
		var d struct {
			Name   string `json:"name"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		inv.OK = false
		inv.Violations = append(inv.Violations, jsonInspectViolation{
			Frame:  ev.Frame,
			Name:   d.Name,
			Detail: d.Detail,
		})
	}
	return inv
}

// runInspectJSON reads the %TEMP% artifacts the engine writes and prints a
// single structured JSON document to stdout. scope selects one of
// "state", "events", "invariants"; empty means all three.
func runInspectJSON(pid int, scope string) error {
	const validScopes = "state, events, invariants"
	switch scope {
	case "", "state", "events", "invariants":
		// ok
	default:
		return fmt.Errorf("inspect --json: unknown --inspectable %q; valid scopes for --json: %s", scope, validScopes)
	}

	if pid <= 0 {
		return fmt.Errorf("inspect --json: a valid game pid is required")
	}

	snapPath, eventsPath := artifactPaths(pid)
	out := jsonInspectOutput{PID: pid}

	wantState := scope == "" || scope == "state"
	wantEvents := scope == "" || scope == "events"
	wantInvariants := scope == "" || scope == "invariants"

	if wantState {
		snap, err := readSnapshotJSON(snapPath)
		if err != nil {
			return err
		}
		out.State = snap
	}

	// Events and invariants both come from the JSONL file.
	if wantEvents || wantInvariants {
		events, err := readEventsJSONL(eventsPath, 64)
		if err != nil {
			// If scope restricts to state only we wouldn't reach here, but guard anyway.
			return err
		}
		if wantEvents {
			out.Events = events
		}
		if wantInvariants {
			inv := extractInvariants(events)
			out.Invariants = &inv
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("inspect --json: encode output: %w", err)
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
