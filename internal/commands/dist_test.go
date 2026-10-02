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
	if err := writeLauncher(p, filepath.Join("g", "g.dll"), []string{"--fixed-size", "--size", "800x600"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "mitiru_host.exe g") || !strings.Contains(s, "g.dll --fixed-size --size 800x600") {
		t.Errorf("launcher missing expected command line:\n%s", s)
	}
	if !strings.Contains(s, `cd /d "%~dp0data"`) {
		t.Errorf("launcher should cd into the data/ runtime dir:\n%s", s)
	}
	if !strings.Contains(s, "if errorlevel 1 pause") {
		t.Errorf("launcher should pause on error so the console doesn't just flash:\n%s", s)
	}
}

func TestCopyDeployFiltersJunk(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	// deploy を模す: host + UI の RCSS と書体 + game subdir + junk。
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
	write("d3dcompiler_47.dll")
	write("SDL2.dll") // vcpkg SDL2: host が import 依存。KEEP 必須
	write(filepath.Join("assets", "ui", "base.rcss"))
	write(filepath.Join("assets", "fonts", "MPLUSRounded1c-Regular.ttf"))
	write(filepath.Join("assets", "fonts", "OFL.txt"))
	write(filepath.Join("assets", "fonts", "Pacifico-Regular.ttf")) // UI が使わない書体
	write(filepath.Join("my_game", "my_game.dll"))
	write(filepath.Join("my_game", "assets", "ui", "main.rml"))
	write("mitiru_host.pdb")                                // junk
	write("CMakeCache.txt")                                 // build 産物 (allowlist 外)
	write("build.ninja")                                    // build 産物
	write("mitiru_inspector.exe")                           // 他ツール exe (配布不要)
	write(filepath.Join("mitiru-engine", "CMakeLists.txt")) // engine source dir

	if _, err := copyDeploy(src, dst, "my_game"); err != nil {
		t.Fatal(err)
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dst, rel))
		return err == nil
	}
	// host + runtime DLL + RmlUi の RCSS と既定書体 + game dir は残る。
	for _, keep := range []string{
		"mitiru_host.exe", "d3dcompiler_47.dll", "SDL2.dll",
		filepath.Join("assets", "ui", "base.rcss"),
		filepath.Join("assets", "fonts", "MPLUSRounded1c-Regular.ttf"),
		filepath.Join("assets", "fonts", "OFL.txt"),
		filepath.Join("my_game", "my_game.dll"),
		filepath.Join("my_game", "assets", "ui", "main.rml"),
	} {
		if !exists(keep) {
			t.Errorf("%s should be kept", keep)
		}
	}
	// junk / build 産物 / 他ツール / engine source は持ち込まない。
	for _, drop := range []string{
		"mitiru_host.pdb", "CMakeCache.txt", "build.ninja", "mitiru_inspector.exe",
		filepath.Join("mitiru-engine", "CMakeLists.txt"),
		filepath.Join("assets", "fonts", "Pacifico-Regular.ttf"),
	} {
		if exists(drop) {
			t.Errorf("%s should be dropped", drop)
		}
	}
}

func TestWriteExeLauncher(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mitiru_host.exe"), []byte("HOST"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeExeLauncher(dir, "my-game", filepath.Join("my_game", "my_game.dll"), []string{"--fixed-size"}); err != nil {
		t.Fatal(err)
	}
	// exe は host のコピー。
	b, _ := os.ReadFile(filepath.Join(dir, "my-game.exe"))
	if string(b) != "HOST" {
		t.Errorf("exe should be a copy of mitiru_host.exe, got %q", string(b))
	}
	// sidecar は dll(スラッシュ) + 引数。
	m, _ := os.ReadFile(filepath.Join(dir, "my-game.mtargs"))
	if got := strings.TrimSpace(string(m)); got != "my_game/my_game.dll --fixed-size" {
		t.Errorf("mtargs = %q, want %q", got, "my_game/my_game.dll --fixed-size")
	}
}

// RmlUi は pack を読まないので、--pack でも assets/ui/ はバラ置きで残り、pack にも入らない。
func TestPackKeepsUIDirLoose(t *testing.T) {
	assets := filepath.Join(t.TempDir(), "assets")
	for _, rel := range []string{"ui/main.rml", "ui/hud.rcss", "sprites/a.png", "audio/pop.wav"} {
		p := filepath.Join(assets, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := packAssets(assets, filepath.Join(filepath.Dir(assets), "assets.mtpak"), "g/assets")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("packed %d files, want 2 (sprites + audio, not ui/)", n)
	}
	if err := removePackedAssets(assets); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(assets, "ui", "main.rml")); err != nil {
		t.Errorf("assets/ui/main.rml must stay loose: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assets, "sprites")); !os.IsNotExist(err) {
		t.Errorf("packed assets must be removed, sprites/ still there (err=%v)", err)
	}
}
