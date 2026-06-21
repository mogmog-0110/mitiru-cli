package commands

// bisect.go ── 決定論バイセクト。回帰を入れた最初の DLL スナップショットを二分探索で
// 特定し壊れた field も出す (engine tools/replay_bisect.py の Go 昇格)。
// Python 版と違い、現行 deployed DLL を退避→終了時に自動復元する。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

var (
	bisectSnapshots string
	bisectReplay    string
	bisectTimeout   int
)

func newBisectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bisect",
		Short: "Binary-search which build introduced a determinism regression",
		Long: `Given a replay that PASSes on a good build and FAILs on a bad one, plus a
directory of DLL snapshots ordered old->new by filename, finds the first
snapshot that introduces the regression in log(N) replay-tests and reports the
broken field.

git bisect's test step, automated by deterministic-replay agreement instead of a
human verdict -- only possible because bit-exact replay lets each build be judged
mechanically (does the same input still reproduce the recorded GameMemory?).

The project is built once for the host exe; its deployed DLL is temporarily
overwritten with each snapshot and restored when bisect finishes.

  mitiru bisect --snapshots ./snaps --replay ./sweep.mtrr`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBisect()
		},
	}
	cmd.Flags().StringVar(&bisectSnapshots, "snapshots", "",
		"dir of *.dll snapshots, name-sorted old->new (required)")
	cmd.Flags().StringVar(&bisectReplay, "replay", "",
		".mtrr that PASSes on good builds and FAILs on bad (required)")
	cmd.Flags().IntVar(&bisectTimeout, "timeout", 180, "per-probe host timeout (seconds)")
	_ = cmd.MarkFlagRequired("snapshots")
	_ = cmd.MarkFlagRequired("replay")
	return cmd
}

func runBisect() error {
	absReplay, err := filepath.Abs(bisectReplay)
	if err != nil {
		return fmt.Errorf("bisect: resolve replay %q: %w", bisectReplay, err)
	}
	if _, err := os.Stat(absReplay); err != nil {
		return fmt.Errorf("bisect: replay %s: %w", absReplay, err)
	}
	snaps, err := filepath.Glob(filepath.Join(bisectSnapshots, "*.dll"))
	if err != nil {
		return fmt.Errorf("bisect: glob snapshots: %w", err)
	}
	sort.Strings(snaps)
	if len(snaps) < 2 {
		return fmt.Errorf("bisect: snapshots は 2 個以上必要 (古→新): %s", bisectSnapshots)
	}

	result, err := runBuild() // host exe + deploy layout を 1 回だけ用意
	if err != nil {
		return err
	}
	art := result.Artifacts

	// 現行 deployed DLL を退避し、bisect 終了時に必ず復元 (Python 版の手動 cp を不要に)。
	orig, err := os.ReadFile(art.DllPath)
	if err != nil {
		return fmt.Errorf("bisect: read deployed dll %s: %w", art.DllPath, err)
	}
	defer func() { _ = os.WriteFile(art.DllPath, orig, 0o644) }()

	// snap を deployed DLL に上書きして replay-test。(PASS か, 壊れた field) を返す。
	probe := func(snap string) (bool, string) {
		data, rerr := os.ReadFile(snap)
		if rerr != nil {
			return false, ""
		}
		if os.WriteFile(art.DllPath, data, 0o644) != nil {
			return false, ""
		}
		out, code, timedOut := runHostCaptured(art.HostExePath, art.DeployDir, bisectTimeout,
			art.DllRel, "--replay-test", absReplay, "--no-tool-windows", "--window-pos", "-2200", "0")
		ok := !timedOut && code == 0 && reReproduced.MatchString(out)
		return ok, formatReplayDiff(out)
	}

	fmt.Printf("bisect: %d スナップショット, replay=%s\n\n", len(snaps), filepath.Base(absReplay))

	// 端の健全性: 最古=PASS / 最新=FAIL でないとバイセクト不能。
	loOK, _ := probe(snaps[0])
	hiOK, _ := probe(snaps[len(snaps)-1])
	fmt.Printf("  %s: %s (最古)\n", filepath.Base(snaps[0]), passFail(loOK))
	fmt.Printf("  %s: %s (最新)\n", filepath.Base(snaps[len(snaps)-1]), passFail(hiOK))
	if !loOK {
		return fmt.Errorf("最古スナップショットで既に FAIL = 範囲外に回帰がある")
	}
	if hiOK {
		return fmt.Errorf("最新スナップショットでも PASS = この範囲に回帰なし")
	}

	// 二分探索: PASS する最大 index lo と FAIL する最小 index hi を詰める。
	lo, hi, tests := 0, len(snaps)-1, 0
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		ok, _ := probe(snaps[mid])
		tests++
		fmt.Printf("  → %s: %s\n", filepath.Base(snaps[mid]), passFail(ok))
		if ok {
			lo = mid
		} else {
			hi = mid
		}
	}

	_, diff := probe(snaps[hi]) // 回帰の入った snapshot の壊れた field を取り直す
	fmt.Printf("\n回帰はここで入った: %s  (直前 %s は OK)\n", filepath.Base(snaps[hi]), filepath.Base(snaps[lo]))
	fmt.Printf("  二分探索 %d 回で特定 (全 %d を試すと %d 回)\n", tests, len(snaps), len(snaps))
	if diff != "" {
		fmt.Printf("  壊れた値: %s\n", diff)
	}
	return nil
}
