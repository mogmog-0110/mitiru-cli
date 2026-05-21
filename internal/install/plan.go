//go:build windows

// Package install drives the one-shot `installer.exe` workflow that gets a
// fresh Windows machine from zero to `mitiru new my_game && mitiru run`.
//
// Spec: docs/INSTALLER.md (engine repo).
package install

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Options gates each step of the installer and routes destructive operations
// past --dry-run.
type Options struct {
	// DryRun: print the plan, change nothing on disk / registry / network.
	DryRun bool

	// TargetDir: where mitiru.exe lands. Defaults to
	// %LOCALAPPDATA%\Programs\MitiruEngine\bin.
	TargetDir string

	// SkipWinget: don't invoke winget. Use this if MSVC is already present.
	SkipWinget bool

	// SkipDeploy: don't copy mitiru.exe into TargetDir. Use this if the user
	// already has their own mitiru build somewhere else and wants to keep it.
	SkipDeploy bool

	// SkipPathEnv: don't append TargetDir to HKCU\Environment\Path. Use this
	// if PATH is managed elsewhere (chocolatey, scoop, profile script, etc.).
	SkipPathEnv bool

	// SkipPrecache: don't pre-download the engine source tarball.
	SkipPrecache bool

	// SkipLongPaths: don't touch HKLM\…\LongPathsEnabled.
	SkipLongPaths bool

	// AssumeYes: skip the "proceed?" prompt (CI / automation).
	AssumeYes bool

	// Force: override the "already installed → skip" heuristics. Useful
	// for repair installs.
	Force bool

	// Stdout / Stderr: defaulted to os.Stdout / os.Stderr if nil.
	Stdout io.Writer
	Stderr io.Writer
}

// DefaultTargetDir returns the canonical install directory under LOCALAPPDATA.
func DefaultTargetDir() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", fmt.Errorf("LOCALAPPDATA env var is not set")
	}
	return filepath.Join(local, "Programs", "MitiruEngine", "bin"), nil
}

// stepAction describes one row in the "what will happen" plan summary that
// is shown to the user before any destructive action.
type stepAction struct {
	name   string // human label
	will   string // "run" | "skip" | "missing"
	reason string // why (e.g. "既存", "--skip-winget", etc.)
}

// buildPlan returns the per-step actions derived from envReport + opts.
// This is what gets printed before y/n consent.
func buildPlan(opts Options, r *envReport) []stepAction {
	steps := []stepAction{}

	// 1. MSVC
	switch {
	case opts.SkipWinget:
		steps = append(steps, stepAction{"MSVC Build Tools 2022", "skip", "--skip-winget"})
	case r.hasMsvc && !opts.Force:
		steps = append(steps, stepAction{"MSVC Build Tools 2022", "skip", "既にある"})
	default:
		why := "winget で install"
		if r.hasMsvc && opts.Force {
			why = "winget で再 install (--force)"
		}
		steps = append(steps, stepAction{"MSVC Build Tools 2022", "run", why})
	}

	// 2. mitiru.exe deploy
	switch {
	case opts.SkipDeploy:
		steps = append(steps, stepAction{"mitiru.exe deploy", "skip", "--skip-deploy"})
	case r.hasMitiru() && strings.EqualFold(filepath.Dir(r.mitiruPath), opts.TargetDir) && !opts.Force:
		steps = append(steps, stepAction{"mitiru.exe deploy", "skip", "target dir に既にある"})
	case r.hasMitiru() && !opts.Force:
		steps = append(steps, stepAction{
			"mitiru.exe deploy", "run",
			fmt.Sprintf("別 location (%s) を target dir にも複製", r.mitiruPath),
		})
	default:
		steps = append(steps, stepAction{"mitiru.exe deploy", "run", opts.TargetDir})
	}

	// 3. PATH
	switch {
	case opts.SkipPathEnv:
		steps = append(steps, stepAction{"PATH 追加", "skip", "--skip-pathenv"})
	default:
		// Idempotency check happens inside appendUserPath. We can't predict
		// here without reading the registry — show as "run" optimistically.
		steps = append(steps, stepAction{"PATH 追加", "run", "HKCU\\Environment\\Path (重複なら skip)"})
	}

	// 4. Pre-cache
	switch {
	case opts.SkipPrecache:
		steps = append(steps, stepAction{"engine source pre-cache", "skip", "--skip-precache"})
	default:
		steps = append(steps, stepAction{"engine source pre-cache", "run", "~/.mitiru/cache/ (既存なら即終了)"})
	}

	// 5. LongPaths (optional)
	switch {
	case opts.SkipLongPaths:
		steps = append(steps, stepAction{"LongPaths registry (optional)", "skip", "--skip-longpaths"})
	default:
		steps = append(steps, stepAction{"LongPaths registry (optional)", "run", "admin 要、失敗時は warn のみ"})
	}
	return steps
}

