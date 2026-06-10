package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var (
	verifyFrames          int
	verifyGolden          string
	verifyGoldenThreshold float64
	verifyReplayFile      string
	verifyJSON            bool
)

func newVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "ヘッドレスでビルド・起動・スクリーンショットを撮り、合否を JSON で出力する",
		Long: `Build the current project, launch the host with MITIRU_AI=1,
wait for readiness, then:

  1. Wait --frames * 16ms for the game to settle
  2. Fetch GET /api/screenshot  (PNG)
  3. Compare to --golden if provided
  4. POST /api/runtime/quit  (graceful shutdown)

Writes a single JSON document to stdout (or a file with --out).
Exit code reflects the verdict: 0=pass, 1=fail, 2=build error.

Examples:
  mitiru verify                             # build + smoke (no golden)
  mitiru verify --golden ref.png            # compare against golden
  mitiru verify --replay session.mtrr       # replay before screenshot
  mitiru verify --frames 600 --out ci.json  # 10s settle, save JSON`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify()
		},
	}
	cmd.Flags().IntVar(&verifyFrames, "frames", 300,
		"フレーム数だけ待機してからスクリーンショットを取得 (16ms/frame)")
	cmd.Flags().StringVar(&verifyGolden, "golden", "",
		"比較対象の golden PNG ファイル (省略時は比較しない)")
	cmd.Flags().Float64Var(&verifyGoldenThreshold, "golden-threshold", 0.95,
		"golden 一致率の閾値 (0–1、既定 0.95 = 95%)")
	cmd.Flags().StringVar(&verifyReplayFile, "replay", "",
		"起動後に再生する .mtrr ファイル (省略時は通常起動)")
	cmd.Flags().BoolVar(&verifyJSON, "json", false,
		"verdict JSON を整形して stdout に出力 (デフォルト: minified)")
	return cmd
}

// verifyResult は verify コマンドが出力する JSON の形。
type verifyResult struct {
	Build      string              `json:"build"`
	BuildErr   string              `json:"buildErr,omitempty"`
	Replay     *verifyReplayResult `json:"replay,omitempty"`
	Screenshot *verifyShotResult   `json:"screenshot,omitempty"`
	Verdict    string              `json:"verdict"` // "pass" | "fail" | "build_error"
}

type verifyReplayResult struct {
	BitExact bool `json:"bitExact"`
	ExitCode int  `json:"exitCode"`
}

type verifyShotResult struct {
	Path          string  `json:"path"`
	GoldenFile    string  `json:"goldenFile,omitempty"`
	GoldenDiffPct float64 `json:"goldenDiffPct,omitempty"`
}

