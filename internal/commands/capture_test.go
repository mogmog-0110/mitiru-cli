package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureHostArgsRunsHeadlessAndPlaysTheScript(t *testing.T) {
	got := strings.Join(captureHostArgs(`game\game.dll`, `C:\out`, 30, 122, `C:\walk.txt`), " ")
	for _, want := range []string{
		`game\game.dll --headless-3d --backend dx12`,
		`--capture-dir C:\out --capture-every 30`,
		`--max-frames 122`,
		`--input-script C:\walk.txt`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
	if strings.Contains(strings.Join(captureHostArgs("g.dll", "d", 1, 3, ""), " "), "--input-script") {
		t.Error("no script means no --input-script")
	}
}

func TestClearCapturesRemovesOnlyFramePNGs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"frame_000000.png", "frame_000001.png", "keep.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearCaptures(dir); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 1 || filepath.Base(left[0]) != "keep.png" {
		t.Errorf("left %v, want only keep.png", left)
	}
}
