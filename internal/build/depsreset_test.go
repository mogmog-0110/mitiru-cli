package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cl が /showIncludes の行を書くコードページと CMake が読むコードページを同じにしないと、
// ninja が header の依存を 1 つも拾えない。chcp は vcvars より前で、入出力の両方を変える。
func TestVcvarsPreludeSetsUTF8CodePageBeforeVcvars(t *testing.T) {
	p := vcvarsPrelude(`C:\VS\vcvars64.bat`)
	chcp := strings.Index(p, "chcp 65001 >NUL\r\n")
	call := strings.Index(p, `call "C:\VS\vcvars64.bat"`)
	if chcp < 0 || call < 0 || chcp > call {
		t.Fatalf("prelude must run chcp 65001 before vcvars:\n%s", p)
	}
}

func TestResetCompilerDetectionRemovesDetectionAndDeps(t *testing.T) {
	out := t.TempDir()
	detect := filepath.Join(out, "CMakeFiles", "3.31.6-msvc6")
	keep := filepath.Join(out, "CMakeFiles", "t_game.dir")
	for _, d := range []string{detect, keep} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(detect, "CMakeCXXCompiler.cmake"), "prefix")
	writeTestFile(t, filepath.Join(out, ".ninja_deps"), "deps")
	writeTestFile(t, filepath.Join(out, "CMakeCache.txt"), "cache")

	if err := resetCompilerDetection(out); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{detect, filepath.Join(out, ".ninja_deps")} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s must be removed", gone)
		}
	}
	for _, kept := range []string{keep, filepath.Join(out, "CMakeCache.txt")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s must be kept: %v", kept, err)
		}
	}
}

func TestResetCompilerDetectionOnFreshDir(t *testing.T) {
	if err := resetCompilerDetection(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("a never-configured dir must not fail: %v", err)
	}
}

func TestDepsSchemeMarksOnlyTheCurrentScheme(t *testing.T) {
	out := t.TempDir()
	if depsSchemeCurrent(out) {
		t.Fatal("a dir without the marker was configured under an older code page rule")
	}
	if err := writeDepsScheme(out); err != nil {
		t.Fatal(err)
	}
	if !depsSchemeCurrent(out) {
		t.Fatal("the written marker must be current")
	}
	writeTestFile(t, filepath.Join(out, depsSchemeFile), "older\n")
	if depsSchemeCurrent(out) {
		t.Error("another scheme name must not count as current")
	}
}
