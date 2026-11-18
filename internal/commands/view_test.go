package commands

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestViewHostArgs(t *testing.T) {
	cases := []struct {
		name string
		opts viewOptions
		want []string
	}{
		{"モデルだけ", viewOptions{Models: []string{`C:\m\a.glb`}}, []string{`C:\m\a.glb`}},
		{"クリップとトゥーン",
			viewOptions{Models: []string{`C:\m\a.glb`, `C:\m\b.glb`}, Clip: "Walk", Toon: true},
			[]string{`C:\m\a.glb`, `C:\m\b.glb`, "--clip", "Walk", "--toon"}},
		{"表だけ", viewOptions{Models: []string{`C:\m\a.glb`}, Check: true},
			[]string{`C:\m\a.glb`, "--view-check"}},
		{"撮影",
			viewOptions{Models: []string{`C:\m\a.glb`}, CaptureDir: `C:\shots`, Frames: 30},
			[]string{`C:\m\a.glb`, "--headless-3d", "--backend", "dx12", "--capture-dir", `C:\shots`, "--max-frames", "30"}},
		{"撮影でフレーム数が 0 なら既定",
			viewOptions{Models: []string{`C:\m\a.glb`}, CaptureDir: `C:\shots`},
			[]string{`C:\m\a.glb`, "--headless-3d", "--backend", "dx12", "--capture-dir", `C:\shots`, "--max-frames", "120"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := viewHostArgs(c.opts); !reflect.DeepEqual(got, c.want) {
				t.Errorf("viewHostArgs = %q, want %q", got, c.want)
			}
		})
	}
}

func TestInterpretViewExit(t *testing.T) {
	var ee *ExitCodeError
	if err := interpretViewExit(0, true, "mitiru_host.exe"); err != nil {
		t.Errorf("exit 0 は成功のはず: %v", err)
	}
	// --check の 1 (警告あり) と 2 (読めない) は終了コードだけを伝える。host が理由を出し終えている
	for _, code := range []int{1, 2} {
		err := interpretViewExit(code, true, "mitiru_host.exe")
		if !errors.As(err, &ee) || ee.Code != code || ee.Message != "" {
			t.Errorf("--check の exit %d = %v, 終了コード %d でメッセージなしのはず", code, err, code)
		}
	}
	// --check なしの 1 は host の異常終了。2 だけは「読めない」として終了コードを保つ
	if err := interpretViewExit(1, false, "mitiru_host.exe"); err == nil || errors.As(err, &ee) {
		t.Errorf("--check なしの exit 1 は普通のエラーのはず: %v", err)
	}
	if err := interpretViewExit(2, false, "mitiru_host.exe"); !errors.As(err, &ee) || ee.Code != 2 {
		t.Errorf("--check なしの exit 2 = %v, 終了コード 2 のはず", err)
	}
	crash := interpretViewExit(int(int32(-1073741819)), false, `C:\x\mitiru_host.exe`)
	if crash == nil || !strings.Contains(crash.Error(), "0xC0000005") {
		t.Errorf("アクセス違反の説明が出るはず: %v", crash)
	}
}

func TestResolveViewModels(t *testing.T) {
	dir := t.TempDir()
	glb := filepath.Join(dir, "fox.glb")
	if err := os.WriteFile(glb, []byte("glTF"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveViewModels([]string{glb})
	if err != nil || len(got) != 1 || got[0] != glb {
		t.Errorf("resolveViewModels = %v, %v", got, err)
	}
	if _, err := resolveViewModels([]string{filepath.Join(dir, "none.glb")}); err == nil || !strings.Contains(err.Error(), "がありません") {
		t.Errorf("無い glb はエラーのはず: %v", err)
	}
	png := filepath.Join(dir, "a.png")
	_ = os.WriteFile(png, []byte("x"), 0o644)
	if _, err := resolveViewModels([]string{png}); err == nil || !strings.Contains(err.Error(), "glb でも gltf でもありません") {
		t.Errorf("glb 以外はエラーのはず: %v", err)
	}
}

func TestValidateViewOptions(t *testing.T) {
	if err := validateViewOptions(viewOptions{Check: true, CaptureDir: `C:\s`, Frames: 10}); err == nil {
		t.Error("--check と --capture-dir は一緒に使えないはず")
	}
	if err := validateViewOptions(viewOptions{Frames: 0}); err == nil {
		t.Error("--frames 0 は誤りのはず")
	}
	if err := validateViewOptions(viewOptions{Frames: 120}); err != nil {
		t.Errorf("既定は通るはず: %v", err)
	}
}

func TestFindViewHostRejectsOldEngine(t *testing.T) {
	root := t.TempDir() // apps/mitiru_view が無いエンジン
	_, err := findViewHost(root)
	if err == nil || !strings.Contains(err.Error(), "0.54.0 以降") {
		t.Errorf("古いエンジンは 0.54.0 以降が要ると案内するはず: %v", err)
	}
}

func TestViewHostCandidatesNeedsViewDll(t *testing.T) {
	root := t.TempDir()
	hostDir := filepath.Join(root, "build", "apps", "mitiru_host")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(hostDir, "mitiru_host.exe")
	if err := os.WriteFile(host, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := viewHostCandidates(root); len(got) != 0 {
		t.Errorf("mitiru_view の DLL が無い host は数えないはず: %v", got)
	}
	viewDir := filepath.Join(hostDir, "mitiru_view")
	_ = os.MkdirAll(viewDir, 0o755)
	_ = os.WriteFile(filepath.Join(viewDir, "mitiru_view.dll"), []byte("x"), 0o644)
	if got := viewHostCandidates(root); len(got) != 1 || got[0] != host {
		t.Errorf("DLL がそろえば host が見つかるはず: %v", got)
	}
}
