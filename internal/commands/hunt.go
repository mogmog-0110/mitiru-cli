package commands

// hunt.go ── mitiru hunt (N2/N3/N4/N6): headless host を並列に何本も走らせ、
// クラッシュ・非決定性・不変条件違反を自動で釣る。fuzz.go (1回の反復・最初の失敗で終了) と
// 違い、時間予算いっぱい探索を続けて複数チケットを起票する「夜間バッチ」向けのコマンド。
//
// 探索器の生成ロジック・オラクル判定・起票・区間削除の最小化は internal/hunt (host/cobra に
// 依存しない純粋パッケージ) に分離し、ここは host プロセスの起動 (runHostCaptured 等
// replaytools.go の既存ヘルパを流用) と並列化・起票の配線だけを持つ。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/hunt"
	"github.com/spf13/cobra"
)

var (
	huntHours    float64
	huntJobs     int
	huntGame     string
	huntSeed     int64
	huntExplorer string
	huntFrames   int
	huntTimeout  int
	huntAsserts  []string
	huntOut      string
	huntSaveKey  string
	huntLoadKey  string
	huntKeysCSV  string
	huntAt       string
)

func newHuntCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hunt",
		Short: "Run headless hosts in parallel to hunt for crashes, nondeterminism, and invariant violations",
		Long: `Spends a time budget throwing input at THIS project's game through N parallel
headless hosts, looking for the same three failure classes as 'mitiru fuzz' --
crash, nondeterminism, and --assert violations -- but keeps going for the whole
budget instead of stopping at the first hit, so it can surface several distinct
bugs in one run (a nightly batch, not a single CI gate check).

Explorers (--explorer):
  random          keep-then-release random key segments (same shape as fuzz)
  novelty         extends input that reached NEW GameMemory states (AFL-style energy)
  from-recording:<mtrr>  branches off a human recording at a random cut frame
  inhuman         all-keys-at-once / 1-frame alternation / 60s hold / (with
                  --save-key/--load-key) save-then-load every frame / extreme
                  numeric PUT to /api/ai/state
  pairs           exhaustively tries every ordered pair of keys in the vocabulary

Each failure is delta-debug minimized (interval deletion) into the shortest
reproducing input, deduplicated by final-state hash + cause, and written to
hunt_out/<date>/ticket_NN/ alongside a hunt_report.md summary.

  mitiru hunt --hours 8 --jobs 4 --explorer novelty
  mitiru hunt --hours 0.05 --jobs 2 --game build\apps\mitiru_host\my_game\my_game.dll --explorer random`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHunt()
		},
	}
	cmd.Flags().Float64Var(&huntHours, "hours", 1, "time budget in hours")
	cmd.Flags().IntVar(&huntJobs, "jobs", 2, "parallel headless host workers")
	cmd.Flags().StringVar(&huntGame, "game", "", "path to a built game DLL (skips 'mitiru build'; deploy layout inferred from its location)")
	cmd.Flags().Int64Var(&huntSeed, "seed", 1, "RNG seed (same seed + jobs reproduces the same candidate stream)")
	cmd.Flags().StringVar(&huntExplorer, "explorer", "novelty", "random | novelty | from-recording:<mtrr> | inhuman | pairs")
	cmd.Flags().IntVar(&huntFrames, "frames", 900, "frames per candidate run")
	cmd.Flags().IntVar(&huntTimeout, "timeout", 60, "per-run host timeout (seconds)")
	cmd.Flags().StringArrayVar(&huntAsserts, "assert", nil, `invariant "field op num"; repeatable`)
	cmd.Flags().StringVar(&huntOut, "out", "hunt_out", "directory to write hunt_out/<date>/ticket_NN/ + hunt_report.md into")
	cmd.Flags().StringVar(&huntSaveKey, "save-key", "", "key bound to this game's save action (enables the inhuman save/load-every-frame check)")
	cmd.Flags().StringVar(&huntLoadKey, "load-key", "", "key bound to this game's load action")
	cmd.Flags().StringVar(&huntKeysCSV, "keys", "", "comma-separated key vocabulary override (default: Left,Right,Down,Up)")
	cmd.Flags().StringVar(&huntAt, "at", "", "print Windows Task Scheduler guidance for a nightly run at HH:MM instead of hunting now (N6)")
	return cmd
}

