package commands

// fuzz.go ── ランダム決定論入力でゲームを叩き、crash / 非決定性 / 不変条件違反を釣り、
// 失敗入力を最小化して再現可能な .mtrr / .txt に残す (engine tools/replay_fuzz.py の Go 昇格)。

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	fuzzIters   int
	fuzzFrames  int
	fuzzSeed    int64
	fuzzAsserts []string
	fuzzTimeout int
)

var fuzzKeys = []string{"Left", "Right", "Down", "Up"}

func newFuzzCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fuzz",
		Short: "Hunt for crashes, nondeterminism, and invariant violations with random input",
		Long: `Throws randomized deterministic input at THIS project's game and watches for
three failure classes, each caught for free by the single flat-POD GameMemory +
bit-exact replay substrate:

  1. crash          -- the recording run dies / produces no .mtrr
  2. nondeterminism -- the same input DIVERGEs when replayed
  3. invariant      -- the final reflected state fails an --assert

A found failure is delta-debug minimized into a reproducing input script and a
.mtrr -- because every random run is a recorded replay, any failure is a
100%-reproducible bug ticket. Exit code is non-zero on a found failure (CI gate).

  mitiru fuzz --iters 50 --assert "playerX<=1232" --assert "playerX>=48"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFuzz()
		},
	}
	cmd.Flags().IntVar(&fuzzIters, "iters", 30, "number of random inputs to try")
	cmd.Flags().IntVar(&fuzzFrames, "frames", 300, "frames per run")
	cmd.Flags().Int64Var(&fuzzSeed, "seed", 1, "RNG seed (same seed reproduces the same inputs)")
	cmd.Flags().StringArrayVar(&fuzzAsserts, "assert", nil,
		`invariant "field op num" (e.g. "playerX<=1232"); repeatable`)
	cmd.Flags().IntVar(&fuzzTimeout, "timeout", 120, "per-run host timeout (seconds)")
	return cmd
}

// genInput はキーを掴んでは離す segment 列を生成する (持続入力 = 端まで寄る等の状況に届く)。
func genInput(rng *rand.Rand, frames int) []string {
	events := []string{}
	for f := 1; f < frames-5; {
		key := fuzzKeys[rng.Intn(len(fuzzKeys))]
		hold := 8 + rng.Intn(63) // 8..70
		up := f + hold
		if up > frames-2 {
			up = frames - 2
		}
		events = append(events, fmt.Sprintf("%d %s down", f, key))
		events = append(events, fmt.Sprintf("%d %s up", up, key))
		f += hold + rng.Intn(13)
	}
	return events
}

// fuzzRun は events を record→replay-test し verdict ("crash"/"nondeterminism"/"ok") と
// ok のときの final JSON を返す。paths は絶対 (host cwd=deployDir なので相対は誤解される)。
func fuzzRun(hostExe, deployDir, dllRel, work string, events []string, frames, timeout int) (string, string) {
	script := filepath.Join(work, "in.txt")
	if os.WriteFile(script, []byte(strings.Join(events, "\n")+"\n"), 0o644) != nil {
		return "crash", ""
	}
	mtrr := filepath.Join(work, "in.mtrr")
	os.Remove(mtrr)

	_, code, timedOut := runHostCaptured(hostExe, deployDir, timeout,
		dllRel, "--size", "1280x720", "--input-script", script, "--record", mtrr,
		"--max-frames", strconv.Itoa(frames), "--window-pos", "-2200", "0", "--no-tool-windows")
	if timedOut || code != 0 {
		return "crash", ""
	}
	if _, err := os.Stat(mtrr); err != nil {
		return "crash", ""
	}

	rout, _, rTimedOut := runHostCaptured(hostExe, deployDir, timeout,
		dllRel, "--replay-test", mtrr, "--window-pos", "-2200", "0", "--no-tool-windows")
	if rTimedOut {
		return "crash", ""
	}
	if reDiverged.MatchString(rout) {
		return "nondeterminism", ""
	}
	if m := reReplayFinal.FindStringSubmatch(rout); m != nil {
		return "ok", m[1]
	}
	return "ok", ""
}

func fuzzFails(verdict, finalJSON string, inv []invariant) bool {
	if verdict == "crash" || verdict == "nondeterminism" {
		return true
	}
	return checkInvariants(finalJSON, inv) != ""
}

// minimizeFuzz は delta-debug: event を間引いてもまだ失敗するなら採用し、最小の再現列を得る。
func minimizeFuzz(hostExe, deployDir, dllRel, work string, events []string, inv []invariant, frames, timeout int) []string {
	cur := append([]string{}, events...)
	for changed := true; changed && len(cur) > 2; {
		changed = false
		for i := 0; i < len(cur); {
			trial := append(append([]string{}, cur[:i]...), cur[i+1:]...)
			v, fj := fuzzRun(hostExe, deployDir, dllRel, work, trial, frames, timeout)
			if fuzzFails(v, fj, inv) {
				cur = trial
				changed = true
			} else {
				i++
			}
		}
	}
	return cur
}

func runFuzz() error {
	inv, err := parseInvariants(fuzzAsserts)
	if err != nil {
		return err
	}
	result, err := runBuild()
	if err != nil {
		return err
	}
	art := result.Artifacts

	work, err := os.MkdirTemp("", "mitiru-fuzz-")
	if err != nil {
		return fmt.Errorf("fuzz: temp dir: %w", err)
	}
	rng := rand.New(rand.NewSource(fuzzSeed))

	fmt.Printf("fuzz: %d 入力, %df, seed=%d, 不変条件 %d 個\n\n", fuzzIters, fuzzFrames, fuzzSeed, len(inv))
	found := false
	for i := 0; i < fuzzIters; i++ {
		events := genInput(rng, fuzzFrames)
		verdict, fj := fuzzRun(art.HostExePath, art.DeployDir, art.DllRel, work, events, fuzzFrames, fuzzTimeout)
		if !fuzzFails(verdict, fj, inv) {
			fmt.Printf("  [%02d] ok\n", i+1)
			continue
		}

		var reason string
		switch verdict {
		case "crash":
			reason = "CRASH"
		case "nondeterminism":
			reason = "非決定性 (同入力で DIVERGE)"
		default:
			reason = "不変条件違反 " + checkInvariants(fj, inv)
		}
		fmt.Printf("  [%02d] FAIL: %s\n", i+1, reason)
		fmt.Printf("       最小化中 (%d events)...\n", len(events))
		minimal := minimizeFuzz(art.HostExePath, art.DeployDir, art.DllRel, work, events, inv, fuzzFrames, fuzzTimeout)
		writeFuzzRepro(art.HostExePath, art.DeployDir, art.DllRel, minimal)
		found = true
		break
	}
	os.RemoveAll(work)

	if !found {
		tail := "クラッシュ/非決定性なし"
		if len(inv) > 0 {
			tail = "不変条件も全て満たした"
		}
		fmt.Printf("\n%d/%d clean ── 決定論 OK・%s\n", fuzzIters, fuzzIters, tail)
		return nil
	}
	os.Exit(1)
	return nil
}

// writeFuzzRepro は最小再現を cwd に .txt + .mtrr で残す (.mtrr = 100% 再現するバグ票)。
func writeFuzzRepro(hostExe, deployDir, dllRel string, minimal []string) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	reproTxt := filepath.Join(cwd, "fuzz_repro.txt")
	_ = os.WriteFile(reproTxt, []byte(strings.Join(minimal, "\n")+"\n"), 0o644)
	reproMtrr := filepath.Join(cwd, "fuzz_repro.mtrr")
	os.Remove(reproMtrr)
	runHostCaptured(hostExe, deployDir, fuzzTimeout,
		dllRel, "--size", "1280x720", "--input-script", reproTxt, "--record", reproMtrr,
		"--max-frames", strconv.Itoa(fuzzFrames), "--window-pos", "-2200", "0", "--no-tool-windows")
	fmt.Printf("       最小再現 = %d events → %s\n", len(minimal), reproTxt)
	fmt.Printf("       再現リプレイ → %s\n", reproMtrr)
	for _, e := range minimal {
		fmt.Printf("         %s\n", e)
	}
}
