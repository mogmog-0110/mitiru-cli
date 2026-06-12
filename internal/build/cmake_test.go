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
	mustWrite(filepath.Join(engineRoot, "examples", "mitiru_host", "main.cpp"), "// host\n")
	return projectRoot, engineRoot
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

// SDL2 backend 有効時に SDL2.dll が host の隣へ deploy されること (0xC0000135 即死対策)。
func TestConfigure_TemplateDeploysSDL2(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)

	for _, want := range []string{
		"if(WIN32 AND TARGET SDL2::SDL2)",
		"$<TARGET_FILE:SDL2::SDL2>",
		"$<TARGET_FILE_DIR:mitiru_host>/SDL2.dll",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMakeLists.txt missing %q", want)
		}
	}
}

// CEF deploy (mitiru_add_cef_game) が引き続き存在すること。
func TestConfigure_TemplateDeploysCEF(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)

	if !strings.Contains(cmake, "mitiru_add_cef_game(mitiru_host)") {
		t.Error("generated CMakeLists.txt missing mitiru_add_cef_game(mitiru_host)")
	}
}
