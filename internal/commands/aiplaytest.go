package commands

// aiplaytest.go ── ゲームをライブ起動 (MITIRU_AI=1) して /api/ai/branch (反実仮想) で
// 入力を試し、到達状態が仕様違反かを判定する (engine tools/ai_playtest*.py の Go 昇格)。
// suite/bisect/fuzz と違い replay-test ではなく AI 観測 API を叩く別系統。
//
// なぜ MitiruEngine でしか成立しないか: 状態が単一 flat-POD + MITIRU_REFLECT で
// 意味的に読め、/api/ai/branch が「この入力を N フレーム入れたら」を副作用なしで返す。

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/spf13/cobra"
)

var (
	aipDriver   string
	aipAsserts  []string
	aipHorizon  int
	aipMaxTurns int
)

// sweep ドライバが試す固定戦略 (名前, branch に渡す keys)。
var aipStrategies = []struct{ name, keys string }{
	{"右へ寄せ続ける", "Right"},
	{"左へ寄せ続ける", "Left"},
	{"ひたすら落とす", "Down"},
	{"何もしない", ""},
}

var reVerdict = regexp.MustCompile(`(?i)VERDICT:\s*(BUG|CLEAN)\s*(.*)`)

func newAiPlaytestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai-playtest",
		Short: "Playtest the game by observing its reflected state and trying inputs counterfactually",
		Long: `Launches THIS project's game with the AI observation API up (MITIRU_AI=1,
off-screen), then probes it through /api/ai/branch -- simulating inputs N frames
forward WITHOUT committing -- and judges whether a reachable state is illegal.

Only possible on MitiruEngine: state is a single flat-POD GameMemory exposed
semantically via MITIRU_REFLECT, and /api/ai/branch answers "what if I held these
keys for N frames" with zero side effects. The agent reasons over named fields,
not pixels.

Drivers:
  --driver sweep        (default) fixed strategies + numeric --assert. No LLM, no key.
  --driver claude-code  drive it with local Claude Code headless (claude -p): semantic
                        judgment with no numeric thresholds, subscription auth, no API key.

  mitiru ai-playtest --assert "playerX<=1232" --assert "playerX>=48"
  mitiru ai-playtest --driver claude-code`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAiPlaytest()
		},
	}
	cmd.Flags().StringVar(&aipDriver, "driver", "sweep",
		"sweep (deterministic, no LLM) | claude-code (LLM judge via claude -p, subscription)")
	cmd.Flags().StringArrayVar(&aipAsserts, "assert", nil,
		`invariant "field op num" for the sweep driver (e.g. "playerX<=1232"); repeatable`)
	cmd.Flags().IntVar(&aipHorizon, "horizon", 300, "frames to simulate per branch (sweep driver)")
	cmd.Flags().IntVar(&aipMaxTurns, "max-turns", 20, "max agent turns (claude-code driver)")
	return cmd
}

