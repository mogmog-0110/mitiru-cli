package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	replayRecordFile string
	replayPlayFile   string
	replayTestFile   string
	replayExpectFile string
	replaySuiteDir   string
	replayGame       bool
)

func newReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Record, play back, or regression-test an input replay (deterministic)",
		Long: `Replays this project's game through the host: the recorded input stream
reproduces a session bit-exact (byte-for-byte identical every run).

Provide exactly one of:
  --record <file>   alias of 'mitiru run --record <file>' (real input needs a window)
  --replay <file>   play back a previously recorded <file> through the host
  --test   <file>   regression test without opening a window
                    prints final-state JSON to stdout and exits 0 on success.
                    Combine with --expect <json> to diff against a known baseline.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
	cmd.Flags().BoolVar(&replayGame, "game", true,
		"deprecated: always on (the standalone replay demo was absorbed into the host path)")
	_ = cmd.Flags().MarkHidden("game")
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
		result, err := runBuild()
		if err != nil {
			return err
		}
		art := result.Artifacts
		c := exec.Command(art.HostExePath, art.DllRel, "--replay", abs)
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

	// host verdict 行の機械可読タグ: "replay state: PASS (bit-exact, N frames)" /
	// "replay state: FAIL (diverged at frame N of M frames)"。タグ部は ASCII 固定。
	passRe := regexp.MustCompile(`replay state: PASS \(bit-exact, (\d+) frames\)`)
	divRe := regexp.MustCompile(`replay state: FAIL \(diverged at frame (\d+)`)
	diffRe := regexp.MustCompile(`replay diff: (\[.*\])`)

	fmt.Printf("replay-suite: %d 本\n\n", len(mtrrs))
	fails := 0
	for _, m := range mtrrs {
		c := exec.Command(art.HostExePath, art.DllRel, "--replay-test", m,
			"--no-tool-windows", "--window-pos", "-2200", "0")
		c.Dir = art.DeployDir
		out, _ := c.CombinedOutput()
		s := string(out)
		name := strings.TrimSuffix(filepath.Base(m), ".mtrr")
		if mm := passRe.FindStringSubmatch(s); mm != nil {
			fmt.Printf("  [PASS] %s  bit-exact / %s frames\n", name, mm[1])
		} else if mm := divRe.FindStringSubmatch(s); mm != nil {
			detail := "DIVERGED @frame " + mm[1]
			if dm := diffRe.FindStringSubmatch(s); dm != nil {
				detail += "  diff: " + dm[1]
			}
			fmt.Printf("  [FAIL] %s  %s\n", name, detail)
			fails++
		} else {
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
