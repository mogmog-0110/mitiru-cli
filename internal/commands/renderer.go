package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var rendererEngineTag = "latest"

func newRendererCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "renderer",
		Short: "Launch the renderer subsystem in isolation (axis 3)",
		Long: `Builds and runs the engine's standalone renderer playground:
a window driven only by the renderer subsystem, with no CEF, no audio,
no ECS, no scene manager. Boots in under a second and shows test
patterns useful for shader / pipeline iteration and for bisecting
"is the renderer broken or is gameplay broken" questions.

This is the axis 3 (per-system isolation) showcase tool. Unlike
'mitiru run', it does not require a mitiru.toml — the project being
built IS the engine's own renderer example.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRenderer()
		},
	}
	cmd.Flags().StringVar(&rendererEngineTag, "engine", "latest",
		"engine version to build against (default 'latest'). Overridable via MITIRU_ENGINE_ROOT.")
	return cmd
}

func runRenderer() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru renderer is currently Windows-only (running on %s)",
			runtime.GOOS)
	}

	engineRoot, err := engine.EnsureSource(rendererEngineTag, os.Stdout)
	if err != nil {
		return fmt.Errorf("renderer: fetch engine source: %w", err)
	}

	// Fast path: if the engine repo has already built mitiru_renderer.exe
	// (engine developers do this constantly), skip the entire cmake +
	// re-link cycle and just run it. The consumer build pipeline is the
	// fallback for users who have the engine source but never built it.
	candidates := []string{
		filepath.Join(engineRoot, "build", "examples", "mitiru_renderer", "mitiru_renderer.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_renderer", "Debug", "mitiru_renderer.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_renderer", "Release", "mitiru_renderer.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			fmt.Printf("Running %s\n", c)
			return runExe(c, filepath.Dir(c))
		}
	}

	// Slow path: build via the engine's main CMake tree using the engine
	// repo's `mitiru_renderer` target. This requires the engine's CMake to
	// have been configured at least once.
	engineBuildDir := filepath.Join(engineRoot, "build")
	if _, err := os.Stat(filepath.Join(engineBuildDir, "CMakeCache.txt")); err != nil {
		return fmt.Errorf("renderer: engine has not been configured yet; expected %s — run `cmake --preset default` from the engine root first",
			engineBuildDir)
	}

	if err := buildEngineTarget(engineBuildDir, "mitiru_renderer"); err != nil {
		return err
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			fmt.Printf("Running %s\n", c)
			return runExe(c, filepath.Dir(c))
		}
	}
	return fmt.Errorf("renderer: build succeeded but mitiru_renderer.exe was not found under %s", engineBuildDir)
}

func buildEngineTarget(buildDir, target string) error {
	vcvars, err := build.FindVcvars64()
	if err != nil {
		return err
	}
	script := fmt.Sprintf(
		"@echo off\r\n"+
			"set \"PATH=C:\\Program Files (x86)\\Microsoft Visual Studio\\Installer;%%PATH%%\"\r\n"+
			"call \"%s\" >NUL\r\n"+
			"if errorlevel 1 exit /b %%errorlevel%%\r\n"+
			"cmake --build \"%s\" --config Debug --target %s\r\n",
		vcvars, buildDir, target)

	tmp, err := os.CreateTemp("", "mitiru_renderer-*.bat")
	if err != nil {
		return fmt.Errorf("renderer: create temp batch: %w", err)
	}
	scriptPath := tmp.Name()
	defer os.Remove(scriptPath)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return fmt.Errorf("renderer: write temp batch: %w", err)
	}
	tmp.Close()

	cmd := exec.Command("cmd", "/c", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("renderer: cmake --build %s --target %s: %w", buildDir, target, err)
	}
	return nil
}

func runExe(exePath, workDir string) error {
	cmd := exec.Command(exePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = workDir

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", exePath, exitErr.ExitCode())
		}
		return fmt.Errorf("renderer: %w", err)
	}
	return nil
}
