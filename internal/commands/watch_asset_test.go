package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestSyncAssetCopiesToDeploy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "assets")
	dep := filepath.Join(dir, "deploy", "proj", "assets")
	if err := os.MkdirAll(filepath.Join(src, "levels"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(src, "levels", "stage1.lvl")
	want := ":tiles\n###\n"
	if err := os.WriteFile(srcFile, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &gameState{srcAssetsDir: src, deployAssetsDir: dep}
	if err := s.syncAsset(srcFile); err != nil {
		t.Fatalf("syncAsset: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dep, "levels", "stage1.lvl"))
	if err != nil {
		t.Fatalf("deploy file not created: %v", err)
	}
	if string(got) != want {
		t.Errorf("deploy content = %q, want %q", got, want)
	}
}

func TestShouldSyncAsset(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "assets")
	if err := os.MkdirAll(filepath.Join(src, "levels"), 0o755); err != nil {
		t.Fatal(err)
	}
	lvl := filepath.Join(src, "levels", "stage1.lvl")
	swap := filepath.Join(src, "levels", "stage1.lvl~")
	for _, f := range []string{lvl, swap} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &gameState{srcAssetsDir: src, deployAssetsDir: filepath.Join(dir, "deploy")}

	cases := []struct {
		name string
		path string
		op   fsnotify.Op
		want bool
	}{
		{"real .lvl write", lvl, fsnotify.Write, true},
		{"editor swap file", swap, fsnotify.Write, false},
		{"directory", filepath.Join(src, "levels"), fsnotify.Write, false},
		{"outside assets", filepath.Join(dir, "outside.lvl"), fsnotify.Write, false},
		{"chmod only", lvl, fsnotify.Chmod, false},
	}
	for _, c := range cases {
		if got := s.shouldSyncAsset(c.path, c.op); got != c.want {
			t.Errorf("%s: shouldSyncAsset = %v, want %v", c.name, got, c.want)
		}
	}
}
