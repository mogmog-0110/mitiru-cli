//go:build windows

package install

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindMitiruSourceInAcceptsReleaseAssetName(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "mitiru_windows_amd64.exe")
	touch(t, want)

	got, err := findMitiruSourceIn([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestFindMitiruSourceInPrefersMitiruExe(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "mitiru.exe")
	touch(t, want)
	touch(t, filepath.Join(dir, "mitiru_windows_amd64.exe"))

	got, err := findMitiruSourceIn([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestFindMitiruSourceInFallsBackToLaterDir(t *testing.T) {
	beside, cwd := t.TempDir(), t.TempDir()
	want := filepath.Join(cwd, "mitiru_windows_amd64.exe")
	touch(t, want)

	got, err := findMitiruSourceIn([]string{beside, cwd})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestFindMitiruSourceInSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "mitiru.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := findMitiruSourceIn([]string{dir}); err == nil {
		t.Fatal("ディレクトリを mitiru.exe として拾った")
	}
}

func TestFindMitiruSourceInReportsMissing(t *testing.T) {
	if _, err := findMitiruSourceIn([]string{t.TempDir()}); err == nil {
		t.Fatal("何も無いのにエラーにならなかった")
	}
}
