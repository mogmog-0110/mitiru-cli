package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
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

	// (3) Dev / cache flow: the engine ships as source only, so the prebuilt
	// exe is usually absent. Resolve the engine the project pins and build the
	// single subsystem target on demand (configured + cached on first run).
	engineRoot, err := resolveEngineRoot()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return findOrBuildEngineExe(engineRoot, "mitiru_subsys_"+name, exeName)
}

// resolveEngineRoot returns the cached engine source tree the current project
// pins (via mitiru.toml). It deliberately does NOT fall back to the "latest"
// GitHub release: tags exist but published Releases may not, and subsystems
// must match the version the project builds against.
func resolveEngineRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	manifestPath, _, err := config.FindManifest(cwd)
	if err != nil {
		return "", fmt.Errorf("run this from inside a mitiru project (mitiru.toml pins the engine version): %w", err)
	}
	cfg, err := config.Load(manifestPath)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", manifestPath, err)
	}
	root, err := engine.EnsureSource(cfg.EngineTag(), os.Stdout)
	if err != nil {
		return "", fmt.Errorf("fetch engine %s: %w", cfg.EngineTag(), err)
	}
	return root, nil
}

// findOrBuildEngineExe returns the path to an engine example executable
// (e.g. mitiru_subsys_replay, mitiru_inspector), building it from the cached
// engine source when it is not already present.
func findOrBuildEngineExe(engineRoot, target, exeName string) (string, error) {
	dir := filepath.Join(engineRoot, "build", "examples", target)
	candidates := []string{
		filepath.Join(dir, exeName),
		filepath.Join(dir, "Debug", exeName),
		filepath.Join(dir, "Release", exeName),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	if err := ensureEngineConfigured(engineRoot); err != nil {
		return "", err
	}
	if err := buildEngineTarget(filepath.Join(engineRoot, "build"), target); err != nil {
		return "", err
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("built %s but no executable appeared under %s", target, dir)
}

// ensureEngineConfigured makes sure the cached engine has a configured build/
// tree (CEF fetched + cmake configured) so individual targets can be built.
// A no-op once CMakeCache.txt exists.
func ensureEngineConfigured(engineRoot string) error {
	buildDir := filepath.Join(engineRoot, "build")
	if _, err := os.Stat(filepath.Join(buildDir, "CMakeCache.txt")); err == nil {
		return nil
	}
	if err := engine.EnsureCEF(engineRoot, os.Stdout); err != nil {
		return fmt.Errorf("CEF setup failed: %w", err)
	}
	vcvars, err := build.FindVcvars64()
	if err != nil {
		return err
	}
	fmt.Println("Configuring engine (first subsystem build; cached afterwards)...")
	// MITIRU_BUILD_TESTS defaults ON for a top-level engine configure, but the
	// release snapshot ships examples/ (the subsystem targets) without tests/,
	// so keep tests off to avoid add_subdirectory(tests) on a missing dir.
	body := msvcPrelude(vcvars) + fmt.Sprintf(
		"cmake -S \"%s\" -B \"%s\" -G Ninja -DCMAKE_BUILD_TYPE=Debug -DMITIRU_BUILD_TESTS=OFF\r\n",
		engineRoot, buildDir)
	if err := runMsvcScript("mitiru_engine_configure", body); err != nil {
		return fmt.Errorf("configure engine at %s: %w", engineRoot, err)
	}
	return nil
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
	body := msvcPrelude(vcvars) + fmt.Sprintf(
		"cmake --build \"%s\" --config Debug --target %s\r\n", buildDir, target)
	if err := runMsvcScript("mitiru_target", body); err != nil {
		return fmt.Errorf("cmake --build %s --target %s: %w", buildDir, target, err)
	}
	return nil
}

// msvcPrelude emits the batch boilerplate that puts the VS Installer on PATH
// and activates vcvars64.bat before any cmake invocation.
func msvcPrelude(vcvars string) string {
	return fmt.Sprintf(
		"@echo off\r\n"+
			"set \"PATH=C:\\Program Files (x86)\\Microsoft Visual Studio\\Installer;%%PATH%%\"\r\n"+
			"call \"%s\" >NUL\r\n"+
			"if errorlevel 1 exit /b %%errorlevel%%\r\n",
		vcvars)
}

// runMsvcScript materialises body as a temp .bat and runs it via cmd /c,
// streaming output to the console. Writing a file sidesteps cmd quoting bugs
// when paths contain spaces.
func runMsvcScript(prefix, body string) error {
	tmp, err := os.CreateTemp("", prefix+"-*.bat")
	if err != nil {
		return fmt.Errorf("create temp batch: %w", err)
	}
	scriptPath := tmp.Name()
	defer os.Remove(scriptPath)
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp batch: %w", err)
	}
	tmp.Close()

	cmd := exec.Command("cmd", "/c", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
