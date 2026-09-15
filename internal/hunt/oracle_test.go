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
