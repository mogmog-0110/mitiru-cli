package commands

// hunt_job.go ── mitiru hunt (N2) の並列ワーカー本体: 候補生成 → host プローブ →
// オラクル判定 → (バグなら) 区間削除で最小化 → 重複統合 → 起票、のループ。
// 1 プローブ = --input-script で記録 (+任意で --state-trace) → --replay-test --json で
// bit-exact 判定、という replaytools.go / fuzz.go と同じ骨格を使う。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/hunt"
)

// huntJobParams は1 job (goroutine) に必要な不変パラメータと共有状態への参照。
type huntJobParams struct {
	art      *build.Artifacts
	keys     []string
	frames   int
	timeout  int
	inv      []hunt.Invariant
	explorer string
	recorded []hunt.Event // explorer=from-recording のときだけ非空
	saveKey  string
	loadKey  string
	deadline time.Time
	seed     int64
	corpus   *hunt.Corpus
	outDir   string
	st       *huntState
}

// huntState は全 job が共有する起票済みチケットと重複統合キー。
type huntState struct {
	mu        sync.Mutex
	tickets   []hunt.Ticket
	dedup     map[string]bool
	nextN     int
	runs      int
	knownHits int
}

func newHuntState() *huntState {
	return &huntState{dedup: make(map[string]bool)}
}

func (st *huntState) incRuns() {
	st.mu.Lock()
	st.runs++
	st.mu.Unlock()
}

// runHuntJob は1 job の探索ループ。random/novelty/from-recording は時間予算いっぱい
// 回し続け、pairs/inhuman は有限列挙を1周したら終わる (呼び出し側で jobs=1 に強制済み)。
func runHuntJob(jobIdx int, p huntJobParams) {
	work, err := os.MkdirTemp("", fmt.Sprintf("mitiru-hunt-%d-", jobIdx))
	if err != nil {
		return
	}
	defer os.RemoveAll(work)
	rng := rand.New(rand.NewSource(p.seed))

	switch p.explorer {
	case "pairs":
		for _, events := range hunt.GenPairs(p.keys, p.frames) {
			if time.Now().After(p.deadline) {
				return
			}
			p.st.incRuns()
			handleHuntCandidate(p, work, events, false)
		}
		return
	case "inhuman":
		runInhumanSequence(p, work)
		return
	}

	for time.Now().Before(p.deadline) {
		events := generateHuntCandidate(p.explorer, rng, p.keys, p.frames, p.corpus, p.recorded)
		if len(events) == 0 {
			continue
		}
		p.st.incRuns()
		handleHuntCandidate(p, work, events, p.explorer == "novelty")
	}
}

func generateHuntCandidate(explorer string, rng *rand.Rand, keys []string, frames int,
	corpus *hunt.Corpus, recorded []hunt.Event) []hunt.Event {
	switch explorer {
	case "novelty":
		return hunt.ExtendRandom(rng, keys, corpus.Pick(rng), frames)
	case "from-recording":
		last := lastEventFrame(recorded)
		cut := 0
		if last > 0 {
			cut = rng.Intn(last + 1)
		}
		return hunt.ExtendRandom(rng, keys, hunt.BranchFromRecording(recorded, cut), frames)
	default: // "random" もここに落ちる
		return hunt.GenRandom(rng, keys, frames)
	}
}

func lastEventFrame(events []hunt.Event) int {
	max := 0
	for _, e := range events {
		var f int
		if _, err := fmt.Sscanf(e, "%d", &f); err == nil && f > max {
			max = f
		}
	}
	return max
}

// runInhumanSequence は「人間には出せない入力」4 種 + PUT 極端値チェックを1 パスだけ回す。
// 決定的な入力を何度繰り返しても結果は同じなので繰り返さない。
func runInhumanSequence(p huntJobParams, work string) {
	variants := [][]hunt.Event{
		hunt.GenAllKeysDown(p.keys, p.frames),
		hunt.GenAlternating(p.keys, p.frames),
		hunt.GenHold(p.keys, p.frames, 60*60), // 60fps 前提で 60 秒
	}
	if sl := hunt.GenSaveLoadEveryFrame(p.saveKey, p.loadKey, p.frames); sl != nil {
		variants = append(variants, sl)
	} else {
		fmt.Println("  inhuman: --save-key/--load-key 未指定のため save→load 連打チェックはスキップ")
	}
	for _, events := range variants {
		if len(events) == 0 {
			continue
		}
		p.st.incRuns()
		handleHuntCandidate(p, work, events, false)
	}

	fmt.Println("  inhuman: PUT /api/ai/state 極端値チェック中...")
	p.st.incRuns()
	if finding := huntPutExtreme(p.art, p.timeout); finding.IsBug() {
		p.st.reportFinding(p, work,
			[]hunt.Event{"# put-extreme: 再現は meta.txt の reason 通りに /api/ai/state へ PUT する"},
			finding, false)
	}
}

