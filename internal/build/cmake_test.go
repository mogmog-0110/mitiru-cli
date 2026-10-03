package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProject は Configure が要求する最小のプロジェクト/エンジン構成を temp に作る。
func fakeProject(t *testing.T) (projectRoot, engineRoot string) {
	t.Helper()
	projectRoot = t.TempDir()
	engineRoot = t.TempDir()

	mustWrite := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(projectRoot, "src", "main.cpp"), "// game\n")
	mustWrite(filepath.Join(engineRoot, "apps", "mitiru_host", "main.cpp"), "// host\n")
	return projectRoot, engineRoot
}

// 旧 engine snapshot (examples/ layout) でも host source を解決できること。
func TestConfigure_FallsBackToExamplesLayout(t *testing.T) {
	projectRoot := t.TempDir()
	engineRoot := t.TempDir()
	hostMain := filepath.Join(engineRoot, "examples", "mitiru_host", "main.cpp")
	if err := os.MkdirAll(filepath.Dir(hostMain), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostMain, []byte("// host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcMain := filepath.Join(projectRoot, "src", "main.cpp")
	if err := os.MkdirAll(filepath.Dir(srcMain), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcMain, []byte("// game\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmake := generatedCMake(t, projectRoot, engineRoot)
	if !strings.Contains(cmake, "examples/mitiru_host/main.cpp") {
		t.Error("generated CMakeLists.txt should reference examples/ host source on old engines")
	}
}

func generatedCMake(t *testing.T, projectRoot, engineRoot string) string {
	t.Helper()
	srcDir, _, err := Configure(Options{
		ProjectRoot: projectRoot,
		ProjectName: "my-first-game",
		EngineRoot:  engineRoot,
		Config:      "Debug",
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(srcDir, "CMakeLists.txt"))
	if err != nil {
		t.Fatalf("read generated CMakeLists.txt: %v", err)
	}
	return string(b)
}

// パッドの DLL が host の隣へ deploy されること。engine が mitiru_deploy_sdl3 を出せば SDL3、古い engine は SDL2。
func TestConfigure_TemplateDeploysGamepadDll(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)

	for _, want := range []string{
		// ゲームの target も渡し、ゲームの DLL が link した物を第三者ライセンスの表記に載せる
		"if(COMMAND mitiru_deploy_runtime)\n    mitiru_deploy_runtime(mitiru_host my_first_game)",
		"elseif(COMMAND mitiru_deploy_sdl3)",
		"mitiru_deploy_sdl3(mitiru_host)",
		"elseif(WIN32 AND TARGET SDL2::SDL2)",
		"$<TARGET_FILE:SDL2::SDL2>",
		"$<TARGET_FILE_DIR:mitiru_host>/SDL2.dll",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMakeLists.txt missing %q", want)
		}
	}
}

// 今の engine (RmlUi) では CEF を deploy せず、RML が引く engine の RCSS を host の隣へ置くこと。
func TestConfigure_TemplateDeploysRmlUiNotCEF(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)

	for _, banned := range []string{"MitiruCef.cmake", "mitiru_add_cef_game", "mitiru_runtime"} {
		if strings.Contains(cmake, banned) {
			t.Errorf("generated CMakeLists.txt must not mention %q for a CEF-free engine", banned)
		}
	}
	if !strings.Contains(cmake, `"${MITIRU_ENGINE_ROOT}/assets/ui"`) {
		t.Error("generated CMakeLists.txt does not deploy the engine RCSS (assets/ui)")
	}
}

// CEF 世代の engine (cmake/MitiruCef.cmake がある) を pin したプロジェクトは、これまでどおり CEF を deploy すること。
func TestConfigure_TemplateKeepsCEFForLegacyEngine(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	legacy := filepath.Join(engineRoot, "cmake", "MitiruCef.cmake")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("# cef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmake := generatedCMake(t, projectRoot, engineRoot)
	if !strings.Contains(cmake, "mitiru_add_cef_game(mitiru_host)") {
		t.Error("legacy engine: generated CMakeLists.txt missing mitiru_add_cef_game(mitiru_host)")
	}
}

// #56: vcpkg applocal の pwsh stale 化を是正する engine モジュールを、project() より前に
// include していること。project() で vcpkg toolchain が pwsh を find_program するため、
// 是正は必ずそれより前に走らせる必要がある。
func TestConfigure_TemplateIncludesPwshFixBeforeProject(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)

	idxInclude := strings.Index(cmake, "VcpkgPwshFix.cmake")
	if idxInclude < 0 {
		t.Fatal("generated CMakeLists.txt missing VcpkgPwshFix.cmake include (#56)")
	}
	// 行頭の project( を実宣言とみなす (コメント中の "project()" 言及に引っかからないように)。
	idxProject := strings.Index(cmake, "\nproject(")
	if idxProject < 0 {
		t.Fatal("generated CMakeLists.txt missing project() call")
	}
	if idxInclude > idxProject {
		t.Errorf("VcpkgPwshFix.cmake include must precede project() (#56): include@%d project@%d",
			idxInclude, idxProject)
	}
}

// 配布物のトップに単独で置くランチャと、単一 exe の器は VC ランタイムの DLL を読まない (静的 CRT)。
func TestConfigure_LaunchersUseStaticCRT(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	for _, app := range []string{"mitiru_start", "mitiru_selfrun", "mitiru_selfpack"} {
		p := filepath.Join(engineRoot, "apps", app, "main.cpp")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("// app\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmake := generatedCMake(t, projectRoot, engineRoot)
	for _, target := range []string{"mitiru_start", "mitiru_selfrun"} {
		want := "set_property(TARGET " + target + ` PROPERTY MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")`
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMakeLists.txt missing %q", want)
		}
	}
}
