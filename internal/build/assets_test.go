package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSyncAssets_MissingSrcIsNoop(t *testing.T) {
	dst := t.TempDir()
	n, err := SyncAssets(filepath.Join(t.TempDir(), "no_such_assets"), dst)
	if err != nil || n != 0 {
		t.Errorf("SyncAssets(missing) = (%d, %v), want (0, nil)", n, err)
	}
}

func TestSyncAssets_CopiesNewAndNestedFiles(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	writeFile(t, filepath.Join(src, "scene.html"), "<html>v1</html>")
	writeFile(t, filepath.Join(src, "css", "hud.css"), "body{}")

	n, err := SyncAssets(src, dst)
	if err != nil {
		t.Fatalf("SyncAssets: %v", err)
	}
	if n != 2 {
		t.Errorf("copied = %d, want 2", n)
	}
	got, err := os.ReadFile(filepath.Join(dst, "css", "hud.css"))
	if err != nil || string(got) != "body{}" {
		t.Errorf("nested copy missing/broken: %q, %v", got, err)
	}
}

func TestSyncAssets_SkipsUnchanged(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "a.html"), "same")

	if _, err := SyncAssets(src, dst); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	n, err := SyncAssets(src, dst)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n != 0 {
		t.Errorf("second sync copied %d files, want 0 (unchanged skip)", n)
	}
}

func TestSyncAssets_RecopiesOnChange(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	srcFile := filepath.Join(src, "scene.html")
	writeFile(t, srcFile, "v1")
	if _, err := SyncAssets(src, dst); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// 内容変更 (サイズ変化) → 再 copy。
	writeFile(t, srcFile, "v2-longer")
	n, err := SyncAssets(src, dst)
	if err != nil || n != 1 {
		t.Fatalf("size-change sync = (%d, %v), want (1, nil)", n, err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "scene.html"))
	if string(got) != "v2-longer" {
		t.Errorf("dst content = %q, want %q", got, "v2-longer")
	}

	// 同サイズで mtime のみ前進 → 再 copy。
	writeFile(t, srcFile, "v3-longer") // 同 9 bytes
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(srcFile, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	n, err = SyncAssets(src, dst)
	if err != nil || n != 1 {
		t.Errorf("mtime-change sync = (%d, %v), want (1, nil)", n, err)
	}
}

func TestNeedsCopy(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	dstPath := filepath.Join(dir, "dst.bin")
	writeFile(t, srcPath, "abc")
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// dst 不在 → copy 要。
	if !needsCopy(srcInfo, dstPath) {
		t.Error("needsCopy(missing dst) = false, want true")
	}

	// 同サイズ + dst の方が新しい → skip。
	writeFile(t, dstPath, "abc")
	if needsCopy(srcInfo, dstPath) {
		t.Error("needsCopy(same size, newer dst) = true, want false")
	}

	// サイズ違い → copy 要。
	writeFile(t, dstPath, "abcdef")
	if !needsCopy(srcInfo, dstPath) {
		t.Error("needsCopy(size mismatch) = false, want true")
	}
}
