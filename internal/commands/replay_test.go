package commands

import "testing"

func TestParseReplayVerdictSingleLine(t *testing.T) {
	stdout := []byte("{\"state\":1}\n{\"verdict\":\"PASS\",\"reason\":\"bit_exact\",\"framesCompared\":10,\"totalFrames\":10}\n")
	v, ok := parseReplayVerdict(stdout)
	if !ok {
		t.Fatalf("expected a verdict to be found")
	}
	if v.Verdict != "PASS" || v.Reason != "bit_exact" {
		t.Errorf("unexpected verdict: %+v", v)
	}
}

// host が --replay-test の bit-exact verdict に続けて --expect の verdict を出す旧経路
// (main.cpp 側は 1 行に修正済みだが、mitiru_host の別バージョンや他ツールが複数行出す
// 可能性は残る) を再現し、末尾行 (= 実際の最終判定) を採用することを固定する。
// ここを誤ると先頭の bit_exact PASS を採用して --expect の FAIL を見逃す false-green になる。
func TestParseReplayVerdictTakesLastLineWhenMultiple(t *testing.T) {
	stdout := []byte(
		"{\"state\":1}\n" +
			"{\"verdict\":\"PASS\",\"reason\":\"bit_exact\",\"framesCompared\":10,\"totalFrames\":10}\n" +
			"{\"verdict\":\"FAIL\",\"reason\":\"expect_mismatch\"}\n",
	)
	v, ok := parseReplayVerdict(stdout)
	if !ok {
		t.Fatalf("expected a verdict to be found")
	}
	if v.Verdict != "FAIL" || v.Reason != "expect_mismatch" {
		t.Errorf("expected final expect verdict to win, got %+v", v)
	}
}

func TestParseReplayVerdictNoVerdictLine(t *testing.T) {
	stdout := []byte("{\"state\":1}\nnot json\n")
	if _, ok := parseReplayVerdict(stdout); ok {
		t.Errorf("expected no verdict to be found")
	}
}
