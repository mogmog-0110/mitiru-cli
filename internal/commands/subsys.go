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

// locateSubsystem は `name` に対応する standalone な subsystem 実行ファイルを探す
// (例 "renderer" -> mitiru_subsys_renderer.exe)。subsystem launch コマンド
// (renderer / audio / input / scene / replay) がバックの exe を発見する方法の
// single source of truth。
//
// 探索順:
//  1. $MITIRU_HOME/bin/mitiru_subsys_<name>.exe   (installed layout)
//  2. <mitiru CLI exe のディレクトリ>/mitiru_subsys_<name>.exe (release zip、
//     subsystem は mitiru.exe と同梱)
//  3. <engine source>/build/examples/mitiru_subsys_<name>/...  (dev tree、
//     engine repo がそこで build する — Debug / Release の subdir を含む)
//
// miss 時は target の build 方法を伝える clean な error を返す。
func locateSubsystem(name string) (string, error) {
	exeName := subsysExeName(name)

	// (1) $MITIRU_HOME/bin 配下の installed layout。
	if home := os.Getenv("MITIRU_HOME"); home != "" {
		c := filepath.Join(home, "bin", exeName)
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	// (2) 実行中の CLI 実行ファイルと同じ場所 (release zip layout)。
	if self, err := os.Executable(); err == nil {
		c := filepath.Join(filepath.Dir(self), exeName)
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	// (3) Dev / cache flow: engine は source のみ配布なので prebuilt exe は通常存在
	// しない。プロジェクトが pin する engine を解決し、単一の subsystem target を
	// on-demand で build する (初回に configure + cache される)。
	engineRoot, err := resolveEngineRoot()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return findOrBuildEngineExe(engineRoot, "mitiru_subsys_"+name, exeName)
}

// resolveEngineRoot は現在のプロジェクトが (mitiru.toml 経由で) pin する cache 済み
// engine source tree を返す。意図的に "latest" GitHub release には fall back しない:
// tag は存在しても publish された Release は存在しないことがあり、subsystem は
// プロジェクトが build する version と一致する必要があるため。
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

// findOrBuildEngineExe は engine の example 実行ファイル (例 mitiru_subsys_replay、
// mitiru_inspector) への path を返す。まだ存在しない場合は cache 済み engine source
// から build する。
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

// ensureEngineConfigured は cache 済み engine が configure 済みの build/ tree
// (CEF 取得 + cmake configure 済み) を持つことを保証し、個別 target を build できる
// ようにする。CMakeCache.txt が存在すれば no-op。
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
	// MITIRU_BUILD_TESTS は top-level engine configure では ON がデフォルトだが、
	// release snapshot は examples/ (subsystem target 群) を tests/ なしで配布するため、
	// 存在しない dir への add_subdirectory(tests) を避けるべく tests は off にする。
	body := msvcPrelude(vcvars) + fmt.Sprintf(
		"cmake -S \"%s\" -B \"%s\" -G Ninja -DCMAKE_BUILD_TYPE=Debug -DMITIRU_BUILD_TESTS=OFF\r\n",
		engineRoot, buildDir)
	if err := runMsvcScript("mitiru_engine_configure", body); err != nil {
		return fmt.Errorf("configure engine at %s: %w", engineRoot, err)
	}
	return nil
}

// subsysExeName は subsystem の platform 固有な実行ファイル名を返す。engine target は
// Windows では .exe として配布される。他 platform では bare な target 名を使い、
// locator が依然として valid な path を組み立てられるようにする。
func subsysExeName(name string) string {
	base := "mitiru_subsys_" + name
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// launchSubsystem は `name` の subsystem exe を探して実行し、stdio を forward して
// `args` を child へ渡す。exit code は error として表面化させ、CLI が subsystem の
// status を mirror できるようにする。
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

// buildEngineTarget は engine 自身の build tree 内で単一の CMake target を build する。
// 先に MSVC 環境 (vcvars64) を通す。pre-built な exe がまだ存在しないとき
// audio / inspector コマンドが使う。
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

// msvcPrelude は、VS Installer を PATH に置き、cmake 呼び出し前に vcvars64.bat を
// 有効化する batch の boilerplate を出力する。
func msvcPrelude(vcvars string) string {
	return fmt.Sprintf(
		"@echo off\r\n"+
			"set \"PATH=C:\\Program Files (x86)\\Microsoft Visual Studio\\Installer;%%PATH%%\"\r\n"+
			"call \"%s\" >NUL\r\n"+
			"if errorlevel 1 exit /b %%errorlevel%%\r\n",
		vcvars)
}

// runMsvcScript は body を一時 .bat として書き出し cmd /c 経由で実行し、出力を
// console に stream する。file に書くことで、path に空白を含むときの cmd quoting bug
// を回避する。
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
