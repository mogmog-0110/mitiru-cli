package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeSccache(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "sccache.exe")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MITIRU_SCCACHE", exe)
	return filepath.ToSlash(exe)
}

func TestCompilerCacheDefinesUseZ7ForDebugInfoConfigs(t *testing.T) {
	exe := fakeSccache(t)
	for _, cfg := range []string{"Debug", "RelWithDebInfo"} {
		got := strings.Join(compilerCacheDefines(cfg, ""), " ")
		for _, want := range []string{
			"CMAKE_CXX_COMPILER_LAUNCHER=" + exe,
			"CMAKE_POLICY_DEFAULT_CMP0141=NEW",
			"CMAKE_MSVC_DEBUG_INFORMATION_FORMAT=Embedded",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q が無い: %s", cfg, want, got)
			}
		}
	}
	if got := strings.Join(compilerCacheDefines("Release", ""), " "); strings.Contains(got, "Embedded") {
		t.Errorf("Release に /Z7 は要らない: %s", got)
	}
}

func TestCompilerCacheDefinesOffSwitches(t *testing.T) {
	fakeSccache(t)
	if got := compilerCacheDefines("Debug", "none"); got != nil {
		t.Errorf("cache = none で切れていない: %v", got)
	}
	t.Setenv("MITIRU_SCCACHE", "off")
	if got := compilerCacheDefines("Debug", ""); got != nil {
		t.Errorf("MITIRU_SCCACHE=off で切れていない: %v", got)
	}
	t.Setenv("MITIRU_SCCACHE", filepath.Join(t.TempDir(), "missing.exe"))
	if got := compilerCacheDefines("Debug", ""); got != nil {
		t.Errorf("存在しない exe を指したら使わない: %v", got)
	}
}
