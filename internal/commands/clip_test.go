package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPlanClipOutputs(t *testing.T) {
	cases := []struct {
		name            string
		rec, out        string
		gif, noMp4      bool
		avi, mp4, gifTo string
	}{
		{"既定は録画の名前の mp4", `C:\g\bug_1.mtrr`, "", false, false, "bug_1.avi", "bug_1.mp4", ""},
		{"mp4 と GIF", `C:\g\bug_1.mtrr`, "", true, false, "bug_1.avi", "bug_1.mp4", "bug_1.gif"},
		{"拡張子が gif なら GIF だけ", `C:\g\bug_1.mtrr`, "a.gif", false, false, "a.avi", "", "a.gif"},
		{"拡張子が avi なら AVI だけ", `C:\g\bug_1.mtrr`, "a.avi", false, false, "a.avi", "", ""},
		{"知らない拡張子は名前の一部で mp4", `C:\g\bug_1.mtrr`, "out.v2", false, false, "out.v2.avi", "out.v2.mp4", ""},
		{"--no-mp4", `C:\g\bug_1.mtrr`, "", true, true, "bug_1.avi", "", "bug_1.gif"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			avi, mp4, gif := planClipOutputs(c.rec, c.out, c.gif, c.noMp4)
			if avi != c.avi || mp4 != c.mp4 || gif != c.gifTo {
				t.Errorf("= (%q, %q, %q), want (%q, %q, %q)", avi, mp4, gif, c.avi, c.mp4, c.gifTo)
			}
		})
	}
}

func TestClipHostArgs(t *testing.T) {
	got := clipHostArgs(`C:\g\game.dll`, `C:\g\bug_1.mtrr`, `C:\o\a.avi`, false)
	want := []string{`C:\g\game.dll`, "--replay-test", `C:\g\bug_1.mtrr`, "--export-video", `C:\o\a.avi`, "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("clipHostArgs = %q, want %q", got, want)
	}
	got3d := clipHostArgs("d.dll", "r.mtrr", "a.avi", true)
	if tail := got3d[len(got3d)-3:]; !reflect.DeepEqual(tail, []string{"--headless-3d", "--backend", "dx12"}) {
		t.Errorf("--3d の引数 = %q", tail)
	}
}

func TestJudgeClipHostResult(t *testing.T) {
	noFrames := map[string]any{"verdict": "FAIL", "reason": "no_frames_compared"}
	diverged := map[string]any{"verdict": "FAIL", "reason": "diverged"}
	corrupt := map[string]any{"verdict": "FAIL", "reason": "replay_file_corrupt"}

	if usable, warn := judgeClipHostResult(0, nil, true); !usable || warn != "" {
		t.Errorf("exit 0 で AVI があれば使える: %v %q", usable, warn)
	}
	if usable, warn := judgeClipHostResult(1, noFrames, true); !usable || !strings.Contains(warn, "途中の状態を持たない") {
		t.Errorf("no_frames_compared は注意つきで使える: %v %q", usable, warn)
	}
	if usable, warn := judgeClipHostResult(1, diverged, true); !usable || !strings.Contains(warn, "ずれました") {
		t.Errorf("diverged は注意つきで使える: %v %q", usable, warn)
	}
	if usable, _ := judgeClipHostResult(1, corrupt, true); usable {
		t.Error("replay_file_corrupt は使えない")
	}
	if usable, _ := judgeClipHostResult(0, nil, false); usable {
		t.Error("AVI が無ければ exit 0 でも使えない")
	}
	if usable, _ := judgeClipHostResult(1, nil, true); usable {
		t.Error("判定が読めない失敗は使えない")
	}
}

func TestParseClipVerdict(t *testing.T) {
	out := "mitiru: replaying\r\n{\"unrelated\":1}\r\n{\"verdict\":\"FAIL\",\"reason\":\"diverged\"}\r\n"
	v := parseClipVerdict(out)
	if v == nil || v["reason"] != "diverged" {
		t.Errorf("verdict を持つ最後の JSON 行を返すはず: %v", v)
	}
	if parseClipVerdict("mitiru: nothing\n{broken}\n") != nil {
		t.Error("読めない行だけなら nil のはず")
	}
}

func TestFindLatestRecording(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	old := filepath.Join(a, "bug_old.mtrr")
	newer := filepath.Join(b, "bug_new.mtrr")
	other := filepath.Join(a, "notes.mtrr")
	for _, p := range []string{old, newer, other} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	_ = os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour))
	_ = os.Chtimes(newer, now, now)
	_ = os.Chtimes(other, now.Add(time.Hour), now.Add(time.Hour)) // bug_ で始まらないので数えない
	if got := findLatestRecording([]string{a, b, a}); got != newer {
		t.Errorf("findLatestRecording = %q, want %q", got, newer)
	}
	if got := findLatestRecording([]string{t.TempDir()}); got != "" {
		t.Errorf("録画が無ければ空のはず: %q", got)
	}
}

func TestClipFFmpegArgs(t *testing.T) {
	mp4 := strings.Join(clipMp4Args("a.avi", "a.mp4"), " ")
	for _, want := range []string{"-i a.avi", "libx264", "scale=out_range=tv,format=yuv420p", "+bitexact", "-movflags +faststart a.mp4"} {
		if !strings.Contains(mp4, want) {
			t.Errorf("mp4 の引数に %q が無い: %s", want, mp4)
		}
	}
	gif := strings.Join(clipGifArgs("a.avi", "a.gif"), " ")
	for _, want := range []string{"fps=15", "min(480,iw)", "palettegen", "-an", "a.gif"} {
		if !strings.Contains(gif, want) {
			t.Errorf("GIF の引数に %q が無い: %s", want, gif)
		}
	}
}

func TestClipTrimKeepsRunes(t *testing.T) {
	got := clipTrim("あいうえお", 7)
	if got != "あい" {
		t.Errorf("clipTrim = %q, 文字の途中で切らず \"あい\" のはず", got)
	}
	if clipTrim("  ab  ", 10) != "ab" {
		t.Error("短ければ前後の空白を除くだけのはず")
	}
}
