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

	"github.com/mogmog-0110/mitiru-cli/internal/hunt"
	"github.com/spf13/cobra"
)

// replayVerdict は host --replay-test --json が stdout の最終行に出す 1 行 JSON
// (mitiru_host apps/mitiru_host/main.cpp の emitJsonVerdict と対で保守する)。
type replayVerdict struct {
	Verdict        string          `json:"verdict"`
	Reason         string          `json:"reason"`
	FramesCompared uint64          `json:"framesCompared"`
	TotalFrames    uint64          `json:"totalFrames"`
	DivergedFrame  *uint64         `json:"divergedAtFrame,omitempty"`
	Diff           json.RawMessage `json:"diff,omitempty"`
	Blame          string          `json:"blame,omitempty"`
}

// parseReplayVerdict は host stdout の最終非空行 (verdict の 1 行 JSON) を取り出す。
// finalState 側の envTag JSON (複数行、末尾 "}" のみ) と区別するため、後ろから
// 1 行ずつ試して "verdict" キーを持つ最初の行を採用する。
func parseReplayVerdict(stdout []byte) (replayVerdict, bool) {
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var v replayVerdict
		if err := json.Unmarshal(line, &v); err == nil && v.Verdict != "" {
			return v, true
		}
	}
	return replayVerdict{}, false
}

var (
	replayRecordFile        string
	replayPlayFile          string
	replayTestFile          string
	replayExpectFile        string
	replaySuiteDir          string
	replayGame              bool
	replayDiff              bool
	replaySaveRoundtripTest bool
)

// stateDiffResult は `mitiru_host --state-diff A B --nolog` が stdout に出す1行 JSON
// (apps/mitiru_host/main.cpp の --state-diff 分岐と対で保守する)。DLL を読まない byte 比較
// なので divergedAtFrame はあっても diff フィールド名までは出ない (host 側が game の reflect
// schema を読み込んでいないため)。
type stateDiffResult struct {
	Diverged            bool   `json:"diverged"`
	FirstDivergentFrame uint32 `json:"firstDivergentFrame"`
	TotalFrames         uint32 `json:"totalFrames"`
}

func newReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Record, play back, or regression-test an input replay (deterministic)",
		Long: `Replays this project's game through the host: the recorded input stream
reproduces a session bit-exact (byte-for-byte identical every run).

Provide exactly one of:
  --record <file>   alias of 'mitiru run --record <file>' (real input needs a window)
  --replay <file>   play back a previously recorded <file> through the host
                    (host forces headless for any replay; no window opens)
  --test   <file>   regression test without opening a window
                    prints final-state JSON to stdout and exits 0 on success.
                    Combine with --expect <json> to diff against a known baseline.
  --diff <a> <b>    compare two .mtrr recordings (e.g. before/after a fix, same
                    input) and report the first frame where GameMemory diverges.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if replayDiff {
				return cobra.ExactArgs(2)(cmd, args)
			}
			return cobra.NoArgs(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if replayDiff {
				return runReplayDiff(args[0], args[1])
			}
			return runReplay()
		},
	}
	cmd.Flags().StringVar(&replayRecordFile, "record", "", "record a session to <file>")
	cmd.Flags().StringVar(&replayPlayFile, "replay", "", "play back <file>")
	cmd.Flags().StringVar(&replayTestFile, "test", "", "regression-test against <file> without opening a window")
	cmd.Flags().StringVar(&replayExpectFile, "expect", "", "expected final-state JSON for --test comparison")
	cmd.Flags().StringVar(&replaySuiteDir, "suite", "",
		"regression suite: replay every *.mtrr in <dir> against this project's game, "+
			"print a pass/fail table, exit non-zero on any divergence (CI gate)")
	cmd.Flags().BoolVar(&replayDiff, "diff", false,
		"compare two .mtrr files (pass them as the 2 positional args): mitiru replay --diff a.mtrr b.mtrr")
	cmd.Flags().BoolVar(&replayGame, "game", true,
		"deprecated: always on (the standalone replay demo was absorbed into the host path)")
	_ = cmd.Flags().MarkHidden("game")
	cmd.Flags().BoolVar(&replaySaveRoundtripTest, "save-roundtrip-test", false,
		"with --test: also check that save -> load -> save is bit-exact (host --save-roundtrip-test)")
	return cmd
}

func runReplay() error {
	if replaySuiteDir != "" {
		return runReplaySuite()
	}

	record := replayRecordFile != ""
	play := replayPlayFile != ""
	test := replayTestFile != ""

	modeCount := 0
	if record {
		modeCount++
	}
	if play {
		modeCount++
	}
	if test {
		modeCount++
	}

	if modeCount > 1 {
		return fmt.Errorf("replay: --record, --replay, and --test are mutually exclusive; pass exactly one")
	}
	if modeCount == 0 {
		return fmt.Errorf("replay: pass exactly one of --record <file>, --replay <file>, or --test <file>")
	}

	if replayExpectFile != "" && !test {
		return fmt.Errorf("replay: --expect requires --test")
	}

	if record {
		// 録画は実入力が要る (= window が要る) ので run 側の経路に委譲する。
		return fmt.Errorf("replay: 録画は `mitiru run --record %s` を使ってください (実入力のため window 起動)", replayRecordFile)
	}

	if play {
		abs, err := filepath.Abs(replayPlayFile)
		if err != nil {
			return fmt.Errorf("replay: resolve %q: %w", replayPlayFile, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return fmt.Errorf("replay: %s: %w", abs, err)
		}
		// プロジェクトの game を host 経由で再生 (standalone replay demo は吸収済み)。
		// host は `--replay` を受け付けない (`--replay-test` のみ)。--test 側と違い
		// --no-tool-windows も --expect も付けない (判定ではなく出力を眺める用途のため)。
		result, err := runBuild()
		if err != nil {
			return err
		}
		art := result.Artifacts
		c := exec.Command(art.HostExePath, art.DllRel, "--replay-test", abs)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Dir = art.DeployDir
		return c.Run()
	}

	// --test モード: headless、window なし、CEF なし。
	abs, err := filepath.Abs(replayTestFile)
	if err != nil {
		return fmt.Errorf("replay: resolve %q: %w", replayTestFile, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("replay: %s: %w", abs, err)
	}

	// このプロジェクトの DLL を host 経由で headless replay する (実ゲーム)。
	return runReplayGameTest(abs)
}

// runReplayGameTest は現在のプロジェクトを build し、記録した session をプロジェクトの
// host DLL 経由で headless に replay する (`mitiru_host <dll> --replay-test`)。--expect
// 指定時は最終 push された view.* state を assert する。host の exit code はそのまま
// 伝播するので CI が regression 結果を見られる。
func runReplayGameTest(absFile string) error {
	result, err := runBuild()
	if err != nil {
		return err
	}
	art := result.Artifacts

	hostArgs := []string{art.DllRel, "--replay-test", absFile}
	if replaySaveRoundtripTest {
		hostArgs = append(hostArgs, "--save-roundtrip-test")
	}
	if replayExpectFile != "" {
		absExpect, err := filepath.Abs(replayExpectFile)
		if err != nil {
			return fmt.Errorf("replay: resolve expect %q: %w", replayExpectFile, err)
		}
		if _, err := os.Stat(absExpect); err != nil {
			return fmt.Errorf("replay: expect %s: %w", absExpect, err)
		}
		hostArgs = append(hostArgs, "--expect", absExpect)
	}

	cmd := exec.Command(art.HostExePath, hostArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = art.DeployDir
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode()) // regression-gate の exit code を忠実に伝える
		}
		return fmt.Errorf("replay --game: %w", err)
	}
	return nil
}

// runReplaySuite は <dir>/*.mtrr を全部、プロジェクトの game へ headless replay-test し、
// pass/fail 表と失敗本数=exit code を出す。録ったプレイ群がゼロコスト回帰テストになる。
// 単一 flat-POD GameMemory + bit-exact replay があるから成立する (決定論ツール軸)。
func runReplaySuite() error {
	absDir, err := filepath.Abs(replaySuiteDir)
	if err != nil {
		return fmt.Errorf("replay --suite: resolve %q: %w", replaySuiteDir, err)
	}
	mtrrs, _ := filepath.Glob(filepath.Join(absDir, "*.mtrr"))
	sort.Strings(mtrrs)
	if len(mtrrs) == 0 {
		return fmt.Errorf("replay --suite: %s に *.mtrr がありません (まず `mitiru run --record` で録画)", absDir)
	}

	result, err := runBuild() // host + DLL を 1 回だけ build
	if err != nil {
		return err
	}
	art := result.Artifacts

	fmt.Printf("replay-suite: %d 本\n\n", len(mtrrs))
	fails := 0
	for _, m := range mtrrs {
		c := exec.Command(art.HostExePath, art.DllRel, "--replay-test", m, "--json",
			"--no-tool-windows", "--window-pos", "-2200", "0")
		c.Dir = art.DeployDir
		var stdout bytes.Buffer
		c.Stdout = &stdout
		_ = c.Run() // 非ゼロ終了は verdict FAIL として下で扱う (エラー自体は無視してよい)
		name := strings.TrimSuffix(filepath.Base(m), ".mtrr")
		v, ok := parseReplayVerdict(stdout.Bytes())
		switch {
		case ok && v.Verdict == "PASS":
			fmt.Printf("  [PASS] %s  bit-exact / %d frames\n", name, v.FramesCompared)
		case ok:
			detail := v.Reason
			if v.DivergedFrame != nil {
				detail = fmt.Sprintf("DIVERGED @frame %d", *v.DivergedFrame)
			}
			if len(v.Diff) > 0 {
				detail += "  diff: " + string(v.Diff)
			}
			fmt.Printf("  [FAIL] %s  %s\n", name, detail)
			fails++
		default:
			fmt.Printf("  [FAIL] %s  (no verdict)\n", name)
			fails++
		}
	}
	fmt.Printf("\n%d/%d green%s\n", len(mtrrs)-fails, len(mtrrs),
		map[bool]string{true: "  -- 全 green", false: fmt.Sprintf("  -- %d 本が回帰", fails)}[fails == 0])
	if fails > 0 {
		os.Exit(1)
	}
	return nil
}

// runReplayDiff は2つの .mtrr (典型的には修正前後、同入力で録ったもの) を host の
// `--state-diff A B` (DLL 不要、GameMemory の byte 比較) にかけ、最初に分岐した frame を
// 報告する (P2)。host は byte 単位でしか比較せず game の reflect schema を読み込まないため、
// どの field が分岐したかまでは出せない (field 単位 diff には `--state-diff` へ
// `--game <dll>` を足す host 側の変更が要るが、このコマンドは host の既存引数だけを使う
// 制約のため見送る)。代わりに、分岐した frame の state blob を両ファイルから CLI 側で
// 読み直し (hunt.ReadStateAtFrame)、byte offset の差分区間を表示する (field 名までは
// 出せないが、host の verdict 1 行だけよりは手掛かりが増える)。
func runReplayDiff(a, b string) error {
	absA, err := filepath.Abs(a)
	if err != nil {
		return fmt.Errorf("replay --diff: resolve %q: %w", a, err)
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return fmt.Errorf("replay --diff: resolve %q: %w", b, err)
	}
	for _, p := range []string{absA, absB} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("replay --diff: %s: %w", p, err)
		}
	}

	result, err := runBuild() // host exe だけ要る (--state-diff は DLL を読まない)
	if err != nil {
		return err
	}
	art := result.Artifacts

	c := exec.Command(art.HostExePath, "--state-diff", absA, absB)
	c.Dir = art.DeployDir
	var stdout bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = os.Stderr
	runErr := c.Run() // exit 1=diverged / 2=比較不能。どちらも下の JSON 解釈で扱うので無視してよい

	var d stateDiffResult
	if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &d) != nil {
		return fmt.Errorf("replay --diff: host から verdict JSON が読めませんでした (%v)\n%s",
			runErr, stdout.String())
	}

	fmt.Printf("replay --diff: %s vs %s\n", filepath.Base(absA), filepath.Base(absB))
	if !d.Diverged {
		fmt.Printf("  一致 ── %d frame とも GameMemory が byte-exact\n", d.TotalFrames)
		return nil
	}
	fmt.Printf("  最初に食い違った frame: %d (全 %d frame 中)\n", d.FirstDivergentFrame, d.TotalFrames)
	fmt.Println("  差分フィールド名は出ません (byte 比較のみ、game の reflect schema 未読込)。")
	printByteDiffRanges(absA, absB, d.FirstDivergentFrame)
	os.Exit(1)
	return nil
}

// printByteDiffRanges はその frame の state blob を両ファイルから読み直し、値が異なる
// byte offset の範囲を表示する。読み直しに失敗しても --diff 本体の verdict (非ゼロ終了)
// は変えず、手掛かりが出せなかった旨だけ伝える。
func printByteDiffRanges(pathA, pathB string, frameIdx uint32) {
	stateA, errA := hunt.ReadStateAtFrame(pathA, frameIdx)
	stateB, errB := hunt.ReadStateAtFrame(pathB, frameIdx)
	if errA != nil || errB != nil {
		fmt.Printf("  byte offset 範囲: 読み直し失敗 (%v / %v)\n", errA, errB)
		return
	}
	if len(stateA) != len(stateB) {
		fmt.Printf("  state サイズが frame %d で既に違います (%d vs %d バイト) — offset 範囲は共通長までのみ\n",
			frameIdx, len(stateA), len(stateB))
	}
	ranges := hunt.DiffByteRanges(stateA, stateB)
	if len(ranges) == 0 {
		fmt.Println("  byte offset 範囲: 共通長の範囲内では差分なし (末尾のサイズ差のみ)")
		return
	}
	fmt.Printf("  差分 byte offset (%d 区間, フィールド名は不明):\n", len(ranges))
	const maxShown = 8
	for i, r := range ranges {
		if i >= maxShown {
			fmt.Printf("    ... 他 %d 区間\n", len(ranges)-maxShown)
			break
		}
		fmt.Printf("    [%d, %d) (%d bytes)\n", r.Start, r.End, r.End-r.Start)
	}
}