func handleHuntCandidate(p huntJobParams, work string, events []hunt.Event, wantTrace bool) {
	pr := huntProbeOnce(p.art, work, events, p.frames, p.timeout, p.inv, wantTrace)
	if wantTrace && len(pr.hashes) > 0 {
		if newCount := p.corpus.Absorb(pr.hashes); newCount > 0 {
			p.corpus.AddSeed(events, newCount)
		}
	}
	if pr.finding.IsBug() {
		p.st.reportFinding(p, work, events, pr.finding, true)
	}
}

// reportFinding は重複統合 (状態ハッシュ+因果) → (minimizable なら) 区間削除で最小化 →
// hunt_out への起票、を行う (N3 + N4)。minimizable=false は PUT 極端値のような
// input-script で再現しない発見用で、そのままチケットに書く。
func (st *huntState) reportFinding(p huntJobParams, work string, events []hunt.Event, finding hunt.Finding, minimizable bool) {
	hashSrc := finding.FinalState
	if hashSrc == "" {
		hashSrc = finding.Reason
	}
	key := hunt.DedupKey(finding, hunt.StateHash(hashSrc))

	st.mu.Lock()
	if st.dedup[key] {
		st.knownHits++
		st.mu.Unlock()
		return
	}
	st.dedup[key] = true
	st.nextN++
	n := st.nextN
	st.mu.Unlock()

	t := hunt.Ticket{N: n, DedupKey: key, Explorer: p.explorer, Events: events, Finding: finding}
	if minimizable {
		kind := finding.Kind
		minimal := minimizeIntervals(events, func(candidate []string) bool {
			pr := huntProbeOnce(p.art, work, candidate, p.frames, p.timeout, p.inv, false)
			return pr.finding.Kind == kind && pr.finding.IsBug()
		})
		final := huntProbeOnce(p.art, work, minimal, p.frames, p.timeout, p.inv, false)
		t.Events = minimal
		t.Finding = final.finding
		t.MtrrSrcPath = filepath.Join(work, "run.mtrr")

		// N7: 最小化された再現手順を --http-port 付きで 1 回だけ流し直し、
		// /api/ai/state.oracle (N1) の違反イベントをチケットへ載せる。
		if oracleLines := fetchOracleForEvents(p.art, work, minimal, p.frames, p.timeout); len(oracleLines) > 0 {
			t.Finding.OracleLines = oracleLines
		}
	}

	if _, err := hunt.WriteTicket(p.outDir, t); err != nil {
		fmt.Fprintf(os.Stderr, "hunt: ticket_%02d 書き込み失敗: %v\n", n, err)
	} else {
		fmt.Printf("  [ticket_%02d] %s: %s\n", n, t.Kind, t.Reason)
	}
	st.mu.Lock()
	st.tickets = append(st.tickets, t)
	st.mu.Unlock()
}

