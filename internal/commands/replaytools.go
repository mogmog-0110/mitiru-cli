package commands

// replaytools.go ── fuzz / bisect が共有する host probe と diff/不変条件パース。
// どちらも「単一 flat-POD GameMemory + bit-exact replay」基盤を食う決定論ツール。

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reReproduced  = regexp.MustCompile(`reproduced bit-exact`)
	reDiverged    = regexp.MustCompile(`DIVERGED at frame (\d+)`)
	reReplayDiff  = regexp.MustCompile(`replay diff: (\[.*\])`)
	reReplayFinal = regexp.MustCompile(`replay final: (\{.*\})`)
	reAssert      = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*(<=|>=|==|!=|<|>)\s*(-?[0-9.]+)\s*$`)
)

// runHostCaptured は host を deployDir を cwd に起動し、combined output と exit code を返す。
// timeout 秒を超えたら kill して timedOut=true。fuzz/bisect は headless record/replay-test
// しか叩かないので CEF は出ず taskkill 不要。
func runHostCaptured(hostExe, deployDir string, timeoutSec int, args ...string) (out string, exitCode int, timedOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, hostExe, args...)
	c.Dir = deployDir
	b, _ := c.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(b), -1, true
	}
	if c.ProcessState != nil {
		exitCode = c.ProcessState.ExitCode()
	}
	return string(b), exitCode, false
}

// diffEntry は engine の reflectDiffBlobs が出す 1 要素 {path, from, to}。
type diffEntry struct {
	Path string      `json:"path"`
	From interface{} `json:"from"`
	To   interface{} `json:"to"`
}

// formatReplayDiff は host stderr の `replay diff: [...]` を "playerX: 650→650.67" に整形 (先頭 3 件)。
func formatReplayDiff(out string) string {
	m := reReplayDiff.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	var items []diffEntry
	if json.Unmarshal([]byte(m[1]), &items) != nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for i, d := range items {
		if i >= 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s: %v→%v", d.Path, d.From, d.To))
	}
	return strings.Join(parts, ", ")
}

// invariant は --assert "field op num" 1 個。
type invariant struct {
	field string
	op    string
	bound float64
}

func parseInvariants(specs []string) ([]invariant, error) {
	out := make([]invariant, 0, len(specs))
	for _, s := range specs {
		m := reAssert.FindStringSubmatch(s)
		if m == nil {
			return nil, fmt.Errorf("bad --assert: %q (expected \"field op num\", 例 \"playerX<=1232\")", s)
		}
		b, _ := strconv.ParseFloat(m[3], 64)
		out = append(out, invariant{field: m[1], op: m[2], bound: b})
	}
	return out, nil
}

func cmpOp(op string, a, b float64) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "==":
		return a == b
	case "!=":
		return a != b
	}
	return false
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// checkInvariants は `replay final: {json}` を不変条件に照らす。違反説明を返す ("" = 違反なし)。
// field が無い / 数値でない場合はその不変条件を skip (MITIRU_REFLECT 宣言済の field のみ判定可)。
func checkInvariants(finalJSON string, inv []invariant) string {
	var st map[string]interface{}
	if finalJSON == "" || json.Unmarshal([]byte(finalJSON), &st) != nil {
		return ""
	}
	for _, c := range inv {
		v, ok := st[c.field]
		if !ok {
			continue
		}
		num, ok := toFloat(v)
		if !ok {
			continue
		}
		if !cmpOp(c.op, num, c.bound) {
			return fmt.Sprintf("%s%s%g (実際 %s=%g)", c.field, c.op, c.bound, c.field, num)
		}
	}
	return ""
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
