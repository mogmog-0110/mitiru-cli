package hunt

// report.go ── 起票 (N4): 見つけたバグ1件を hunt_out/<date>/ticket_<n>/ に固める。
// 重複統合は呼び出し側 (DedupKey) が済ませてから WriteTicket を呼ぶ前提。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ticket は起票1件: 最短 .mtrr + 入力スクリプト + 発生フレームの状態 JSON + (あれば) why 出力。
type Ticket struct {
	N           int
	DedupKey    string
	Explorer    string
	Events      []Event
	MtrrSrcPath string // 最小化後の再現 .mtrr のコピー元 (無ければ空)
	Why         string // `mitiru why` 相当の出力 (無ければ空)
	Finding
}

// WriteTicket は hunt_out/<date>/ticket_<n>/ に成果物を書き出し、ディレクトリを返す。
func WriteTicket(outDir string, t Ticket) (string, error) {
	dir := filepath.Join(outDir, fmt.Sprintf("ticket_%02d", t.N))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("hunt: ticket dir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repro.txt"),
		[]byte(strings.Join(t.Events, "\n")+"\n"), 0o644); err != nil {
		return dir, fmt.Errorf("hunt: write repro.txt: %w", err)
	}
	if t.MtrrSrcPath != "" {
		if data, err := os.ReadFile(t.MtrrSrcPath); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "repro.mtrr"), data, 0o644)
		}
	}
	if t.FinalState != "" {
		_ = os.WriteFile(filepath.Join(dir, "state.json"), []byte(t.FinalState), 0o644)
	}
	if t.Why != "" {
		_ = os.WriteFile(filepath.Join(dir, "why.txt"), []byte(t.Why), 0o644)
	}
	var meta strings.Builder
	fmt.Fprintf(&meta, "kind: %s\nreason: %s\nblame: %s\nexplorer: %s\ndedupKey: %s\nevents: %d\n",
		t.Kind, t.Reason, t.Blame, t.Explorer, t.DedupKey, len(t.Events))
	if t.DivergedFrame != nil {
		fmt.Fprintf(&meta, "divergedAtFrame: %d\n", *t.DivergedFrame)
	}
	if t.Diff != "" {
		fmt.Fprintf(&meta, "diff: %s\n", t.Diff)
	}
	if summary := SummarizeOracle(t.OracleLines); summary != "" {
		fmt.Fprintf(&meta, "oracle: %s\n", summary)
	}
	_ = os.WriteFile(filepath.Join(dir, "meta.txt"), []byte(meta.String()), 0o644)
	return dir, nil
}

// Stats は1回の hunt 実行の集計 (hunt_report.md の見出しに使う)。
type Stats struct {
	Explorer     string
	Jobs         int
	Duration     time.Duration
	Runs         int
	NovelStates  int // novelty explorer が到達した状態ハッシュ数 (他 explorer では 0)
	NewTickets   int
	KnownDupHits int // 既知チケットに丸められた再検出回数
}

// WriteReport は hunt_report.md を書き出す (N4: 「朝に出す」レポート)。
func WriteReport(outDir string, stats Stats, tickets []Ticket) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# hunt report — %s\n\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "- explorer: %s\n- jobs: %d\n- duration: %s\n- runs: %d\n",
		stats.Explorer, stats.Jobs, stats.Duration.Round(time.Second), stats.Runs)
	if stats.NovelStates > 0 {
		fmt.Fprintf(&b, "- novelty 到達状態数: %d\n", stats.NovelStates)
	}
	fmt.Fprintf(&b, "- 新規チケット: %d\n- 重複統合 (既知として丸めた再検出): %d\n\n",
		stats.NewTickets, stats.KnownDupHits)

	if len(tickets) == 0 {
		b.WriteString("0 件 ── クラッシュ / 非決定性 / 不変条件違反なし\n")
	} else {
		sorted := append([]Ticket{}, tickets...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].N < sorted[j].N })
		for _, t := range sorted {
			fmt.Fprintf(&b, "## ticket_%02d — %s\n\n- reason: %s\n", t.N, t.Kind, t.Reason)
			if t.Blame != "" {
				fmt.Fprintf(&b, "- blame: %s\n", t.Blame)
			}
			if t.DivergedFrame != nil {
				fmt.Fprintf(&b, "- diverged at frame: %d\n", *t.DivergedFrame)
			}
			if t.Diff != "" {
				fmt.Fprintf(&b, "- diff: %s\n", t.Diff)
			}
			if summary := SummarizeOracle(t.OracleLines); summary != "" {
				fmt.Fprintf(&b, "- oracle: %s\n", summary)
			}
			fmt.Fprintf(&b, "- explorer: %s\n- events: %d\n- dedupKey: `%s`\n\n",
				t.Explorer, len(t.Events), t.DedupKey)
		}
	}
	return os.WriteFile(filepath.Join(outDir, "hunt_report.md"), []byte(b.String()), 0o644)
}