// fetchOracleForEvents は入力列を --http-port 付きで 1 回 headless 実行し、
// /api/ai/state.oracle (N1) を読んで [oracle] 行の形へ変換する (N7)。
// host が http を開く前に叩かないよう、GET /api/ai/state の 200 を上限 5 秒ポーリングしてから読む。
// 取得できなければ (起動失敗・タイムアウト・oracle 未申告) nil を返す。チケット自体は
// この失敗と無関係に成立済みなので、呼び出し側は結果を無視してよい。
func fetchOracleForEvents(art *build.Artifacts, work string, events []hunt.Event, frames, timeoutSec int) []string {
	script := filepath.Join(work, "oracle_probe.txt")
	if os.WriteFile(script, []byte(strings.Join(events, "\n")+"\n"), 0o644) != nil {
		return nil
	}
	port, err := pickFreePort()
	if err != nil {
		return nil
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cmd := exec.Command(art.HostExePath, art.DllRel, "--headless", "--input-script", script,
		"--http-port", strconv.Itoa(port), "--max-frames", strconv.Itoa(frames), "--no-tool-windows")
	cmd.Dir = art.DeployDir
	if err := cmd.Start(); err != nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var lines []string
	if waitReadyOrHostExit(baseURL, 5*time.Second, done, cmd) == nil {
		lines, _ = hunt.FetchOracleFromApiState(baseURL)
	}

	select {
	case <-done:
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
	}
	return lines
}

// huntProbeResult は1回の host プローブ (record→replay-test) の結果。
type huntProbeResult struct {
	finding hunt.Finding
	hashes  []uint64
}

// huntProbeOnce は候補入力を記録 (+任意で state-trace) し、--replay-test --json で
// bit-exact 判定する (fuzz.go の fuzzRun と同じ骨格。hunt はハッシュ抽出と invariant を
// internal/hunt に委ねている点だけが違う)。
func huntProbeOnce(art *build.Artifacts, work string, events []hunt.Event, frames, timeoutSec int,
	inv []hunt.Invariant, wantTrace bool) huntProbeResult {
	script := filepath.Join(work, "in.txt")
	if os.WriteFile(script, []byte(strings.Join(events, "\n")+"\n"), 0o644) != nil {
		return huntProbeResult{finding: hunt.Finding{Kind: "crash", Reason: "input-script 書き込み失敗"}}
	}
	mtrr := filepath.Join(work, "run.mtrr")
	os.Remove(mtrr)

	recordArgs := []string{art.DllRel, "--headless", "--input-script", script, "--record", mtrr,
		"--max-frames", strconv.Itoa(frames), "--no-tool-windows", "--oracle-log"}
	var trace string
	if wantTrace {
		trace = filepath.Join(work, "trace.jsonl")
		os.Remove(trace)
		recordArgs = append(recordArgs, "--state-trace", trace)
	}
	out, code, timedOut := runHostCaptured(art.HostExePath, art.DeployDir, timeoutSec, recordArgs...)
	oracle := hunt.ScanOracleLines(out)
	if timedOut {
		return huntProbeResult{finding: hunt.Finding{Kind: "crash", Reason: "録画実行がタイムアウト", OracleLines: oracle}}
	}
	if code != 0 {
		return huntProbeResult{finding: hunt.Finding{Kind: "crash", Reason: fmt.Sprintf("録画実行が exit %d", code), OracleLines: oracle}}
	}
	if _, err := os.Stat(mtrr); err != nil {
		return huntProbeResult{finding: hunt.Finding{Kind: "crash", Reason: ".mtrr が生成されませんでした", OracleLines: oracle}}
	}

	var hashes []uint64
	if wantTrace {
		if f, ferr := os.Open(trace); ferr == nil {
			hashes = hunt.HashTraceFile(bufio.NewReader(f))
			f.Close()
		}
	}

	rout, rcode, rTimedOut := runHostCaptured(art.HostExePath, art.DeployDir, timeoutSec,
		art.DllRel, "--replay-test", mtrr, "--json", "--no-tool-windows", "--oracle-log")
	oracle = append(oracle, hunt.ScanOracleLines(rout)...)
	if rTimedOut {
		return huntProbeResult{finding: hunt.Finding{Kind: "crash", Reason: "replay-test がタイムアウト", OracleLines: oracle}, hashes: hashes}
	}
	finalState := extractHuntReplayFinal(rout)
	v, ok := hunt.ParseReplayVerdict([]byte(rout))
	if !ok {
		reason := "verdict JSON が出力されませんでした"
		if rcode != 0 {
			reason = fmt.Sprintf("replay-test が exit %d (verdict なし)", rcode)
		}
		return huntProbeResult{finding: hunt.Finding{Kind: "crash", Reason: reason, OracleLines: oracle}, hashes: hashes}
	}
	if v.Verdict != "PASS" {
		diff := ""
		if len(v.Diff) > 0 {
			diff = string(v.Diff)
		}
		return huntProbeResult{finding: hunt.Finding{
			Kind: "nondeterminism", Reason: v.Reason, DivergedFrame: v.DivergedFrame,
			Diff: diff, Blame: v.Blame, FinalState: finalState, OracleLines: oracle,
		}, hashes: hashes}
	}
	if len(oracle) > 0 {
		return huntProbeResult{finding: hunt.Finding{Kind: "oracle", Reason: strings.Join(oracle, " / "), FinalState: finalState, OracleLines: oracle}, hashes: hashes}
	}
	if reason := hunt.CheckInvariants(finalState, inv); reason != "" {
		return huntProbeResult{finding: hunt.Finding{Kind: "invariant", Reason: reason, FinalState: finalState}, hashes: hashes}
	}
	return huntProbeResult{finding: hunt.Finding{Kind: "ok", FinalState: finalState}, hashes: hashes}
}

func extractHuntReplayFinal(out string) string {
	if m := reReplayFinal.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// huntPutExtreme はゲームを MITIRU_AI=1 でライブ起動し、reflect state の数値 field 全部に
// 極端値 (±1e300, ±1e18) を PUT して host が生き続けるか・NaN/Inf が state に混ざらないかを見る
// (「極端値 PUT」。record/replay の入力列では再現できない別系統の探索なので、ここだけ
// aiplaytest.go と同じ HTTP 観測 API 経由になる)。
func huntPutExtreme(art *build.Artifacts, timeoutSec int) hunt.Finding {
	port, err := pickFreePort()
	if err != nil {
		return hunt.Finding{Kind: "ok"}
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	hostArgs := append([]string{art.DllRel}, tomlHostArgs()...)
	hostArgs = append(hostArgs, "--window-pos", "-2200", "0", "--no-tool-windows")
	hostCmd := exec.Command(art.HostExePath, hostArgs...)
	hostCmd.Dir = art.DeployDir
	hostCmd.Env = append(build.HostEnv(), "MITIRU_AI=1", fmt.Sprintf("MITIRU_AI_PORT=%d", port))
	if err := hostCmd.Start(); err != nil {
		return hunt.Finding{Kind: "ok"}
	}
	hostExit := make(chan error, 1)
	go func() { hostExit <- hostCmd.Wait() }()
	defer func() {
		if hostCmd.Process == nil {
			return
		}
		_, _, _ = apiPost(baseURL, apiQuit, nil, "")
		select {
		case <-hostExit:
		case <-time.After(3 * time.Second):
			_ = hostCmd.Process.Kill()
		}
	}()

	if err := waitReadyOrHostExit(baseURL, time.Duration(timeoutSec)*time.Second, hostExit, hostCmd); err != nil {
		return hunt.Finding{Kind: "crash", Reason: "put-extreme: host が起動しませんでした: " + err.Error()}
	}

	body0, status0, err := apiGet(baseURL, apiState)
	if err != nil || status0 != 200 {
		return hunt.Finding{Kind: "ok"} // 観測できないゲームは対象外扱い (推測で判定しない)
	}
	var fields map[string]interface{}
	if json.Unmarshal(body0, &fields) != nil {
		return hunt.Finding{Kind: "ok"}
	}

	for name, v := range fields {
		if _, ok := v.(float64); !ok {
			continue
		}
		for _, ex := range []float64{1e300, -1e300, 1e18, -1e18} {
			if f := huntTryPutExtreme(baseURL, hostExit, name, ex); f.IsBug() {
				return f
			}
		}
	}
	return hunt.Finding{Kind: "ok"}
}

// huntTryPutExtreme は1 field に1つの極端値を PUT し、host 生存と state の NaN/Inf 混入を見る。
func huntTryPutExtreme(baseURL string, hostExit <-chan error, field string, value float64) hunt.Finding {
	payload := fmt.Sprintf(`{%q:%v}`, field, value)
	if _, _, err := apiPut(baseURL, apiState, strings.NewReader(payload), "application/json"); err != nil {
		return hunt.Finding{Kind: "crash", Reason: fmt.Sprintf("PUT %s=%v で host が応答不能: %v", field, value, err)}
	}
	select {
	case herr := <-hostExit:
		return hunt.Finding{Kind: "crash", Reason: fmt.Sprintf("PUT %s=%v の直後に host が終了しました (%v)", field, value, herr)}
	default:
	}
	after, statusAfter, err := apiGet(baseURL, apiState)
	if err != nil || statusAfter != 200 {
		return hunt.Finding{Kind: "crash", Reason: fmt.Sprintf("PUT %s=%v の後に state が取得できません", field, value)}
	}
	lower := strings.ToLower(string(after))
	if strings.Contains(lower, "nan") || strings.Contains(lower, "inf") {
		return hunt.Finding{Kind: "invariant",
			Reason: fmt.Sprintf("PUT %s=%v の後に state へ NaN/Inf が混入", field, value), FinalState: string(after)}
	}
	return hunt.Finding{Kind: "ok"}
}
