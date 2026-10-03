package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/console"
)

// distCheckFrames は --check で回すフレーム数。3D の取り込み (.clod の変換) が終わって、
// 最初の絵が出るまでに足りる長さ。
const distCheckFrames = 90

// distCheckProblemWords は host のログのうち、配布物に何かが欠けていることを示す言い回し。
// engine の warnOnce と loader が出す文に合わせてある。engine は欠けを知らせる文に
// 読めません / 見つかりません / 開けません / ありません / 失敗しました のどれかを入れる。
var distCheckProblemWords = []string{
	"読めない", "読めません", "ありません", "見つから", "見つかりません", "開けません", "失敗", "失敗しました",
	"not found", "cannot open", "failed to load", "missing",
}

// distCheckResult は --check の 1 回分の結果。
type distCheckResult struct {
	ExitCode int
	Problems []string // 欠けを示すログの行
	Shot     string   // 最後に撮った絵 (無ければ空)
	Log      string   // host のログを書いた先
}

// splitMtargs は launch.mtargs の 1 行を引数に分ける (mtargsJoin の逆。"..." で囲んだ所は 1 つ)。
func splitMtargs(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range strings.TrimSpace(line) {
		switch {
		case r == '"':
			inQuote, has = !inQuote, true
		case (r == ' ' || r == '\t') && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// cleanEnviron は開発機にしか無い変数 (MITIRU_* と開発ツールの PATH) を外した環境を返す。
// 遊ぶ側の PC では、配布物の中と Windows の System32 しか当てにできない。
func cleanEnviron(environ []string) []string {
	root := `C:\Windows`
	var out []string
	for _, kv := range environ {
		key := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		switch {
		case strings.HasPrefix(key, "MITIRU_"), key == "PATH", key == "INCLUDE", key == "LIB",
			key == "LIBPATH", strings.HasPrefix(key, "VS"), strings.HasPrefix(key, "VCTOOLS"):
			continue
		case key == "SYSTEMROOT":
			root = strings.SplitN(kv, "=", 2)[1]
		}
		out = append(out, kv)
	}
	return append(out, "PATH="+filepath.Join(root, "System32")+";"+root)
}

// findDistProblems は host のログから、欠けを示す行を拾う。
func findDistProblems(log string) []string {
	var out []string
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		for _, w := range distCheckProblemWords {
			if strings.Contains(low, strings.ToLower(w)) {
				out = append(out, line)
				break
			}
		}
	}
	return out
}

// runDistCheck は bundleRoot を一時フォルダへ写し、素の PC に近い環境で host を headless で走らせる。
// 撮った絵は shotOut へ写す。
func runDistCheck(bundleRoot, shotOut string) (distCheckResult, error) {
	var res distCheckResult
	tmp, err := os.MkdirTemp("", "mitiru-dist-check-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmp)
	if err := copyTree(bundleRoot, tmp); err != nil {
		return res, fmt.Errorf("dist --check: copy bundle: %w", err)
	}
	data := filepath.Join(tmp, "data")
	launch, err := os.ReadFile(filepath.Join(data, "launch.mtargs"))
	if err != nil {
		return res, fmt.Errorf("dist --check: launch.mtargs が無い (ランチャ stub の無い engine では確かめられない): %w", err)
	}
	capDir := filepath.Join(tmp, "_capture")
	args := append(splitMtargs(string(launch)), "--headless-3d", "--max-frames", fmt.Sprint(distCheckFrames),
		"--capture-dir", capDir, "--capture-every", fmt.Sprint(distCheckFrames-1))
	if !strings.Contains(string(launch), "--backend") {
		// 窓のある起動は DX12 を選ぶ。窓の無い headless でも同じ描画の経路を通す
		args = append(args, "--backend", "dx12")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(data, "mitiru_host.exe"), args...)
	cmd.Dir = data
	cmd.Env = cleanEnviron(os.Environ())
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return res, fmt.Errorf("dist --check: host を起動できない: %w", runErr)
	}
	if exitErr != nil {
		res.ExitCode = exitErr.ExitCode()
	}
	res.Problems = findDistProblems(log.String())
	// ログは撮った絵の隣に残す。OK でも、何を読んで何を作ったかを後から見られるように
	logOut := strings.TrimSuffix(shotOut, filepath.Ext(shotOut)) + ".log"
	if err := os.MkdirAll(filepath.Dir(logOut), 0o755); err == nil {
		_ = os.WriteFile(logOut, log.Bytes(), 0o644)
		res.Log = logOut
	}
	if shot, serr := lastCapture(capDir); serr == nil {
		if err := copyFile(shot, shotOut); err == nil {
			res.Shot = shotOut
		}
	}
	if res.ExitCode != 0 {
		res.Problems = append(res.Problems, exitCodeText(uint32(res.ExitCode)))
	}
	return res, nil
}

// exitCodeText は host の終了コードを、何が起きたかの文にする。
func exitCodeText(code uint32) string {
	switch code {
	case 0xC0000135:
		return "host が起動する前に止まりました。必要な DLL がありません (0xC0000135)。"
	case 0xC0000139, 0xC000007B:
		return fmt.Sprintf("host が起動する前に止まりました。DLL の版か形式が合いません (0x%08X)。", code)
	default:
		return fmt.Sprintf("host が終了コード %d (0x%08X) で終わりました。", int32(code), code)
	}
}

// isBlankImage は PNG が 1 色だけか返す (描画が何も届いていない画面)。
func isBlankImage(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return false, err
	}
	b := img.Bounds()
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			if r, g, bl, _ := img.At(x, y).RGBA(); r != r0 || g != g0 || bl != b0 {
				return false, nil
			}
		}
	}
	return true, nil
}

// copyTree は src 以下を dst へ写す。
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

// checkDistBundle は --check の結果を表にして、欠けがあればエラーにする。
func checkDistBundle(bundleRoot, shotOut string) error {
	console.Verbosef("dist --check: running a copy of the bundle headless without dev env vars or PATH\n")
	res, err := runDistCheck(bundleRoot, shotOut)
	if err != nil {
		return err
	}
	if res.Shot != "" {
		if blank, berr := isBlankImage(res.Shot); berr == nil && blank {
			res.Problems = append(res.Problems, "撮った絵が 1 色だけで、何も描かれていません ("+res.Shot+")。")
		}
		fmt.Printf("最後のフレームを %s に保存しました (ログは %s です)。\n", res.Shot, res.Log)
	} else {
		res.Problems = append(res.Problems, "絵を 1 枚も撮れませんでした。")
	}
	if len(res.Problems) > 0 {
		return fmt.Errorf("配布物に欠けているものがあります。\n  %s", strings.Join(res.Problems, "\n  "))
	}
	fmt.Println("配布物は開発用の環境が無くても動きました。欠けの知らせは無く、終了コードは 0 です。")
	return nil
}
