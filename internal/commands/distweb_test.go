package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

func TestRejectWebConflicts(t *testing.T) {
	none := func(string) bool { return false }
	if err := rejectWebConflicts(none); err != nil {
		t.Errorf("何も指定しなければ通るはず: %v", err)
	}
	changed := func(name string) bool { return name == "sign" || name == "onefile" }
	err := rejectWebConflicts(changed)
	if err == nil || !strings.Contains(err.Error(), "--onefile") || !strings.Contains(err.Error(), "--sign") {
		t.Errorf("衝突したフラグの名前を並べるはず: %v", err)
	}
	// pack は既定が true なので、--pack だけでは衝突にしない
	if err := rejectWebConflicts(func(n string) bool { return n == "pack" || n == "zip" }); err != nil {
		t.Errorf("--pack と --zip は --web でも使える: %v", err)
	}
}

func TestWebIgnoredNotices(t *testing.T) {
	cfg := &config.ProjectConfig{}
	if got := webIgnoredNotices(cfg); len(got) != 0 {
		t.Errorf("設定が無ければ知らせは無いはず: %v", got)
	}
	cfg.Engine.Features = []string{"dawn"}
	cfg.Render.NeedsSet = true
	got := strings.Join(webIgnoredNotices(cfg), "\n")
	if !strings.Contains(got, "[engine] features") || !strings.Contains(got, "[render] needs") {
		t.Errorf("2 つの設定を知らせるはず: %q", got)
	}
}

func TestWebBuildScript(t *testing.T) {
	got := webBuildScript(`E:\emsdk`, `C:\p\build\web\cmake`, `C:\p\build\web\out`, "pong")
	for _, want := range []string{
		`call "E:\emsdk\emsdk_env.bat"`,
		`-DCMAKE_TOOLCHAIN_FILE="E:/emsdk/upstream/emscripten/cmake/Modules/Platform/Emscripten.cmake"`,
		`cmake -S "C:\p\build\web\cmake" -B "C:\p\build\web\out" -G Ninja`,
		`cmake --build "C:\p\build\web\out" --target pong`,
		"-DCMAKE_BUILD_TYPE=Release",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("batch に無い: %s\n%s", want, got)
		}
	}
}

func TestInstallWebBundle(t *testing.T) {
	built := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "pong-web")
	if _, err := installWebBundle(built, bundle); err == nil || !strings.Contains(err.Error(), "index.html がありません") {
		t.Errorf("index.html が無ければ断るはず: %v", err)
	}
	for name, body := range map[string]string{"index.html": "<html>", "index.js": "js", "index.wasm": "wasm"} {
		_ = os.WriteFile(filepath.Join(built, name), []byte(body), 0o644)
	}
	_ = os.MkdirAll(bundle, 0o755)
	_ = os.WriteFile(filepath.Join(bundle, "stale.txt"), []byte("old"), 0o644) // 配布先は作り直す
	n, err := installWebBundle(built, bundle)
	if err != nil || n != 3 {
		t.Fatalf("installWebBundle = %d, %v", n, err)
	}
	if fileExists(filepath.Join(bundle, "stale.txt")) {
		t.Error("前から配布先にあったファイルは消えるはず")
	}
	if !fileExists(filepath.Join(bundle, "index.wasm")) {
		t.Error("index.wasm が写っていない")
	}
}

func TestEmsdkMissingErrorIsActionable(t *testing.T) {
	err := emsdkMissingError(os.ErrNotExist)
	for _, want := range []string{"約 2 GB", "EMSDK", "勝手には入れません"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("emsdk が無いときの案内に %q が無い: %v", want, err)
		}
	}
}