func runHunt() error {
	if huntAt != "" {
		printHuntAtGuidance(huntAt)
		return nil
	}
	inv, err := hunt.ParseInvariants(huntAsserts)
	if err != nil {
		return err
	}
	art, err := resolveHuntArtifacts(huntGame)
	if err != nil {
		return err
	}

	keys := hunt.DefaultKeys
	if huntKeysCSV != "" {
		keys = strings.Split(huntKeysCSV, ",")
	}

	explorerKind, explorerArg := splitExplorerSpec(huntExplorer)
	var recordedEvents []hunt.Event
	if explorerKind == "from-recording" {
		if explorerArg == "" {
			return fmt.Errorf("hunt: --explorer from-recording:<path.mtrr> に .mtrr パスが要ります")
		}
		recordedEvents, err = hunt.ExtractEventsFromRecording(explorerArg)
		if err != nil {
			return err
		}
		if len(recordedEvents) == 0 {
			return fmt.Errorf("hunt: %s から入力イベントを抽出できませんでした", explorerArg)
		}
	}

	outDir := filepath.Join(huntOut, time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("hunt: out dir %s: %w", outDir, err)
	}

	jobs := huntJobs
	if explorerKind == "inhuman" || explorerKind == "pairs" {
		// 決定的な有限列挙は並列に繰り返しても無意味 (同じ入力を何本並べても同じ結果)。
		if jobs != 1 {
			fmt.Printf("hunt: --explorer %s は決定的な有限探索のため --jobs は無視し 1 本で走ります\n", explorerKind)
		}
		jobs = 1
	}

	fmt.Printf("hunt: explorer=%s jobs=%d hours=%g frames=%d out=%s\n\n",
		huntExplorer, jobs, huntHours, huntFrames, outDir)

	start := time.Now()
	deadline := start.Add(time.Duration(huntHours * float64(time.Hour)))
	st := newHuntState()
	corpus := hunt.NewCorpus()

	var wg sync.WaitGroup
	for j := 0; j < jobs; j++ {
		wg.Add(1)
		go func(jobIdx int) {
			defer wg.Done()
			runHuntJob(jobIdx, huntJobParams{
				art: art, keys: keys, frames: huntFrames, timeout: huntTimeout,
				inv: inv, explorer: explorerKind,
				recorded: recordedEvents, saveKey: huntSaveKey, loadKey: huntLoadKey,
				deadline: deadline, seed: huntSeed + int64(jobIdx), corpus: corpus,
				outDir: outDir, st: st,
			})
		}(j)
	}
	wg.Wait()

	stats := hunt.Stats{
		Explorer: huntExplorer, Jobs: jobs, Duration: time.Since(start),
		Runs: st.runs, NovelStates: corpus.Reached(), NewTickets: len(st.tickets), KnownDupHits: st.knownHits,
	}
	if err := hunt.WriteReport(outDir, stats, st.tickets); err != nil {
		return err
	}
	fmt.Printf("\nhunt: %d runs, 新規チケット %d 件, 重複統合 %d 件 → %s\n",
		st.runs, len(st.tickets), st.knownHits, filepath.Join(outDir, "hunt_report.md"))
	if len(st.tickets) > 0 {
		os.Exit(1) // CI ゲート的に使うときのため: 見つかったら非ゼロ
	}
	return nil
}

func printHuntAtGuidance(at string) {
	exe, err := os.Executable()
	if err != nil {
		exe = "mitiru"
	}
	hh, mm := "02", "00"
	if parts := strings.SplitN(at, ":", 2); len(parts) == 2 {
		hh, mm = parts[0], parts[1]
	}
	fmt.Printf(`hunt --at は自前でスケジューラを持たず、Windows タスクスケジューラへの登録案内だけ出します。

以下を管理者 PowerShell で実行するとタスクが登録されます (毎日 %s:%s に起動):

  schtasks /Create /TN "MitiruHunt" /SC DAILY /ST %s:%s /TR "\"%s\" hunt --hours 6 --jobs 4" /F

タスクを消すには:

  schtasks /Delete /TN "MitiruHunt" /F

生成済みの案内 .bat が欲しい場合は tools/hunt_nightly.bat を参照してください
(docs/BUG_HUNT.md に運用手順あり)。
`, hh, mm, hh, mm, exe)
}

// splitExplorerSpec は "from-recording:<path>" のようなコロン区切り引数を分ける。
func splitExplorerSpec(spec string) (kind, arg string) {
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

// resolveHuntArtifacts は --game <dll> から deploy layout (mitiru_host.exe の隣) を推定する。
// 未指定なら通常どおり mitiru build する。
func resolveHuntArtifacts(gamePath string) (*build.Artifacts, error) {
	if gamePath == "" {
		result, err := runBuild()
		if err != nil {
			return nil, err
		}
		return result.Artifacts, nil
	}
	dllAbs, err := filepath.Abs(gamePath)
	if err != nil {
		return nil, fmt.Errorf("hunt: resolve --game %q: %w", gamePath, err)
	}
	if _, err := os.Stat(dllAbs); err != nil {
		return nil, fmt.Errorf("hunt: --game %s: %w", dllAbs, err)
	}
	targetDir := filepath.Dir(dllAbs)
	for _, deployDir := range []string{filepath.Dir(targetDir), targetDir} {
		hostExe := filepath.Join(deployDir, "mitiru_host.exe")
		if _, err := os.Stat(hostExe); err != nil {
			continue
		}
		dllRel, err := filepath.Rel(deployDir, dllAbs)
		if err != nil {
			continue
		}
		return &build.Artifacts{DeployDir: deployDir, HostExePath: hostExe, DllPath: dllAbs, DllRel: dllRel}, nil
	}
	return nil, fmt.Errorf("hunt: --game %s の隣 (または親) に mitiru_host.exe が見つかりません (deploy レイアウト外)", dllAbs)
}
