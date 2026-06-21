package commands

import "testing"

func TestParseInvariants(t *testing.T) {
	inv, err := parseInvariants([]string{"playerX<=1232", "  playerX >= 48 ", "score==0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv) != 3 {
		t.Fatalf("want 3 invariants, got %d", len(inv))
	}
	if inv[0].field != "playerX" || inv[0].op != "<=" || inv[0].bound != 1232 {
		t.Errorf("inv[0] mismatch: %+v", inv[0])
	}
	if inv[1].op != ">=" || inv[1].bound != 48 {
		t.Errorf("inv[1] mismatch: %+v", inv[1])
	}
	if _, err := parseInvariants([]string{"not a valid assert"}); err == nil {
		t.Errorf("expected error on malformed assert")
	}
}

func TestCheckInvariants(t *testing.T) {
	inv, _ := parseInvariants([]string{"playerX<=1232", "playerX>=48"})

	// 違反: playerX=1790 は <=1232 を破る
	if msg := checkInvariants(`{"playerX":1790.0,"score":3,"over":false}`, inv); msg == "" {
		t.Errorf("expected violation for playerX=1790, got clean")
	}
	// 合格: 範囲内
	if msg := checkInvariants(`{"playerX":640.0,"score":3}`, inv); msg != "" {
		t.Errorf("expected clean for playerX=640, got %q", msg)
	}
	// 欠落 field は skip (MITIRU_REFLECT 宣言済の field のみ判定)
	if msg := checkInvariants(`{"score":3}`, inv); msg != "" {
		t.Errorf("missing field should be skipped, got %q", msg)
	}
	// bool field (over==1 → true) を数値化して判定
	bi, _ := parseInvariants([]string{"over==0"})
	if msg := checkInvariants(`{"over":true}`, bi); msg == "" {
		t.Errorf("expected violation: over=true should fail over==0")
	}
	// 空/壊れた JSON は違反扱いしない
	if msg := checkInvariants("", inv); msg != "" {
		t.Errorf("empty final should be clean, got %q", msg)
	}
}

func TestFormatReplayDiff(t *testing.T) {
	out := `... DIVERGED at frame 8\nreplay diff: [{"path":"playerX","from":650.0,"to":650.67}]\n`
	got := formatReplayDiff(out)
	if got != "playerX: 650→650.67" {
		t.Errorf("formatReplayDiff = %q, want playerX: 650→650.67", got)
	}
	if formatReplayDiff("no diff here") != "" {
		t.Errorf("expected empty for output without a diff line")
	}
}

func TestFuzzFails(t *testing.T) {
	inv, _ := parseInvariants([]string{"playerX<=1232"})
	cases := []struct {
		verdict string
		final   string
		want    bool
	}{
		{"crash", "", true},
		{"nondeterminism", "", true},
		{"ok", `{"playerX":1790.0}`, true}, // 不変条件違反
		{"ok", `{"playerX":640.0}`, false}, // clean
	}
	for _, c := range cases {
		if got := fuzzFails(c.verdict, c.final, inv); got != c.want {
			t.Errorf("fuzzFails(%q, %q) = %v, want %v", c.verdict, c.final, got, c.want)
		}
	}
}

func TestCmpOp(t *testing.T) {
	if !cmpOp("<=", 1232, 1232) || cmpOp("<=", 1233, 1232) {
		t.Errorf("<= boundary wrong")
	}
	if !cmpOp("!=", 1, 0) || cmpOp("==", 1, 0) {
		t.Errorf("== / != wrong")
	}
}
