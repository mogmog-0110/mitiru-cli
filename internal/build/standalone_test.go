package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmakeBuildCommand_TargetIsOptional(t *testing.T) {
	all := cmakeBuildCommand(`C:\p\build`, "Debug", "")
	if strings.Contains(all, "--target") {
		t.Fatalf("no target should mean no --target: %s", all)
	}
	one := cmakeBuildCommand(`C:\p\build`, "Release", "desktop_world")
	if !strings.HasSuffix(one, "--config Release --target desktop_world") {
		t.Fatalf("unexpected build command: %s", one)
	}
}

func TestFindStandaloneExe_PrefersBinThenRootThenConfigDirs(t *testing.T) {
	out := t.TempDir()
	if _, err := FindStandaloneExe(out, "Debug", "game"); err == nil {
		t.Fatal("an empty build dir must not yield an exe")
	}

	// The last candidate first: a multi-config layout.
	deep := filepath.Join(out, "Debug", "game.exe")
	if err := os.MkdirAll(filepath.Dir(deep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindStandaloneExe(out, "Debug", "game")
	if err != nil || got != deep {
		t.Fatalf("got %q, %v; want %q", got, err, deep)
	}

	// bin/ wins once it exists, which is where CMAKE_RUNTIME_OUTPUT_DIRECTORY puts it.
	top := filepath.Join(out, "bin", "game.exe")
	if err := os.MkdirAll(filepath.Dir(top), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(top, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = FindStandaloneExe(out, "Debug", "game")
	if err != nil || got != top {
		t.Fatalf("got %q, %v; want %q", got, err, top)
	}
}

func TestFindStandaloneExe_NamesEveryPlaceItLooked(t *testing.T) {
	out := t.TempDir()
	_, err := FindStandaloneExe(out, "Release", "dw")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"dw.exe", filepath.Join("bin", "dw.exe"), filepath.Join("Release", "dw.exe"), "[build] target"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should mention %q:\n%s", want, err)
		}
	}
}

func TestRunStandalone_RefusesAMissingCMakeLists(t *testing.T) {
	root := t.TempDir()
	_, err := RunStandalone(StandaloneOptions{
		ProjectRoot: root,
		SourceDir:   filepath.Join(root, "src"),
		Target:      "game",
	})
	if err == nil || !strings.Contains(err.Error(), "CMakeLists.txt") {
		t.Fatalf("expected a CMakeLists.txt error, got %v", err)
	}
}
