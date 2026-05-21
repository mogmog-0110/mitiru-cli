//go:build windows

// Package install drives the one-shot `installer.exe` workflow that gets a
// fresh Windows machine from zero to `mitiru new my_game && mitiru run`.
//
// Spec: docs/INSTALLER.md (engine repo).
package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	// SkipPrecache: don't pre-download the engine source tarball.
	SkipPrecache bool

	// SkipLongPaths: don't touch HKLM\…\LongPathsEnabled.
	SkipLongPaths bool

	// AssumeYes: treat any "proceed?" prompt as yes (for CI / automation).
	AssumeYes bool

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
	fmt.Fprintln(opts.Stdout)

	// Step 1: environment check (shows the user what they have / lack).
	fmt.Fprintln(opts.Stdout, "Step 1/5: 環境を確認...")
	report, err := snapshot()
	if err != nil {
		return fmt.Errorf("environment check: %w", err)
	}
	report.print(opts.Stdout)

	if !report.hasWinget && !opts.SkipWinget {
		return fmt.Errorf(
			"winget が見つかりません — Windows 10 1809+ / Windows 11 が必要です。\n" +
				"  Microsoft Store の 'App Installer' を入れるか、--skip-winget で手動 install してください。")
	}
	fmt.Fprintln(opts.Stdout)

	// Step 2: install MSVC Build Tools 2022 via winget (skips silently if
	// already installed, per winget's idempotency).
	fmt.Fprintln(opts.Stdout, "Step 2/5: MSVC Build Tools 2022 を install")
	if opts.SkipWinget {
		fmt.Fprintln(opts.Stdout, "  --skip-winget が指定されたため skip")
	} else if report.hasMsvc {
		fmt.Fprintln(opts.Stdout, "  既に install 済み — skip")
	} else if err := installBuildTools(opts); err != nil {
		return fmt.Errorf("install build tools: %w", err)
	}
	fmt.Fprintln(opts.Stdout)

	// Step 3: deploy mitiru.exe next to itself → target dir.
	fmt.Fprintln(opts.Stdout, "Step 3/5: mitiru.exe を deploy")
	if err := deployMitiru(opts); err != nil {
		return fmt.Errorf("deploy mitiru.exe: %w", err)
	}
	fmt.Fprintln(opts.Stdout)

	// Step 4: append target dir to user PATH (HKCU\Environment\Path).
	fmt.Fprintln(opts.Stdout, "Step 4/5: PATH に追加")
	if err := appendUserPath(opts); err != nil {
		return fmt.Errorf("append PATH: %w", err)
	}
	fmt.Fprintln(opts.Stdout)

	// Step 5 (optional): pre-cache the engine source.
	fmt.Fprintln(opts.Stdout, "Step 5/5: engine source を pre-cache")
	if opts.SkipPrecache {
		fmt.Fprintln(opts.Stdout, "  --skip-precache が指定されたため skip")
	} else if err := precacheEngine(opts); err != nil {
		// Pre-cache failure is non-fatal — `mitiru build` will retry on
		// first run. Log and continue.
		fmt.Fprintf(opts.Stderr, "  warning: engine source pre-cache に失敗: %v\n", err)
		fmt.Fprintln(opts.Stderr, "  (初回 `mitiru build` 時に再 try されます)")
	}
	fmt.Fprintln(opts.Stdout)

	// Optional: LongPaths registry. Requires admin — best-effort.
	if !opts.SkipLongPaths {
		fmt.Fprintln(opts.Stdout, "Optional: LongPaths registry")
		if err := enableLongPaths(opts); err != nil {
			fmt.Fprintf(opts.Stderr, "  skipped: %v\n", err)
			fmt.Fprintln(opts.Stderr, "  (admin で再実行するか、手動で HKLM\\SYSTEM\\CurrentControlSet\\Control\\FileSystem\\LongPathsEnabled = 1 にすると将来詰みを防げます)")
		}
		fmt.Fprintln(opts.Stdout)
	}

	// Done — print the next-step recipe.
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