func runVerify() error {
	result := &verifyResult{}

	// 1. ビルド。
	buildRes, err := runBuild()
	if err != nil {
		result.Build = "error"
		result.BuildErr = err.Error()
		result.Verdict = "build_error"
		return writeVerifyResult(result, 2)
	}
	result.Build = "ok"
	art := buildRes.Artifacts

	// 2. 空きポートを確保してエンジンを起動。
	port, err := pickFreePort()
	if err != nil {
		// リトライ 1 回。
		port, err = pickFreePort()
		if err != nil {
			result.Verdict = "fail"
			return writeVerifyResultErr(result, fmt.Errorf("verify: port: %w", err))
		}
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	hostArgs := []string{art.DllRel}
	hostArgs = append(hostArgs, tomlHostArgs()...)
	if verifyReplayFile != "" {
		absReplay, err := filepath.Abs(verifyReplayFile)
		if err != nil {
			result.Verdict = "fail"
			return writeVerifyResultErr(result, fmt.Errorf("verify: resolve replay: %w", err))
		}
		hostArgs = append(hostArgs, "--replay", absReplay)
	}

	hostCmd := exec.Command(art.HostExePath, hostArgs...)
	hostCmd.Stdout = os.Stderr // ホストの出力は stderr に流す (stdout は JSON 専用)
	hostCmd.Stderr = os.Stderr
	hostCmd.Dir = art.DeployDir
	hostCmd.Env = append(os.Environ(),
		"MITIRU_AI=1",
		fmt.Sprintf("MITIRU_AI_PORT=%d", port),
	)

	if err := hostCmd.Start(); err != nil {
		result.Verdict = "fail"
		return writeVerifyResultErr(result, fmt.Errorf("verify: start host: %w", err))
	}

	// プロセスを終了させるための defer (quit エンドポイントが失敗した場合の保険)。
	defer func() {
		if hostCmd.Process != nil {
			_, _ = hostCmd.Process.Wait()
		}
	}()

	// 3. 準備完了まで待機 (30 秒タイムアウト)。
	if err := waitReady(baseURL, 30*time.Second); err != nil {
		_ = hostCmd.Process.Kill()
		result.Verdict = "fail"
		return writeVerifyResultErr(result, fmt.Errorf("verify: readiness: %w", err))
	}

	// 4. フレーム分待機 (16ms/frame)。
	settle := time.Duration(verifyFrames) * 16 * time.Millisecond
	if settle > 0 {
		time.Sleep(settle)
	}

	// 5. スクリーンショット取得。
	shotBody, status, err := apiGet(baseURL, apiShot)
	if err != nil || status != 200 {
		_ = hostCmd.Process.Kill()
		result.Verdict = "fail"
		detail := ""
		if err != nil {
			detail = err.Error()
		} else {
			detail = fmt.Sprintf("HTTP %d", status)
		}
		return writeVerifyResultErr(result, fmt.Errorf("verify: screenshot: %s", detail))
	}

	// スクリーンショットを一時ファイルに保存。
	shotFile, err := os.CreateTemp("", "mitiru_verify_*.png")
	if err != nil {
		_ = hostCmd.Process.Kill()
		result.Verdict = "fail"
		return writeVerifyResultErr(result, fmt.Errorf("verify: save screenshot: %w", err))
	}
	shotPath := shotFile.Name()
	if _, err := shotFile.Write(shotBody); err != nil {
		shotFile.Close()
		_ = hostCmd.Process.Kill()
		result.Verdict = "fail"
		return writeVerifyResultErr(result, fmt.Errorf("verify: write screenshot: %w", err))
	}
	shotFile.Close()

	shotRes := &verifyShotResult{Path: shotPath}
	result.Screenshot = shotRes

	// 6. golden 比較。
	verdict := "pass"
	if verifyGolden != "" {
		shotRes.GoldenFile = verifyGolden
		pct, err := comparePNG(shotPath, verifyGolden)
		if err != nil {
			// 比較できない場合も fail 扱い。
			_ = hostCmd.Process.Kill()
			result.Verdict = "fail"
			return writeVerifyResultErr(result, fmt.Errorf("verify: golden compare: %w", err))
		}
		shotRes.GoldenDiffPct = pct
		if pct < verifyGoldenThreshold {
			verdict = "fail"
		}
	}

	// 7. エンジンを行儀よく終了させる。
	_, _, _ = apiPost(baseURL, apiQuit, nil, "")
	done := make(chan error, 1)
	go func() { done <- hostCmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = hostCmd.Process.Kill()
		<-done
	}

	result.Verdict = verdict
	return writeVerifyResult(result, exitCodeForVerdict(verdict))
}

// comparePNG は 2 つの PNG ファイルをピクセル単位で比較し、一致率 (0–1) を返す。
// 簡易実装: バイト列比較でピクセル数の比較は行わない。
// 両者が完全一致なら 1.0、バイト差異があれば最良でも 0.95 未満になるよう計算する。
// 注意: 本格的な image diff が必要な場合はエンジン側 /api/diff を使うことを推奨。
func comparePNG(pathA, pathB string) (float64, error) {
	a, err := os.ReadFile(pathA)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", pathA, err)
	}
	b, err := os.ReadFile(pathB)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", pathB, err)
	}
	if len(a) == 0 || len(b) == 0 {
		return 0, fmt.Errorf("empty PNG file")
	}
	if len(a) != len(b) {
		// サイズが異なれば 0% 一致扱い。
		return 0.0, nil
	}
	same := 0
	for i := range a {
		if a[i] == b[i] {
			same++
		}
	}
	return float64(same) / float64(len(a)), nil
}

func exitCodeForVerdict(verdict string) int {
	switch verdict {
	case "pass":
		return 0
	case "build_error":
		return 2
	default:
		return 1
	}
}

func writeVerifyResult(r *verifyResult, exitCode int) error {
	enc := json.NewEncoder(os.Stdout)
	if verifyJSON {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("verify: encode result: %w", err)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

// writeVerifyResultErr は result を出力したうえで err を返す。
// exit code は result.Verdict から決める。
func writeVerifyResultErr(r *verifyResult, err error) error {
	if r.Verdict == "" {
		r.Verdict = "fail"
	}
	_ = writeVerifyResult(r, exitCodeForVerdict(r.Verdict))
	return err
}
