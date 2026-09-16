package hunt

import "testing"

func TestSummarizeOracleEmpty(t *testing.T) {
	if got := SummarizeOracle(nil); got != "" {
		t.Fatalf("SummarizeOracle(nil) = %q, want \"\"", got)
	}
}

func TestSummarizeOracleWithField(t *testing.T) {
	lines := []string{
		"[oracle] frame=12 kind=nan field=player.hp: NaN detected",
		"[oracle] frame=13 kind=range field=player.x: out of range",
	}
	got := SummarizeOracle(lines)
	want := "NaN/range 違反 2件、最初の違反フィールド: player.hp"
	if got != want {
		t.Fatalf("SummarizeOracle() = %q, want %q", got, want)
	}
}

func TestSummarizeOracleWithoutField(t *testing.T) {
	lines := []string{"[oracle] malformed line with no field tag"}
	got := SummarizeOracle(lines)
	want := "NaN/range 違反 1件"
	if got != want {
		t.Fatalf("SummarizeOracle() = %q, want %q", got, want)
	}
}

// Oracle.hpp の reportOracleEvent が出す書式を ScanOracleLines が拾えることを確認する。
func TestScanOracleLinesMatchesEngineFormat(t *testing.T) {
	combined := "some other host stdout\n" +
		"[oracle] kind=nan frame=42 field=player.hp value=nan\n" +
		"[oracle] kind=range frame=43 field=player.x value=999.5\n" +
		"more noise\n"

	got := ScanOracleLines(combined)

	want := []string{
		"[oracle] kind=nan frame=42 field=player.hp value=nan",
		"[oracle] kind=range frame=43 field=player.x value=999.5",
	}
	if len(got) != len(want) {
		t.Fatalf("ScanOracleLines() returned %d lines, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ScanOracleLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
