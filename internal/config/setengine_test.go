package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetEnginePreservesComments(t *testing.T) {
	const src = `[project]
name = "demo"
version = "0.1.0"
engine = "0.6.0"   # pinned engine

[lofi]
# 当時のドット質感を固定仕様にする
enabled = true
bits    = "5,6,5"  # RGB565
`
	dir := t.TempDir()
	path := filepath.Join(dir, "mitiru.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetEngine(path, "0.7.0"); err != nil {
		t.Fatalf("SetEngine: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.Contains(got, `engine = "0.7.0"`) {
		t.Errorf("engine not bumped to 0.7.0:\n%s", got)
	}
	if strings.Contains(got, `engine = "0.6.0"`) {
		t.Errorf("old engine version still present:\n%s", got)
	}
	// 行末コメントとセクションコメントが残っていること。
	for _, want := range []string{"# pinned engine", "# 当時のドット質感を固定仕様にする", `bits    = "5,6,5"`, "# RGB565"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost content %q after rewrite:\n%s", want, got)
		}
	}
	// 再 parse でき、値が反映されていること。
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Project.Engine != "0.7.0" {
		t.Errorf("reloaded engine = %q; want 0.7.0", cfg.Project.Engine)
	}
}

func TestSetEngineNoEngineLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mitiru.toml")
	if err := os.WriteFile(path, []byte("[project]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetEngine(path, "0.7.0"); err == nil {
		t.Error("expected error when no engine line present")
	}
}
