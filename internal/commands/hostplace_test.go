//go:build windows

package commands

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func deepHostPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), strings.Repeat("深", 130), "mitiru_host.exe")
}

// 起動する呼び出しは、深すぎる置き場を error で止める。
func TestCheckHostPlace_LaunchStopsOnDeepPlace(t *testing.T) {
	var warn bytes.Buffer
	if err := checkHostPlace(deepHostPath(t), true, &warn); err == nil {
		t.Fatal("a launch from a too-deep place must stop")
	}
	if warn.Len() != 0 {
		t.Errorf("a stop is reported through the error, got stderr %q", warn.String())
	}
}

// 組むだけの呼び出し (mitiru build) は止めず、警告を出す。
func TestCheckHostPlace_BuildOnlyWarns(t *testing.T) {
	var warn bytes.Buffer
	if err := checkHostPlace(deepHostPath(t), false, &warn); err != nil {
		t.Fatalf("build-only must not stop: %v", err)
	}
	if !strings.Contains(warn.String(), "警告") || !strings.Contains(warn.String(), "止めた") {
		t.Errorf("warning = %q", warn.String())
	}
}

func TestCheckHostPlace_ShallowIsSilent(t *testing.T) {
	var warn bytes.Buffer
	if err := checkHostPlace(filepath.Join(t.TempDir(), "mitiru_host.exe"), true, &warn); err != nil || warn.Len() != 0 {
		t.Fatalf("shallow place must pass silently: err=%v out=%q", err, warn.String())
	}
}

// 配布物の置き先 (--out) が深いと、組む前に止まる。まだ存在しない置き先でも測れる。
func TestCheckDistPlace_DeepOutStopsBeforeBuilding(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "mitiru.toml")
	writeTestFile(t, manifest, "[project]\nname = \"game\"\nengine = \"0.45.0\"\n")
	old := distOut
	defer func() { distOut = old }()

	distOut = filepath.Join(dir, "dist")
	if err := checkDistPlace(manifest); err != nil {
		t.Fatalf("shallow out must pass: %v", err)
	}
	distOut = filepath.Join(dir, strings.Repeat("深", 130))
	if err := checkDistPlace(manifest); err == nil {
		t.Fatal("a deep --out must stop")
	}
}
