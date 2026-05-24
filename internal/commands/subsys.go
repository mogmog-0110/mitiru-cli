package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
)

// locateSubsystem finds the standalone subsystem executable for `name`
// (e.g. "renderer" -> mitiru_subsys_renderer.exe). It is the single source
// of truth for how the subsystem launch commands (renderer / audio / input /
// scene / replay) discover their backing exe.
//
// Search order:
//  1. $MITIRU_HOME/bin/mitiru_subsys_<name>.exe   (installed layout)
//  2. <dir of the mitiru CLI exe>/mitiru_subsys_<name>.exe (release zip,
//     subsystems shipped alongside mitiru.exe)
//  3. <engine source>/build/examples/mitiru_subsys_<name>/...  (dev tree,
//     where the engine repo builds them — Debug / Release subdirs included)
//
// On miss it returns a clean error telling the user how to build the target.
func locateSubsystem(name string) (string, error) {
	exeName := subsysExeName(name)

	// (1) Installed layout under $MITIRU_HOME/bin.
	if home := os.Getenv("MITIRU_HOME"); home != "" {
		c := filepath.Join(home, "bin", exeName)
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	// (2) Alongside the running CLI executable (release zip layout).
	if self, err := os.Executable(); err == nil {
		c := filepath.Join(filepath.Dir(self), exeName)
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	// (3) Dev tree: the engine source's build/examples output.
	engineRoot, err := engine.EnsureSource("latest", os.Stdout)
	if err != nil {
		return "", fmt.Errorf("%s: locate engine source: %w", name, err)
	}
	dir := filepath.Join(engineRoot, "build", "examples", "mitiru_subsys_"+name)
	for _, c := range []string{
		filepath.Join(dir, exeName),
		filepath.Join(dir, "Debug", exeName),
		filepath.Join(dir, "Release", exeName),
	} {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf(
		"mitiru_subsys_%s not found. Build it with: cmake --build build --target mitiru_subsys_%s",
		name, name)
}

// subsysExeName returns the platform-specific executable file name for a
// subsystem. The engine targets ship as .exe on Windows; on other platforms
// the bare target name is used so the locator still composes valid paths.
func subsysExeName(name string) string {
	base := "mitiru_subsys_" + name
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// launchSubsystem locates and runs the subsystem exe for `name`, forwarding
// stdio and passing `args` through to the child. The exit code is surfaced as
// an error so the CLI can mirror the subsystem's status.
func launchSubsystem(name string, args ...string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru %s is currently Windows-only (running on %s)",
			name, runtime.GOOS)
	}

	exePath, err := locateSubsystem(name)
	if err != nil {
		return err
	}

	fmt.Printf("Running %s\n", exePath)

	cmd := exec.Command(exePath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = filepath.Dir(exePath)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", filepath.Base(exePath), exitErr.ExitCode())
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// buildEngineTarget builds a single CMake target inside the engine's own
// build tree, going through an MSVC environment (vcvars64) first. Used by the
// audio / inspector commands when a pre-built exe is not yet present.
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

	tmp, err := os.CreateTemp("", "mitiru_target-*.bat")
	if err != nil {
		return fmt.Errorf("create temp batch: %w", err)
	}
	scriptPath := tmp.Name()
	defer os.Remove(scriptPath)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp batch: %w", err)
	}
	tmp.Close()

	cmd := exec.Command("cmd", "/c", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cmake --build %s --target %s: %w", buildDir, target, err)
	}
	return nil
}
