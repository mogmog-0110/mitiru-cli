package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeEmsdk(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tc := filepath.Join(dir, filepath.FromSlash(emscriptenToolchainRel))
	if err := os.MkdirAll(filepath.Dir(tc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tc, []byte("# toolchain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "emsdk_env.bat"), []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFindEmsdk(t *testing.T) {
	good := fakeEmsdk(t)
	empty := t.TempDir()

	got, err := FindEmsdk([]string{filepath.ToSlash(empty), filepath.ToSlash(good)})
	if err != nil || got != filepath.FromSlash(filepath.ToSlash(good)) {
		t.Errorf("FindEmsdk = %q, %v: ツールチェーンのある 2 つ目を返すはず", got, err)
	}
	// ツールチェーンだけで emsdk_env.bat が無い場所は、組む batch が失敗するので選ばない
	broken := fakeEmsdk(t)
	_ = os.Remove(filepath.Join(broken, "emsdk_env.bat"))
	if _, err := FindEmsdk([]string{filepath.ToSlash(broken)}); err == nil {
		t.Error("emsdk_env.bat の無い emsdk は見つからない扱いのはず")
	}
	_, err = FindEmsdk([]string{filepath.ToSlash(empty)})
	if err == nil || !strings.Contains(err.Error(), "(emsdk) が見つかりません") || !strings.Contains(err.Error(), filepath.ToSlash(empty)) {
		t.Errorf("探した場所を並べた error のはず: %v", err)
	}
}

func TestEmsdkCandidatesOrder(t *testing.T) {
	env := func(k string) string {
		if k == "EMSDK" {
			return `D:\tools\emsdk`
		}
		return ""
	}
	got := strings.Join(EmsdkCandidates(env, `C:\Users\me`), "|")
	want := `D:\tools\emsdk|E:/emsdk|C:/emsdk|C:/Users/me/emsdk`
	if got != want {
		t.Errorf("EmsdkCandidates = %s, want %s", got, want)
	}
	if got := EmsdkCandidates(func(string) string { return "" }, ""); len(got) != 2 {
		t.Errorf("EMSDK も home も無ければ 2 つのはず: %v", got)
	}
}

func TestRenderWebCMake(t *testing.T) {
	text, err := RenderWebCMake(WebOptions{
		ProjectName: "my game",
		EngineRoot:  `C:\engine`,
		MainCpp:     `C:\proj\src\main.cpp`,
		AssetsDir:   `C:\proj\assets`,
		Title:       `Pong "Deluxe"`,
		Width:       640,
		Height:      480,
		SingleFile:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`add_subdirectory("${MITIRU_ENGINE_ROOT}" mitiru-engine)`,
		`include("${MITIRU_ENGINE_ROOT}/cmake/MitiruWebGame.cmake")`,
		"mitiru_add_web_game(my_game\n",
		`MAIN "C:/proj/src/main.cpp"`,
		`ASSETS "C:/proj/assets"`,
		`TITLE "Pong \"Deluxe\""`,
		"WIDTH 640",
		"HEIGHT 480",
		"SINGLE_FILE)",
		`target_include_directories(my_game PRIVATE "C:/proj/src")`,
		"MITIRU_BUILD_TESTS OFF",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("生成した CMake に無い:\n%s\n--- 全文 ---\n%s", want, text)
		}
	}
	if strings.Contains(text, "SHARED") {
		t.Error("ブラウザ向けは SHARED のゲーム DLL を作らない")
	}
}

func TestRenderWebCMakeOptionalParts(t *testing.T) {
	text, err := RenderWebCMake(WebOptions{ProjectName: "tiny", EngineRoot: "E:/e", MainCpp: "E:/p/src/main.cpp"})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"ASSETS", "WIDTH", "HEIGHT", "SINGLE_FILE"} {
		if strings.Contains(text, absent) {
			t.Errorf("指定しなければ %s は書かないはず:\n%s", absent, text)
		}
	}
	if !strings.Contains(text, `TITLE "tiny"`) {
		t.Errorf("題名が無ければプロジェクト名を使うはず:\n%s", text)
	}
}

func TestCheckWebEngine(t *testing.T) {
	root := t.TempDir()
	if err := CheckWebEngine(root); err == nil || !strings.Contains(err.Error(), "0.54.0 以降") {
		t.Errorf("古いエンジンは案内つきで断るはず: %v", err)
	}
	for _, rel := range []string{webGameModule, "web/web_main.cpp", "web/dist_shell.html"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	if err := CheckWebEngine(root); err != nil {
		t.Errorf("そろえば通るはず: %v", err)
	}
}

func TestCountNetworkURLs(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<script src=index.js></script>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "index.js"), []byte(`fetch("https://cdn.example/x"); // http://a`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "index.wasm"), []byte("http://ignored"), 0o644)
	n, err := CountNetworkURLs(dir)
	if err != nil || n != 2 {
		t.Errorf("CountNetworkURLs = %d, %v, 2 のはず (.wasm は数えない)", n, err)
	}
}
