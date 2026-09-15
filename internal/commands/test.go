package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

// mitiru test は `tests/*.cpp` を MSVC (`cl /std:c++20 /utf-8`) で 1 本ずつ組み、
// 実行して exit code を集計するサブコマンド (E5)。oscar-rythm の
// `tests/test_logic.cpp`（Catch2 不要・自前 CHECK マクロ + main() で exit code を返す
// 純ロジックテスト）が要望元で、そのビルドコマンド
// (`cl /nologo /std:c++20 /utf-8 /EHsc /I src tests\test_logic.cpp`) をそのまま
// サブコマンド化したもの。`tests/` 配下の `.cpp` はそれぞれ独立した実行可能ファイルとして
// ビルドされ、main() を持つ単体テストランナー (Catch2 でも自前ハーネスでも可) を想定する。
func newTestCommand() *cobra.Command {
	var (
		filter    string
		release   bool
		extraIncl []string
	)
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Compile and run tests/*.cpp with MSVC",
		Long: `Compiles each tests/*.cpp in the current project with cl (/std:c++20 /utf-8)
and runs the resulting executable, aggregating pass/fail by exit code.

Each tests/*.cpp is expected to be a self-contained executable (own main()) —
either a Catch2 runner or a hand-rolled harness (e.g. oscar-rythm's
tests/test_logic.cpp, which needs no engine or Catch2 dependency at all).
Source under the project's src/ directory is on the include path by default.

Examples:
  mitiru test                  # build + run every tests/*.cpp
  mitiru test --filter logic   # only tests/*logic*.cpp
  mitiru test --release        # /O2 instead of /Od /Zi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(filter, release, extraIncl)
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "only build/run tests whose filename contains this substring")
	cmd.Flags().BoolVar(&release, "release", false, "compile with /O2 instead of /Od /Zi")
	cmd.Flags().StringArrayVar(&extraIncl, "include", nil, "extra -I directory (repeatable)")
	return cmd
}

type testCaseResult struct {
	name     string
	buildErr string // 非空なら compile 失敗 (実行はしていない)
	exitCode int
	ranAt    time.Duration
	output   string
}

func runTest(filter string, release bool, extraIncl []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	manifestPath, projectRoot, err := config.FindManifest(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(manifestPath)
	if err != nil {
		return err
	}

	testsDir := filepath.Join(projectRoot, "tests")
	files, err := findTestSources(testsDir, filter)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("mitiru test: %s に *.cpp が見つかりません (filter=%q)\n", testsDir, filter)
		return nil
	}

	clPath, vsEnv, err := resolveClToolchain()
	if err != nil {
		return fmt.Errorf("MSVC toolchain の解決に失敗: %w", err)
	}

	outDir := filepath.Join(projectRoot, "build", "tests")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outDir, err)
	}

	includeDirs := []string{filepath.Join(projectRoot, "src")}
	includeDirs = append(includeDirs, extraIncl...)
	if !cfg.Standalone() && cfg.Project.Engine != "" {
		// engine ヘッダに依存するテストのために -I <engine>/include も足すが、
		// ここで新規に取得しには行かない (`mitiru test` の主目的は engine を
		// 経由しない速い純ロジック検証。未取得なら build/run が先に済ませている
		// はず)。既にローカル cache にあるものだけを使う。
		if root, ok := cachedEngineRoot(cfg.Project.Engine); ok {
			includeDirs = append(includeDirs, filepath.Join(root, "include"))
		}
	}

	results := make([]testCaseResult, 0, len(files))
	for _, src := range files {
		results = append(results, runOneTest(clPath, vsEnv, src, outDir, includeDirs, release))
	}

	return summarizeTests(results)
}

// cachedEngineRoot は version (mitiru.toml の "engine =") に対応する engine source が
// 既に ~/.mitiru/cache/ に展開済みなら、その root を返す。"latest" の解決や
// download は行わない (network を伴う EnsureSource とはここで役割を分ける)。
func cachedEngineRoot(version string) (string, bool) {
	v := strings.TrimSpace(version)
	if v == "" || v == "latest" {
		return "", false
	}
	if v[0] != 'v' && v[0] != 'V' {
		v = "v" + v
	}
	cacheRoot, err := engine.CacheRoot()
	if err != nil {
		return "", false
	}
	versionDir := filepath.Join(cacheRoot, "engine-"+v)
	if _, statErr := os.Stat(filepath.Join(versionDir, ".mitiru-cache-ok")); statErr != nil {
		return "", false
	}
	entries, err := os.ReadDir(versionDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(versionDir, e.Name())
		if _, statErr := os.Stat(filepath.Join(candidate, "CMakeLists.txt")); statErr == nil {
			return candidate, true
		}
	}
	return "", false
}

// findTestSources は testsDir 直下 (非再帰) の *.cpp を名前順に集める。
func findTestSources(testsDir, filter string) ([]string, error) {
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", testsDir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".cpp") {
			continue
		}
		if filter != "" && !strings.Contains(e.Name(), filter) {
			continue
		}
		out = append(out, filepath.Join(testsDir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// resolveClToolchain は vcvars64.bat を評価して cl.exe と、それが必要とする
// PATH/INCLUDE/LIB を持つ環境を返す。build.VsToolchainPath (PATH のみ、Debug CRT
// loader 解決用) では標準ヘッダ (<algorithm> 等) を探す INCLUDE が欠け cl が
// C1083 で落ちるため、ここでは 3 変数とも自前で評価する。vswhere ベースの探索
// (FindVcvars64) 自体は共通のものを再利用し、パスを直値で持たない。
func resolveClToolchain() (clPath string, env []string, err error) {
	vcvars, err := build.FindVcvars64()
	if err != nil {
		return "", nil, err
	}
	const marker = "MITIRU_TEST_VSENV:"
	script := "@echo off\r\n" +
		// vcvars64.bat 自身が vswhere.exe を PATH 上に期待するため、素の環境では
		// 見つからず途中で失敗する。VS Installer dir を先に足しておく
		// (build.evalVcvarsPath と同じ回避策)。
		"set \"PATH=C:\\Program Files (x86)\\Microsoft Visual Studio\\Installer;%PATH%\"\r\n" +
		fmt.Sprintf("call \"%s\" >NUL 2>&1\r\n", vcvars) +
		"if errorlevel 1 exit /b %errorlevel%\r\n" +
		"echo " + marker + "PATH=%PATH%\r\n" +
		"echo " + marker + "INCLUDE=%INCLUDE%\r\n" +
		"echo " + marker + "LIB=%LIB%\r\n"

	tmp, err := os.CreateTemp("", "mitiru_test_vsenv-*.bat")
	if err != nil {
		return "", nil, fmt.Errorf("create vsenv batch: %w", err)
	}
	scriptPath := tmp.Name()
	defer func() { _ = os.Remove(scriptPath) }()
	if _, werr := tmp.WriteString(script); werr != nil {
		_ = tmp.Close()
		return "", nil, fmt.Errorf("write vsenv batch: %w", werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return "", nil, fmt.Errorf("close vsenv batch: %w", cerr)
	}

	out, err := exec.Command("cmd", "/c", scriptPath).Output()
	if err != nil {
		return "", nil, fmt.Errorf("evaluate vcvars64.bat: %w", err)
	}

	vars := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, marker) {
			continue
		}
		kv := strings.SplitN(strings.TrimPrefix(line, marker), "=", 2)
		if len(kv) == 2 {
			vars[kv[0]] = kv[1]
		}
	}
	pathVal := vars["PATH"]
	if pathVal == "" {
		return "", nil, fmt.Errorf("vcvars64.bat output did not contain a PATH value")
	}

	env = os.Environ()
	env = build.PrependPath(env, pathVal)
	if inc := vars["INCLUDE"]; inc != "" {
		env = append(env, "INCLUDE="+inc)
	}
	if lib := vars["LIB"]; lib != "" {
		env = append(env, "LIB="+lib)
	}

	for _, dir := range strings.Split(pathVal, ";") {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, "cl.exe")
		if st, statErr := os.Stat(candidate); statErr == nil && !st.IsDir() {
			return candidate, env, nil
		}
	}
	return "", nil, fmt.Errorf("cl.exe が vcvars64.bat の PATH 上に見つかりません")
}

func runOneTest(clPath string, env []string, src, outDir string, includeDirs []string, release bool) testCaseResult {
	name := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	exePath := filepath.Join(outDir, name+".exe")
	objDir := outDir + string(filepath.Separator)

	args := []string{"/nologo", "/std:c++20", "/utf-8", "/EHsc"}
	if release {
		args = append(args, "/O2", "/DNDEBUG")
	} else {
		args = append(args, "/Od", "/Zi", "/MDd")
	}
	for _, inc := range includeDirs {
		args = append(args, "/I", inc)
	}
	args = append(args, src, "/Fe:"+exePath, "/Fo:"+objDir)

	buildCmd := exec.Command(clPath, args...)
	buildCmd.Env = env
	buildCmd.Dir = outDir
	buildOut, buildErr := buildCmd.CombinedOutput()
	if buildErr != nil {
		return testCaseResult{name: name, buildErr: string(buildOut), exitCode: -1}
	}

	start := time.Now()
	runCmd := exec.Command(exePath)
	runCmd.Dir = outDir
	runOut, runErr := runCmd.CombinedOutput()
	elapsed := time.Since(start)

	code := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	return testCaseResult{name: name, exitCode: code, ranAt: elapsed, output: string(runOut)}
}

func summarizeTests(results []testCaseResult) error {
	failed := 0
	fmt.Println()
	for _, r := range results {
		if r.buildErr != "" {
			failed++
			fmt.Printf("  [BUILD FAIL] %s\n", r.name)
			fmt.Println(indentLines(r.buildErr))
			continue
		}
		status := "PASS"
		if r.exitCode != 0 {
			status = "FAIL"
			failed++
		}
		fmt.Printf("  [%s] %s (exit=%d, %.0fms)\n", status, r.name, r.exitCode, r.ranAt.Seconds()*1000)
		if status == "FAIL" {
			fmt.Println(indentLines(r.output))
		}
	}
	fmt.Printf("\n%d tests, %d failed\n", len(results), failed)
	if failed > 0 {
		return fmt.Errorf("%d/%d tests failed", failed, len(results))
	}
	return nil
}

func indentLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}
