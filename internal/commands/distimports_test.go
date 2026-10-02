package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestIsDebugCRT(t *testing.T) {
	debug := []string{"ucrtbased.dll", "VCRUNTIME140D.dll", "vcruntime140_1d.dll", "MSVCP140D.dll",
		"msvcp140d_atomic_wait.dll", "concrt140d.dll"}
	release := []string{"vcruntime140.dll", "VCRUNTIME140_1.dll", "msvcp140_atomic_wait.dll", "d3d12.dll", "ucrtbase.dll"}
	for _, n := range debug {
		if !isDebugCRT(n) {
			t.Errorf("isDebugCRT(%q) = false, want true", n)
		}
	}
	for _, n := range release {
		if isDebugCRT(n) {
			t.Errorf("isDebugCRT(%q) = true, want false", n)
		}
	}
}

// 配布物の data/ を模し、PE の import は表から返す。
func fakeDist(t *testing.T, files []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckDistImportsSortsProblems(t *testing.T) {
	data := fakeDist(t, []string{"mitiru_host.exe", "SDL3.dll", "game/game.dll", "game/helper.dll"})
	imports := map[string][]string{
		"mitiru_host.exe": {"KERNEL32.dll", "VCRUNTIME140.dll", "MSVCP140.dll", "onnxruntime.dll", "SDL3.dll"},
		"SDL3.dll":        {"KERNEL32.dll"},
		"game/game.dll":   {"VCRUNTIME140.dll", "helper.dll", "ucrtbased.dll", "nowhere.dll"},
		"game/helper.dll": {"KERNEL32.dll"},
	}
	fakeImports := func(path string) ([]string, error) {
		rel, _ := filepath.Rel(data, path)
		return imports[filepath.ToSlash(rel)], nil
	}
	// onnxruntime.dll は開発機の System32 にあっても同梱が要る
	system := func(name string) bool {
		low := strings.ToLower(name)
		return low == "kernel32.dll" || low == "onnxruntime.dll" || low == "vcruntime140.dll"
	}
	rep, err := checkDistImports(data, fakeImports, system)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"msvcp140.dll", "vcruntime140.dll"}; !reflect.DeepEqual(rep.MissingVC, want) {
		t.Errorf("MissingVC = %v, want %v", rep.MissingVC, want)
	}
	if len(rep.DebugCRT) != 1 || rep.DebugCRT[0].DLL != "ucrtbased.dll" || rep.DebugCRT[0].Importer != "game/game.dll" {
		t.Errorf("DebugCRT = %+v, want ucrtbased.dll from game/game.dll", rep.DebugCRT)
	}
	var missing []string
	for _, p := range rep.Missing {
		missing = append(missing, p.DLL)
	}
	if want := []string{"nowhere.dll", "onnxruntime.dll"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("Missing = %v, want %v (helper.dll sits next to its importer, SDL3.dll is bundled)", missing, want)
	}
}

func TestReadPEImportsReadsARealExe(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PE は Windows の System32 で確かめる")
	}
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "where.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skip(err)
	}
	libs, err := readPEImports(exe)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range libs {
		if strings.EqualFold(l, "KERNEL32.dll") || strings.HasPrefix(strings.ToLower(l), "api-ms-win-") {
			found = true
		}
	}
	if !found {
		t.Errorf("where.exe imports %v, want KERNEL32.dll or an API set", libs)
	}
}

func TestDistDropsDevOnlyTopLevelFiles(t *testing.T) {
	drop := []string{"compile_commands.json", "mitiru_build.json", "game_navbake_Release.stamp",
		"level.obj.clod.tmp", "build.ninja", "mitiru_tool.exe"}
	keep := []string{"mitiru_host.exe", "SDL3.dll", "onnxruntime.dll", "launch.mtargs", "data.pak"}
	for _, f := range drop {
		if !isDistDropTopLevel(f) {
			t.Errorf("isDistDropTopLevel(%q) = false, want true (dev only)", f)
		}
	}
	for _, f := range keep {
		if isDistDropTopLevel(f) {
			t.Errorf("isDistDropTopLevel(%q) = true, want false", f)
		}
	}
}