func runAiPlaytest() error {
	inv, err := parseInvariants(aipAsserts)
	if err != nil {
		return err
	}
	if aipDriver == "sweep" && len(inv) == 0 {
		fmt.Fprintln(os.Stderr,
			"注意: sweep は --assert が無いと違反を検出できません (状態の観察のみ)。")
	}

	res, err := runBuild()
	if err != nil {
		return err
	}
	art := res.Artifacts

	port, err := pickFreePort()
	if err != nil {
		return fmt.Errorf("ai-playtest: %w", err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// ゲームを画面外で起動 (--window-pos -2200 0: ユーザー画面を奪わない / no-GUI-launch 規約)。
	hostArgs := append([]string{art.DllRel}, tomlHostArgs()...)
	hostArgs = append(hostArgs, "--window-pos", "-2200", "0", "--no-tool-windows")
	hostCmd := exec.Command(art.HostExePath, hostArgs...)
	hostCmd.Stdout = os.Stderr // ホスト出力は stderr (stdout は結果専用)
	hostCmd.Stderr = os.Stderr
	hostCmd.Dir = art.DeployDir
	hostCmd.Env = append(build.HostEnv(),
		"MITIRU_AI=1",
		fmt.Sprintf("MITIRU_AI_PORT=%d", port),
	)
	if err := hostCmd.Start(); err != nil {
		return fmt.Errorf("ai-playtest: start host %s: %w", art.HostExePath, err)
	}
	hostExit := make(chan error, 1)
	go func() { hostExit <- hostCmd.Wait() }()
	killHost := func() {
		if hostCmd.Process == nil {
			return
		}
		_, _, _ = apiPost(baseURL, apiQuit, nil, "") // graceful quit
		select {
		case <-hostExit:
		case <-time.After(3 * time.Second):
			_ = hostCmd.Process.Kill()
		}
	}
	defer killHost()

	if err := waitReadyOrHostExit(baseURL, 30*time.Second, hostExit, hostCmd); err != nil {
		return fmt.Errorf("ai-playtest: %w", err)
	}

	var found bool
	switch aipDriver {
	case "sweep":
		found, err = aiPlaytestSweep(baseURL, inv)
	case "claude-code":
		found, err = aiPlaytestClaudeCode(baseURL)
	default:
		return fmt.Errorf("ai-playtest: unknown --driver %q (sweep|claude-code)", aipDriver)
	}
	if err != nil {
		return fmt.Errorf("ai-playtest: %w", err)
	}

	killHost()
	if found {
		os.Exit(1) // バグ発見 = CI ゲートで非ゼロ
	}
	return nil
}

// aiPlaytestSweep は固定戦略を branch で試し、--assert 違反を探す (LLM なし)。
func aiPlaytestSweep(baseURL string, inv []invariant) (bool, error) {
	st0, _, err := apiGet(baseURL, apiState)
	if err != nil {
		return false, err
	}
	fmt.Printf("観測開始: %s\n\n", strings.TrimSpace(string(st0)))
	fmt.Printf("ai-playtest (sweep): %d 戦略 × branch %df, 不変条件 %d 個\n\n",
		len(aipStrategies), aipHorizon, len(inv))

	found := false
	var firstBug string
	for _, s := range aipStrategies {
		body := fmt.Sprintf(`{"keys":%q,"frames":%d}`, s.keys, aipHorizon)
		out, status, err := apiPost(baseURL, apiBranch, strings.NewReader(body), "application/json")
		if err != nil || status != 200 {
			fmt.Printf("  [%s] branch 失敗 (status %d)\n", s.name, status)
			continue
		}
		shown := strings.TrimSpace(string(out))
		if v := checkInvariants(string(out), inv); v != "" {
			fmt.Printf("  [%s] → %s  ✗ %s\n", s.name, shown, v)
			if !found {
				firstBug = fmt.Sprintf("戦略「%s」(branch keys=%q) で %s", s.name, s.keys, v)
				found = true
			}
		} else {
			fmt.Printf("  [%s] → %s  ok\n", s.name, shown)
		}
	}

	fmt.Println()
	if found {
		fmt.Printf("バグ発見: %s\n  → この入力を記録すれば決定論 .mtrr のバグ票になる\n", firstBug)
		return true, nil
	}
	fmt.Println("全戦略 clean ── 観測した範囲で不変条件違反なし")
	return false, nil
}

// aiPlaytestClaudeCode はローカル Claude Code を headless (`claude -p`) で呼び、
// サブスク認証 (API キー不要) で observe/branch を curl 駆動・意味判定させる。
func aiPlaytestClaudeCode(baseURL string) (bool, error) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		return false, fmt.Errorf("`claude` (Claude Code) が PATH に無い。導入するか --driver sweep を使う")
	}
	st0, _, _ := apiGet(baseURL, apiState)
	fmt.Printf("観測開始: %s\n\n", strings.TrimSpace(string(st0)))
	fmt.Println("ai-playtest (claude-code headless / サブスク認証・API キー不要)")

	prompt := fmt.Sprintf(`あなたは MitiruEngine 製の決定論ゲームの自動プレイテスターです。画面は 1280x720。状態 API がローカルに立っています — curl で叩いてください:
  GET %[1]s%[2]s … 現在の状態 (MITIRU_REFLECT の名前付き field)
  POST %[1]s%[3]s  本文 {"keys":"Right","frames":120} … その入力を frames だけ押し続けたら状態がどうなるかを副作用なしでシミュレート (コミットされない/何度でも安全)。keys 例: Right / Left / Down / ""(無入力)。

仕事: 入力でゲームを「明らかに仕様違反な状態」に追い込む。数値しきい値は与えない。field 名の意味から正当な状態を推論せよ (座標が画面外へ突き抜ける/スコア負・暴走/フラグ矛盾/NaN・異常値/凍結 など)。まず state を観測し、怪しい仮説 (端まで寄せ続ける等) を branch で検証する。

最後に必ず次のどちらか 1 行だけを出力して終了せよ:
  VERDICT: BUG <一行: どの field がどう壊れたか + 再現 keys/frames>
  VERDICT: CLEAN <一行: 何を試して問題なかったか>`, baseURL, apiState, apiBranch)

	cmd := exec.Command(claude, "-p", prompt, "--allowedTools", "Bash",
		"--max-turns", fmt.Sprintf("%d", aipMaxTurns))
	out, _ := cmd.CombinedOutput() // Go の string は UTF-8 そのまま → Python の cp932 罠は無い
	text := string(out)

	tail := text
	if len(tail) > 1000 {
		tail = tail[len(tail)-1000:]
	}
	if s := strings.TrimSpace(tail); s != "" {
		fmt.Println(s)
	}

	m := reVerdict.FindStringSubmatch(text)
	if m == nil {
		fmt.Fprintln(os.Stderr, "エージェントが VERDICT を出さずに終了 (inconclusive)")
		return false, nil
	}
	if strings.EqualFold(m[1], "BUG") {
		fmt.Printf("\nバグ発見: %s  → 記録すれば決定論 .mtrr のバグ票\n", strings.TrimSpace(m[2]))
		return true, nil
	}
	fmt.Printf("\nclean: %s\n", strings.TrimSpace(m[2]))
	return false, nil
}
