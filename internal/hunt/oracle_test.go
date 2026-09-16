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

// engine 側 (NumberAppend.hpp) は JSON に書けない NaN/Inf を "NaN"/"Inf"/"-Inf" 文字列で出す。
// CheckInvariants がこれを非有限値として数値扱いできることを確認する (6-3 と対の Go 側修正)。
func TestCheckInvariantsHandlesNonFiniteStrings(t *testing.T) {
	inv, err := ParseInvariants([]string{"hp < 100"})
	if err != nil {
		t.Fatalf("ParseInvariants: %v", err)
	}

	// NaN との比較は IEEE754 で常に false → "hp < 100" は破れる (Go の NaN 比較もそのまま false)。
	got := CheckInvariants(`{"hp":"NaN"}`, inv)
	if got == "" {
		t.Fatalf("CheckInvariants() with NaN field = %q, want a violation", got)
	}

	// +Inf は 100 未満ではないので同じく破れる。
	got = CheckInvariants(`{"hp":"Inf"}`, inv)
	if got == "" {
		t.Fatalf("CheckInvariants() with Inf field = %q, want a violation", got)
	}

	// -Inf は 100 未満なので違反なし。
	got = CheckInvariants(`{"hp":"-Inf"}`, inv)
	if got != "" {
		t.Fatalf("CheckInvariants() with -Inf field = %q, want no violation", got)
	}
}
