package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestComposeNoticesEngineOnly(t *testing.T) {
	project, data := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(data, distNoticesFile), "RmlUi MIT\n")
	got, err := composeNotices(project, data)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "RmlUi MIT\n" {
		t.Errorf("engine-only notices = %q, want the engine file verbatim", got)
	}
}

func TestComposeNoticesGameBeforeEngine(t *testing.T) {
	project, data := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(project, distNoticesFile), "Game font OFL")
	writeTestFile(t, filepath.Join(data, distNoticesFile), "RmlUi MIT\n")
	got, err := composeNotices(project, data)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	gi, si, ei := strings.Index(s, "Game font OFL"), strings.Index(s, strings.Repeat("=", 72)),
		strings.Index(s, "RmlUi MIT")
	if gi != 0 || si <= gi || ei <= si {
		t.Errorf("want game, separator, engine in that order, got %q", s)
	}
	if !strings.HasPrefix(s, "Game font OFL\r\n") {
		t.Errorf("game text without a trailing newline must end its line before the separator: %q", s)
	}
}

func TestComposeNoticesMissingEngineFails(t *testing.T) {
	project, data := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(project, distNoticesFile), "Game font OFL\n")
	_, err := composeNotices(project, data)
	if err == nil {
		t.Fatal("missing engine notices must fail dist")
	}
	if !strings.Contains(err.Error(), distNoticesFile) || !strings.Contains(err.Error(), "Python") {
		t.Errorf("error should name the file and how to get it: %v", err)
	}
}

// deploy dir の engine の表記は copyDeploy で data/ に写るが、配布物ではトップだけに置かれる。
func TestBundleNoticesAtTopLevelOnly(t *testing.T) {
	deploy, project, bundle := t.TempDir(), t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(deploy, "mitiru_host.exe"), "HOST")
	writeTestFile(t, filepath.Join(deploy, distNoticesFile), "RmlUi MIT\n")
	writeTestFile(t, filepath.Join(deploy, "my_game", "my_game.dll"), "DLL")
	writeTestFile(t, filepath.Join(project, distNoticesFile), "Game font OFL\n")
	data := filepath.Join(bundle, "data")
	if _, err := copyDeploy(deploy, data, "my_game"); err != nil {
		t.Fatal(err)
	}
	if _, err := writeBundleNotices(project, bundle, data); err != nil {
		t.Fatal(err)
	}
	top, err := os.ReadFile(filepath.Join(bundle, distNoticesFile))
	if err != nil {
		t.Fatalf("bundle lacks the top-level %s: %v", distNoticesFile, err)
	}
	if !strings.Contains(string(top), "Game font OFL") || !strings.Contains(string(top), "RmlUi MIT") {
		t.Errorf("top-level notices must hold both game and engine parts: %q", top)
	}
	if _, err := os.Stat(filepath.Join(data, distNoticesFile)); !os.IsNotExist(err) {
		t.Errorf("data/%s must not be duplicated (err=%v)", distNoticesFile, err)
	}
}
