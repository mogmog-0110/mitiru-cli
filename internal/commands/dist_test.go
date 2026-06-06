package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDistBundleName(t *testing.T) {
	cases := map[string]string{
		"my-game":   "my-game",
		"My Game!":  "My_Game", // 末尾 _ は trim される
		"だっしゅ":      "game",    // 非 ASCII は _、trim 後は空 → "game"
		"":          "game",
		"_leading_": "leading",
		"a/b\\c":    "a_b_c",
	}
	for in, want := range cases {
		got := distBundleName(in)
		// 全 _ のケースは trim で空 → "game"
		if strings.Trim(want, "_") == "" {
			want = "game"
		}
		if got != want {
			t.Errorf("distBundleName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteLauncher(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.bat")
	if err := writeLauncher(p, filepath.Join("g", "g.dll"), []string{"--no-cef", "--size", "800x600"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "mitiru_host.exe g") || !strings.Contains(s, "g.dll --no-cef --size 800x600") {
		t.Errorf("launcher missing expected command line:\n%s", s)
	}
	if !strings.Contains(s, `cd /d "%~dp0"`) {
		t.Errorf("launcher should cd to its own dir:\n%s", s)
	}
}

func TestCopyDeployFiltersCefAndJunk(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	// deploy を模す: host + CEF + game subdir + junk。
	write := func(rel string) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mitiru_host.exe")
	write("libcef.dll")
	write("d3dcompiler_47.dll")
	write(filepath.Join("locales", "ja.pak"))
	write(filepath.Join("my_game", "my_game.dll"))
	write(filepath.Join("my_game", "assets", "scene.html"))
	write("mitiru_host.pdb")     // junk
	write("CMakeCache.txt")      // build 産物 (allowlist 外)
	write("build.ninja")         // build 産物
	write("mitiru_inspector.exe") // 他ツール exe (配布不要)
	write(filepath.Join("mitiru-engine", "CMakeLists.txt")) // engine source dir

	// noCef=true → CEF と locales と build 産物が除外される。
	if _, err := copyDeploy(src, dst, "my_game", true); err != nil {
		t.Fatal(err)
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dst, rel))
		return err == nil
	}
	for _, keep := range []string{
		"mitiru_host.exe", "d3dcompiler_47.dll",
		filepath.Join("my_game", "my_game.dll"),
		filepath.Join("my_game", "assets", "scene.html"),
	} {
		if !exists(keep) {
			t.Errorf("noCef: %s should be kept", keep)
		}
	}
	for _, drop := range []string{
		"libcef.dll", filepath.Join("locales", "ja.pak"), "mitiru_host.pdb",
		"CMakeCache.txt", "build.ninja", "mitiru_inspector.exe",
		filepath.Join("mitiru-engine", "CMakeLists.txt"),
	} {
		if exists(drop) {
			t.Errorf("noCef: %s should be dropped", drop)
		}
	}

	// noCef=false → CEF は残るが build 産物は除外。
	dst2 := t.TempDir()
	if _, err := copyDeploy(src, dst2, "my_game", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst2, "libcef.dll")); err != nil {
		t.Errorf("cef build: libcef.dll should be kept")
	}
	for _, drop := range []string{"mitiru_host.pdb", "CMakeCache.txt", "mitiru_inspector.exe"} {
		if _, err := os.Stat(filepath.Join(dst2, drop)); err == nil {
			t.Errorf("cef build: %s should be dropped", drop)
		}
	}
}

func TestWriteExeLauncher(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mitiru_host.exe"), []byte("HOST"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeExeLauncher(dir, "my-game", filepath.Join("my_game", "my_game.dll"), []string{"--no-cef"}); err != nil {
		t.Fatal(err)
	}
	// exe は host のコピー。
	b, _ := os.ReadFile(filepath.Join(dir, "my-game.exe"))
	if string(b) != "HOST" {
		t.Errorf("exe should be a copy of mitiru_host.exe, got %q", string(b))
	}
	// sidecar は dll(スラッシュ) + 引数。
	m, _ := os.ReadFile(filepath.Join(dir, "my-game.mtargs"))
	if got := strings.TrimSpace(string(m)); got != "my_game/my_game.dll --no-cef" {
		t.Errorf("mtargs = %q, want %q", got, "my_game/my_game.dll --no-cef")
	}
}
