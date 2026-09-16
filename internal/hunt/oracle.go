package hunt

// oracle.go ── host の出力を判定に変換する (N2)。internal/commands (fuzz/bisect/replay) と
// 同じ verdict 形式を独立して読む (import cycle を避けるため薄く複製、TODO N1 が
// `[oracle]` 行を出すようになったら ScanOracleLines がそのまま拾える設計にしてある)。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// ReplayVerdict は host --replay-test --json が出す 1 行 JSON
// (apps/mitiru_host/main.cpp の emitJsonVerdict と対で保守する)。
type ReplayVerdict struct {
	Verdict        string          `json:"verdict"`
	Reason         string          `json:"reason"`
	FramesCompared uint64          `json:"framesCompared"`
	TotalFrames    uint64          `json:"totalFrames"`
	DivergedFrame  *uint64         `json:"divergedAtFrame,omitempty"`
	Diff           json.RawMessage `json:"diff,omitempty"`
	Blame          string          `json:"blame,omitempty"`
}

// ParseReplayVerdict は stdout の最終非空行から verdict JSON を取り出す
// (internal/commands/replay.go の parseReplayVerdict と同方針)。
func ParseReplayVerdict(stdout []byte) (ReplayVerdict, bool) {
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var v ReplayVerdict
		if err := json.Unmarshal(line, &v); err == nil && v.Verdict != "" {
			return v, true
		}
	}
	return ReplayVerdict{}, false
}

var reOracleLine = regexp.MustCompile(`(?m)^\[oracle\].*$`)

// ScanOracleLines は host の combined output から N1 の組込オラクルが出す "[oracle] ..." 行を
// 拾う。N1 が未実装のうちは常に空 (呼び出し側は crash / 非0 exit / JSON verdict だけで
// 判定を続けられる)。
func ScanOracleLines(combinedOut string) []string {
	return reOracleLine.FindAllString(combinedOut, -1)
}

// oracleStateEvent は GET /api/ai/state の "oracle" 配列 1 要素 (host 側
// mitiru::observe::oracleEventsJson が積む形。engine 側は Oracle.hpp を参照)。
type oracleStateEvent struct {
	Kind    string  `json:"kind"`
	Frame   uint32  `json:"frame"`
	Field   string  `json:"field"`
	Value   float64 `json:"value"`
	Message string  `json:"message"`
}

// FetchOracleFromApiState は host の GET /api/ai/state を叩き、"oracle" 配列を
// ScanOracleLines と同じ文字列表現に変換して返す (N7: host が --http-port で起動して
// いる時の読み取り経路。--headless の record/replay-test 単発プローブのように HTTP
// サーバーを立てずに動かす経路は、これまで通り ScanOracleLines で stderr を読む)。
func FetchOracleFromApiState(baseURL string) ([]string, error) {
	resp, err := http.Get(baseURL + "/api/ai/state")
	if err != nil {
		return nil, fmt.Errorf("GET /api/ai/state: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read /api/ai/state: %w", err)
	}

	var state struct {
		Oracle []oracleStateEvent `json:"oracle"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, fmt.Errorf("parse /api/ai/state: %w", err)
	}

	out := make([]string, 0, len(state.Oracle))
	for _, e := range state.Oracle {
		out = append(out, fmt.Sprintf("[oracle] frame=%d kind=%s field=%s: %s",
			e.Frame, e.Kind, e.Field, e.Message))
	}
	return out, nil
}

var reOracleField = regexp.MustCompile(`field=(\S+):`)

// SummarizeOracle は ScanOracleLines / FetchOracleFromApiState が返す "[oracle] ..." 行の
// 束を、チケットの meta.txt / hunt_report.md に載せる 1 行へ要約する (N7)。
// 空なら "" を返す (呼び出し側はその場合 oracle 節を出さない)。
func SummarizeOracle(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	if m := reOracleField.FindStringSubmatch(lines[0]); m != nil {
		return fmt.Sprintf("NaN/range 違反 %d件、最初の違反フィールド: %s", len(lines), m[1])
	}
	return fmt.Sprintf("NaN/range 違反 %d件", len(lines))
}

// Invariant は --assert "field op num" 1個 (internal/commands の invariant と同形)。
type Invariant struct {
	Field string
	Op    string
	Bound float64
}

var reAssert = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*(<=|>=|==|!=|<|>)\s*(-?[0-9.]+)\s*$`)

func ParseInvariants(specs []string) ([]Invariant, error) {
	out := make([]Invariant, 0, len(specs))
	for _, s := range specs {
		m := reAssert.FindStringSubmatch(s)
		if m == nil {
			return nil, fmt.Errorf("bad --assert: %q (expected \"field op num\")", s)
		}
		var b float64
		_, _ = fmt.Sscanf(m[3], "%g", &b)
		out = append(out, Invariant{Field: m[1], Op: m[2], Bound: b})
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
	case string:
		// engine 側 (NumberAppend.hpp) が JSON に数値として書けない NaN/Inf を "NaN"/"Inf"/"-Inf"
		// 文字列で出すようになった (6-3)。ここで数値へ戻さないと非有限値の不変条件が常に skip される。
		switch n {
		case "NaN":
			return math.NaN(), true
		case "Inf":
			return math.Inf(1), true
		case "-Inf":
			return math.Inf(-1), true
		}
	}
	return 0, false
}

// CheckInvariants は `replay final: {json}` 相当の最終 state JSON を不変条件に照らす。
// 違反の説明文字列を返す ("" = 違反なし)。
func CheckInvariants(finalJSON string, inv []Invariant) string {
	var st map[string]interface{}
	if finalJSON == "" || json.Unmarshal([]byte(finalJSON), &st) != nil {
		return ""
	}
	for _, c := range inv {
		v, ok := st[c.Field]
		if !ok {
			continue
		}
		num, ok := toFloat(v)
		if !ok {
			continue
		}
		if !cmpOp(c.Op, num, c.Bound) {
			return fmt.Sprintf("%s%s%g (実際 %s=%g)", c.Field, c.Op, c.Bound, c.Field, num)
		}
	}
	return ""
}

// Finding は1回のプローブの判定結果。
type Finding struct {
	Kind          string // "ok" | "crash" | "nondeterminism" | "invariant" | "oracle"
	Reason        string
	DivergedFrame *uint64
	Diff          string
	Blame         string
	FinalState    string
	OracleLines   []string
}

// IsBug は再現すべきバグとして扱うか。
func (f Finding) IsBug() bool { return f.Kind != "ok" }

// DedupKey は状態ハッシュ + 因果 (blame があればそれ、無ければ reason) を結合した
// 重複統合キー (N4: 「状態ハッシュ + 因果で重複統合」)。
func DedupKey(f Finding, finalStateHash uint64) string {
	cause := f.Blame
	if cause == "" {
		cause = f.Reason
	}
	if cause == "" {
		cause = f.Kind
	}
	return fmt.Sprintf("%s|%s|%x", f.Kind, strings.TrimSpace(cause), finalStateHash)
}