func printPlan(w io.Writer, steps []stepAction) (toRun int) {
	fmt.Fprintln(w, "実行プラン:")
	for _, s := range steps {
		mark := "  ✓"
		if s.will == "skip" {
			mark = "  ⊘"
		}
		if s.will == "run" {
			toRun++
		}
		fmt.Fprintf(w, "%s %-32s %s\n", mark, s.name, s.reason)
	}
	return toRun
}

// promptConsent reads y/n from stdin. ENTER without input = yes.
// "Press y / Enter to continue, anything else to abort."
func promptConsent(opts Options) (bool, error) {
	if opts.AssumeYes || opts.DryRun {
		return true, nil
	}
	fmt.Fprint(opts.Stdout, "\n続行する? [Y/n]: ")
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return false, fmt.Errorf("read stdin: %w", err)
	}
	ans := strings.TrimSpace(strings.ToLower(line))
	return ans == "" || ans == "y" || ans == "yes", nil
}

// Run executes the installer flow according to opts. It is the single entry
// point used by cmd/installer/main.go.
func Run(opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.TargetDir == "" {
		td, err := DefaultTargetDir()
		if err != nil {
			return err
		}
		opts.TargetDir = td
	}

	fmt.Fprintf(opts.Stdout, "MitiruEngine Installer\n")
	if opts.DryRun {
		fmt.Fprintf(opts.Stdout, "  (dry-run — nothing will be changed)\n")
	}
	if opts.Force {
		fmt.Fprintf(opts.Stdout, "  --force — 既存検出による skip を無視\n")
	}
	fmt.Fprintln(opts.Stdout)

	// Step 1: environment check
	fmt.Fprintln(opts.Stdout, "Step 1/5: 環境を確認...")
	report, err := snapshot()
	if err != nil {
		return fmt.Errorf("environment check: %w", err)
	}
	report.print(opts.Stdout)
	fmt.Fprintln(opts.Stdout)

	if !report.hasWinget && !opts.SkipWinget {
		return fmt.Errorf(
			"winget が見つかりません — Windows 10 1809+ / Windows 11 が必要です。\n" +
				"  Microsoft Store の 'App Installer' を入れるか、--skip-winget で手動 install してください。")
	}

	// Plan summary (driven by detection + flags)
	plan := buildPlan(opts, report)
	toRun := printPlan(opts.Stdout, plan)

	if toRun == 0 {
		fmt.Fprintln(opts.Stdout, "\nやることがない — 全ステップ skip されました。すでに環境は整ってます。")
		printDone(opts)
		return nil
	}

	// Existing mitiru at non-target location warning
	if report.hasMitiru() && !strings.EqualFold(filepath.Dir(report.mitiruPath), opts.TargetDir) && !opts.SkipDeploy {
		fmt.Fprintf(opts.Stdout, "\n警告: mitiru は既に PATH 経由で %s から見えています\n", report.mitiruPath)
		fmt.Fprintf(opts.Stdout, "        target dir (%s) にも deploy します。後勝ち / 先勝ちは PATH の順序で決まります。\n", opts.TargetDir)
		fmt.Fprintln(opts.Stdout, "        既存版だけで OK なら --skip-deploy --skip-pathenv で進めてください。")
	}

	ok, err := promptConsent(opts)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(opts.Stdout, "中止しました。")
		return nil
	}
	fmt.Fprintln(opts.Stdout)

	// Step 2: install MSVC Build Tools via winget.
	fmt.Fprintln(opts.Stdout, "Step 2/5: MSVC Build Tools 2022 を install")
	switch {
	case opts.SkipWinget:
		fmt.Fprintln(opts.Stdout, "  --skip-winget が指定されたため skip")
	case report.hasMsvc && !opts.Force:
		fmt.Fprintln(opts.Stdout, "  既に install 済み — skip")
	default:
		if err := installBuildTools(opts); err != nil {
			return fmt.Errorf("install build tools: %w", err)
		}
	}
	fmt.Fprintln(opts.Stdout)

	// Step 3: deploy mitiru.exe.
	fmt.Fprintln(opts.Stdout, "Step 3/5: mitiru.exe を deploy")
	switch {
	case opts.SkipDeploy:
		fmt.Fprintln(opts.Stdout, "  --skip-deploy が指定されたため skip")
	case report.hasMitiru() && strings.EqualFold(filepath.Dir(report.mitiruPath), opts.TargetDir) && !opts.Force:
		fmt.Fprintln(opts.Stdout, "  既に target dir に存在 — skip")
	default:
		if err := deployMitiru(opts); err != nil {
			return fmt.Errorf("deploy mitiru.exe: %w", err)
		}
	}
	fmt.Fprintln(opts.Stdout)

	// Step 4: append user PATH.
	fmt.Fprintln(opts.Stdout, "Step 4/5: PATH に追加")
	switch {
	case opts.SkipPathEnv:
		fmt.Fprintln(opts.Stdout, "  --skip-pathenv が指定されたため skip")
	default:
		if err := appendUserPath(opts); err != nil {
			return fmt.Errorf("append PATH: %w", err)
		}
	}
	fmt.Fprintln(opts.Stdout)

	// Step 5: pre-cache engine source.
	fmt.Fprintln(opts.Stdout, "Step 5/5: engine source を pre-cache")
	if opts.SkipPrecache {
		fmt.Fprintln(opts.Stdout, "  --skip-precache が指定されたため skip")
	} else if err := precacheEngine(opts); err != nil {
		fmt.Fprintf(opts.Stderr, "  warning: engine source pre-cache に失敗: %v\n", err)
		fmt.Fprintln(opts.Stderr, "  (初回 `mitiru build` 時に再 try されます)")
	}
	fmt.Fprintln(opts.Stdout)

	// Optional: LongPaths registry.
	if !opts.SkipLongPaths {
		fmt.Fprintln(opts.Stdout, "Optional: LongPaths registry")
		if err := enableLongPaths(opts); err != nil {
			fmt.Fprintf(opts.Stderr, "  skipped: %v\n", err)
			fmt.Fprintln(opts.Stderr, "  (admin で再実行するか、手動で HKLM\\SYSTEM\\CurrentControlSet\\Control\\FileSystem\\LongPathsEnabled = 1 にすると将来詰みを防げます)")
		}
		fmt.Fprintln(opts.Stdout)
	}

	printDone(opts)
	return nil
}

func printDone(opts Options) {
	fmt.Fprintln(opts.Stdout, "完了!")
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "新しい terminal を開いて、こうしてください:")
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "    mitiru new my_game")
	fmt.Fprintln(opts.Stdout, "    cd my_game")
	fmt.Fprintln(opts.Stdout, "    mitiru run")
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "何かハマったら: https://github.com/mogmog-0110/MitiruEngine/blob/main/docs/FIRST_TOUCH.md")
}
