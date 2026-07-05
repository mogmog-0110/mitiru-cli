package commands

import (
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tc-hib/winres"
)

func TestDistFaceArgs(t *testing.T) {
	if got := strings.Join(distFaceArgs("MyGame", false), " "); got != "--title MyGame" {
		t.Errorf("title only: %q", got)
	}
	if got := strings.Join(distFaceArgs("MyGame", true), " "); got != "--title MyGame --icon icon.ico" {
		t.Errorf("title+icon: %q", got)
	}
	// 空 name は --title を付けない (空 token が後続フラグを食う事故防止)。
	if got := strings.Join(distFaceArgs("", true), " "); got != "--icon icon.ico" {
		t.Errorf("no name: %q", got)
	}
}

func TestMtargsJoin(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"--no-cef", "--size", "800x600"}, "--no-cef --size 800x600"},
		{[]string{"--title", "My Game"}, `--title "My Game"`},          // 空白 token は quote
		{[]string{"--title", ""}, `--title ""`},                        // 空 token も quote
		{[]string{"--title", "Tab\there"}, "--title \"Tab\there\""},    // タブも quote
		{[]string{}, ""},
	}
	for _, c := range cases {
		if got := mtargsJoin(c.in); got != c.want {
			t.Errorf("mtargsJoin(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteLauncherQuotesSpacedArgs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.bat")
	if err := writeLauncher(p, "g/g.dll", []string{"--title", "My Game", "--icon", "icon.ico"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `g/g.dll --title "My Game" --icon icon.ico`) {
		t.Errorf("bat should quote spaced title:\n%s", string(b))
	}
}

func TestWriteExeLauncherQuotesSpacedArgs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mitiru_host.exe"), []byte("HOST"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeExeLauncher(dir, "my-game", "my_game/my_game.dll",
		[]string{"--title", "My Game"}); err != nil {
		t.Fatal(err)
	}
	m, _ := os.ReadFile(filepath.Join(dir, "my-game.mtargs"))
	if got := strings.TrimSpace(string(m)); got != `my_game/my_game.dll --title "My Game"` {
		t.Errorf("mtargs = %q", got)
	}
}

// 実 PE (テストバイナリ自身のコピー) への埋め込み → 読み戻しで round-trip を検証。
func TestEmbedExeIconRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PE round-trip is Windows-only")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip("test binary path unavailable")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "g.exe")
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, b, 0o755); err != nil {
		t.Fatal(err)
	}

	// 16x16 単色の実 ico を生成。
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	icon, err := winres.NewIconFromImages([]image.Image{img})
	if err != nil {
		t.Fatal(err)
	}
	ico := filepath.Join(dir, "icon.ico")
	f, err := os.Create(ico)
	if err != nil {
		t.Fatal(err)
	}
	if err := icon.SaveICO(f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := embedExeIcon(exe, ico); err != nil {
		t.Fatalf("embed: %v", err)
	}

	fr, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()
	rs, err := winres.LoadFromEXE(fr)
	if err != nil {
		t.Fatalf("embedded exe unreadable as PE: %v", err)
	}
	if _, err := rs.GetIcon(winres.ID(1)); err != nil {
		t.Errorf("icon group missing after embed: %v", err)
	}
}

func TestEmbedExeIconFailsGracefully(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "g.exe")
	ico := filepath.Join(dir, "icon.ico")

	// ico が無い → error (dist は警告して続行する契約)。
	if err := embedExeIcon(exe, ico); err == nil {
		t.Error("missing ico should fail")
	}

	// 不正 ico → error。exe は無傷のまま。
	if err := os.WriteFile(ico, []byte("not an ico"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("STUB"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := embedExeIcon(exe, ico); err == nil {
		t.Error("invalid ico should fail")
	}
	if b, _ := os.ReadFile(exe); string(b) != "STUB" {
		t.Errorf("exe must stay untouched on failure, got %q", string(b))
	}
	if _, err := os.Stat(exe + ".tmp"); err == nil {
		t.Error("temp exe should be cleaned up on failure")
	}
}
