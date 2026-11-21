package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/spf13/cobra"
)

// clip の終了コード (engine の tools/mitiru_clip.py と同じ)。
const (
	clipExitUsage  = 2
	clipExitHost   = 3
	clipExitFFmpeg = 4

	clipGifWidth = 480
	clipGifFPS   = 15
)

// clipBitExact は、同じ ffmpeg なら同じバイト列になるよう、時刻とバージョンの書き込みを切り、符号化を 1 スレッドにする。
var clipBitExact = []string{"-map_metadata", "-1", "-fflags", "+bitexact", "-flags:v", "+bitexact",
	"-flags:a", "+bitexact", "-threads", "1"}

type clipOptions struct {
	Rec     string // 空なら一番新しい bug_*.mtrr
	Out     string
	Gif     bool
	NoMp4   bool
	KeepAvi bool
	ThreeD  bool
	FFmpeg  string
}

func newClipCommand() *cobra.Command {
	var opts clipOptions

	cmd := &cobra.Command{
		Use:   "clip [rec.mtrr]",
		Short: "Turn a recording saved with F11 (bug_*.mtrr) into an mp4 / GIF",
		Long: `Replay a recording (.mtrr) with this project's host and game DLL, export
it as an AVI, and convert it to mp4 with ffmpeg (--gif for a GIF too).

With no recording given, the newest bug_*.mtrr in the current folder, next to
the game DLL, or next to mitiru_host.exe is used. F11 in the running game
saves one next to mitiru_host.exe. Use the same DLL that made the recording:
the replay is deterministic, so the same recording and DLL give the same video.

  mitiru clip                       newest F11 recording → <name>.mp4 here
  mitiru clip bug_1.mtrr -o a.gif   GIF (480 px wide, 15 fps, no sound)
  mitiru clip --gif                 mp4 and GIF
  mitiru clip --3d                  draw 3D and UI on the GPU (--headless-3d --backend dx12)

Without ffmpeg on PATH it stops at the AVI and exits 0 (winget install
Gyan.FFmpeg). Exit codes: 2 bad arguments, 3 the host could not write the AVI,
4 ffmpeg failed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Rec = args[0]
			}
			return runClip(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Out, "out", "o", "",
		"output; the extension (.mp4 / .gif / .avi) picks what to make (default: <recording name>.mp4 in the current folder)")
	cmd.Flags().BoolVar(&opts.Gif, "gif", false, "also make a GIF")
	cmd.Flags().BoolVar(&opts.NoMp4, "no-mp4", false, "do not make an mp4")
	cmd.Flags().BoolVar(&opts.KeepAvi, "keep-avi", false, "keep the AVI after making the mp4 / GIF")
	cmd.Flags().BoolVar(&opts.ThreeD, "3d", false, "draw 3D and UI on the GPU (--headless-3d --backend dx12)")
	cmd.Flags().StringVar(&opts.FFmpeg, "ffmpeg", "", "ffmpeg to use (default: PATH)")
	cmd.Flags().BoolVar(&buildRelease, "release", false, "build + replay with Release configuration")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo); overrides --release")
	return cmd
}

// findLatestRecording は dirs の bug_*.mtrr のうち、更新が一番新しい 1 つを返す。無ければ空。
func findLatestRecording(dirs []string) string {
	type found struct {
		path string
		mod  int64
	}
	var all []found
	seen := map[string]bool{}
	for _, d := range dirs {
		matches, _ := filepath.Glob(filepath.Join(d, "bug_*.mtrr"))
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil || seen[strings.ToLower(abs)] {
				continue
			}
			seen[strings.ToLower(abs)] = true
			st, err := os.Stat(abs)
			if err != nil || st.IsDir() {
				continue
			}
			all = append(all, found{abs, st.ModTime().UnixNano()})
		}
	}
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].mod != all[j].mod {
			return all[i].mod > all[j].mod
		}
		return all[i].path > all[j].path
	})
	return all[0].path
}

// planClipOutputs は出力の AVI / mp4 / GIF の場所を決める。空文字は作らないもの。
// 録画は host のフォルダにあるので、既定の出力は録画の隣ではなくカレントフォルダに置く。
func planClipOutputs(rec, out string, gif, noMp4 bool) (avi, mp4, gifOut string) {
	target := out
	if target == "" {
		target = strings.TrimSuffix(filepath.Base(rec), filepath.Ext(rec)) + ".mp4"
	}
	ext := strings.ToLower(filepath.Ext(target))
	known := ext == ".mp4" || ext == ".gif" || ext == ".avi"
	stem := target
	if known {
		stem = strings.TrimSuffix(target, filepath.Ext(target))
	}
	avi = stem + ".avi"
	if (ext == ".mp4" || !known) && !noMp4 {
		mp4 = stem + ".mp4"
	}
	if gif || ext == ".gif" {
		gifOut = stem + ".gif"
	}
	return avi, mp4, gifOut
}

func clipHostArgs(dll, rec, avi string, threeD bool) []string {
	args := []string{dll, "--replay-test", rec, "--export-video", avi, "--json"}
	if threeD {
		args = append(args, "--headless-3d", "--backend", "dx12")
	}
	return args
}

// parseClipVerdict は host の stdout の最後にある、verdict を持つ JSON 1 行を返す。無ければ nil。
func parseClipVerdict(stdout string) map[string]any {
	lines := strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		if _, ok := v["verdict"]; ok {
			return v
		}
	}
	return nil
}

// judgeClipHostResult は host の結果が動画として使えるかと、使えるときの注意を返す。
// F11 の録画は途中の状態を持たないので、host は「比べた状態が 0 件」を FAIL と言うが、AVI は書けている。
func judgeClipHostResult(code int, verdict map[string]any, aviExists bool) (usable bool, warning string) {
	if !aviExists {
		return false, ""
	}
	if code == 0 {
		return true, ""
	}
	reason, _ := verdict["reason"].(string)
	switch reason {
	case "no_frames_compared":
		return true, "録画は途中の状態を持たない (F11 の録画はこの形) ので、再生が録画のときと同じ動きかは確かめていません"
	case "diverged":
		return true, "再生の途中でゲームの状態が録画からずれました。動画は録画のときと違う動きかもしれません (DLL か資産が録画のときと違う可能性)"
	}
	return false, ""
}

// clipMp4Args の scale は、MJPEG が全レンジの YUV で出てくるので、放っておくと mp4 も全レンジのままになり、
// ブラウザや一部のプレイヤーで色がずれるのを防ぐ。
func clipMp4Args(avi, mp4 string) []string {
	args := []string{"-y", "-v", "error", "-i", avi, "-c:v", "libx264", "-preset", "medium", "-crf", "18",
		"-vf", "scale=out_range=tv,format=yuv420p", "-c:a", "aac", "-b:a", "160k"}
	args = append(args, clipBitExact...)
	return append(args, "-movflags", "+faststart", mp4)
}

// clipGifArgs は色数が 256 なので、動画ごとに色の表を作り直す。音は入れない。
func clipGifArgs(avi, gif string) []string {
	graph := fmt.Sprintf("fps=%d,scale='min(%d,iw)':-1:flags=lanczos,split[a][b];"+
		"[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle",
		clipGifFPS, clipGifWidth)
	return []string{"-y", "-v", "error", "-i", avi, "-an", "-vf", graph, "-loop", "0",
		"-fflags", "+bitexact", "-map_metadata", "-1", gif}
}

func clipSizeText(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	if st.Size() >= 1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(st.Size())/(1024*1024))
	}
	return fmt.Sprintf("%d KB", (st.Size()+512)/1024)
}

func clipUsageError(format string, a ...any) error {
	return &ExitCodeError{Code: clipExitUsage, Message: "clip: " + fmt.Sprintf(format, a...)}
}

func runClip(opts clipOptions) error {
	// 指定された録画が無いのに、ビルドを何分も待たせない
	if opts.Rec != "" && !fileExists(absOrSelf(opts.Rec)) {
		return clipUsageError("指定した録画 %s が見つかりません。ゲームで F11 を押して保存するか、録画のパスを直してください", absOrSelf(opts.Rec))
	}
	result, err := runBuild()
	if err != nil {
		return err
	}
	art := result.Artifacts
	hostExe, dll := art.HostExePath, art.DllPath
	if !fileExists(dll) {
		return clipUsageError("ゲームの DLL %s がありません", dll)
	}

	rec := ""
	if opts.Rec != "" {
		abs, err := filepath.Abs(opts.Rec)
		if err != nil {
			return clipUsageError("録画 %q の場所が分かりません: %v", opts.Rec, err)
		}
		rec = abs
	} else {
		cwd, _ := os.Getwd()
		rec = findLatestRecording([]string{cwd, filepath.Dir(dll), filepath.Dir(hostExe)})
	}
	if rec == "" || !fileExists(rec) {
		where := "カレントフォルダ、DLL のフォルダ、mitiru_host のフォルダに bug_*.mtrr"
		if opts.Rec != "" {
			where = "指定した録画"
		}
		return clipUsageError("%sが見つかりません。ゲームで F11 を押して保存するか、録画のパスを渡してください", where)
	}

	aviRel, mp4Rel, gifRel := planClipOutputs(rec, opts.Out, opts.Gif, opts.NoMp4)
	avi, mp4, gif := absOrSelf(aviRel), absOrSelf(mp4Rel), absOrSelf(gifRel)
	if err := os.MkdirAll(filepath.Dir(avi), 0o755); err != nil {
		return clipUsageError("%s を作れません: %v", filepath.Dir(avi), err)
	}
	// AVI が残っていると、host が失敗しても成功に見える
	_ = os.Remove(avi)

	fmt.Printf("[clip] host を走らせます: %s\n", filepath.Base(hostExe))
	stdout, stderr, code, runErr := runCaptured(hostExe, clipHostArgs(dll, rec, avi, opts.ThreeD), art.DeployDir)
	if runErr != nil {
		return fmt.Errorf("%s を起動できません (%w)。", hostExe, runErr)
	}
	usable, warning := judgeClipHostResult(code, parseClipVerdict(stdout), fileExists(avi))
	if !usable {
		fmt.Print(stdout)
		fmt.Fprint(os.Stderr, stderr)
		return &ExitCodeError{Code: clipExitHost, Message: fmt.Sprintf(
			"[clip] 録画を動画にできませんでした (mitiru_host が exit %d)。DLL が録画と同じバージョンか確かめてください", code)}
	}
	// 動画にできたときの host の判定行 (FAIL no_frames_compared など) は紛らわしいので、host の通知だけ見せる
	for _, line := range strings.Split(strings.ReplaceAll(stdout+stderr, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "mitiru:") {
			fmt.Println(line)
		}
	}
	if warning != "" {
		fmt.Fprintf(os.Stderr, "[clip] 注意: %s\n", warning)
	}
	fmt.Printf("[clip] AVI: %s (%s)\n", avi, clipSizeText(avi))

	if mp4 == "" && gif == "" {
		return nil
	}
	ffmpegName := opts.FFmpeg
	if ffmpegName == "" {
		ffmpegName = "ffmpeg"
	}
	ffmpeg, lookErr := exec.LookPath(ffmpegName)
	if lookErr != nil {
		fmt.Fprintf(os.Stderr, "[clip] ffmpeg が PATH に無いので、AVI までにしました: %s\n"+
			"[clip] mp4 にするには ffmpeg を入れて (Windows なら winget install Gyan.FFmpeg)、同じコマンドをもう一度流してください\n", avi)
		return nil
	}
	steps := []struct {
		target string
		args   []string
	}{{mp4, clipMp4Args(avi, mp4)}, {gif, clipGifArgs(avi, gif)}}
	for _, s := range steps {
		if s.target == "" {
			continue
		}
		fmt.Printf("[clip] ffmpeg を走らせます: %s\n", filepath.Base(s.target))
		_, errText, fcode, ferr := runCaptured(ffmpeg, s.args, "")
		if ferr != nil || fcode != 0 || !fileExists(s.target) {
			return &ExitCodeError{Code: clipExitFFmpeg, Message: fmt.Sprintf(
				"[clip] ffmpeg が %s を作れませんでした (exit %d): %s\n[clip] AVI は残してあります: %s",
				filepath.Base(s.target), fcode, clipTrim(errText, 400), avi)}
		}
		fmt.Printf("[clip] %s: %s (%s)\n", strings.ToUpper(strings.TrimPrefix(filepath.Ext(s.target), ".")), s.target, clipSizeText(s.target))
	}
	if !opts.KeepAvi {
		_ = os.Remove(avi)
	}
	return nil
}

func clipTrim(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return s[:cut]
	}
	return s
}

// absOrSelf は空なら空、絶対にできなければそのまま返す。
func absOrSelf(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// runCaptured は exe を dir (空なら今の場所) で走らせ、stdout と stderr を文字列で返す。
// 起動できなかったときだけ err を返し、0 以外の終了コードは code で返す。
func runCaptured(exe string, args []string, dir string) (stdout, stderr string, code int, err error) {
	cmd := exec.Command(exe, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	cmd.Dir = dir
	cmd.Env = build.HostEnv()
	runErr := cmd.Run()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return out.String(), errBuf.String(), exitErr.ExitCode(), nil
		}
		return "", "", 0, runErr
	}
	return out.String(), errBuf.String(), 0, nil
}
