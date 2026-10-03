package commands

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitMtargsInvertsMtargsJoin(t *testing.T) {
	tokens := []string{"g/g.dll", "--title", "My Game", "--size", "1280x720", "--game-name", "g"}
	if got := splitMtargs(mtargsJoin(tokens) + "\r\n"); !reflect.DeepEqual(got, tokens) {
		t.Errorf("splitMtargs(mtargsJoin(x)) = %q, want %q", got, tokens)
	}
}

func TestCleanEnvironDropsDevVariables(t *testing.T) {
	env := cleanEnviron([]string{
		`MITIRU_ENGINE_ROOT=E:\engine`, `Path=C:\VS\bin;C:\Windows\System32`, `VSINSTALLDIR=C:\VS`,
		`SystemRoot=D:\Win`, `LOCALAPPDATA=C:\Users\a\AppData\Local`,
	})
	joined := strings.Join(env, "\n")
	for _, gone := range []string{"MITIRU_ENGINE_ROOT", "VSINSTALLDIR", `C:\VS\bin`} {
		if strings.Contains(joined, gone) {
			t.Errorf("clean env still has %s:\n%s", gone, joined)
		}
	}
	if !strings.Contains(joined, `PATH=D:\Win\System32;D:\Win`) || !strings.Contains(joined, "LOCALAPPDATA=") {
		t.Errorf("clean env should keep LOCALAPPDATA and point PATH at SystemRoot only:\n%s", joined)
	}
}

func TestFindDistProblemsPicksMissingAssetLines(t *testing.T) {
	log := strings.Join([]string{
		"[mitiru_host] settings: C:\\x\\settings.json",
		"[mitiru] メッシュコライダーを読めない: g/assets/level.obj",
		"[clod] importing level.obj -> level.obj.clod (converts once)",
		"[mitiru] clod: モデル変換に失敗: モデルファイルがありません: g/assets/level.obj",
		"[mitiru_host] headless: 91 frames",
	}, "\n")
	got := findDistProblems(log)
	if len(got) != 2 || !strings.Contains(got[0], "読めない") || !strings.Contains(got[1], "ありません") {
		t.Errorf("findDistProblems = %q, want the 2 missing-file lines", got)
	}
}

// engine の新しい文は「mitiru: 」で始まり、欠けを 5 つの言い回しのどれかで言う。
func TestFindDistProblemsMatchesEngineNoticeWording(t *testing.T) {
	lines := []string{
		"mitiru: 音声ファイル a.wav を読めません。assets からの相対パスと、形式を確かめてください。",
		"mitiru: g/assets/level.obj が見つかりません。",
		"mitiru: g/assets/font.ttf を開けません。",
		"mitiru: assets/ui/main.rml がありません。",
		"mitiru: モデルの変換に失敗しました (g/assets/level.obj)。",
	}
	log := strings.Join(append([]string{"mitiru: 起動しました"}, lines...), "\n")
	got := findDistProblems(log)
	if !reflect.DeepEqual(got, lines) {
		t.Errorf("findDistProblems = %q, want every engine notice line", got)
	}
}

func TestIsBlankImage(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, paint func(*image.RGBA)) string {
		img := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for i := range img.Pix {
			img.Pix[i] = 255
		}
		paint(img)
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		return p
	}
	blank := write("blank.png", func(*image.RGBA) {})
	drawn := write("drawn.png", func(img *image.RGBA) { img.Set(16, 16, color.RGBA{255, 0, 0, 255}) })
	if b, err := isBlankImage(blank); err != nil || !b {
		t.Errorf("isBlankImage(blank) = %v, %v; want true", b, err)
	}
	if b, err := isBlankImage(drawn); err != nil || b {
		t.Errorf("isBlankImage(drawn) = %v, %v; want false", b, err)
	}
}
