package commands

import (
	"strings"
	"testing"
)

func TestUpdateNotices(t *testing.T) {
	cases := []struct {
		name                                  string
		enginePin, latestEngine, cliCur, cliL string
		wantEngine, wantCLI                   bool
	}{
		{"both newer", "0.7.0", "0.7.1", "0.6.0", "0.7.0", true, true},
		{"engine only", "0.7.0", "0.7.1", "0.7.0", "0.7.0", true, false},
		{"cli only", "0.7.1", "0.7.1", "0.6.0", "0.7.0", false, true},
		{"none (up to date)", "0.7.1", "0.7.1", "0.7.1", "0.7.1", false, false},
		{"none (local ahead)", "0.8.0", "0.7.1", "0.9.0", "0.7.0", false, false},
		{"latest empty (offline)", "0.7.0", "", "0.7.0", "", false, false},
		{"pin unparseable, cli newer", "latest", "0.7.1", "0.6.0", "0.7.0", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := updateNotices(c.enginePin, c.latestEngine, c.cliCur, c.cliL)
			joined := strings.Join(got, "\n")
			hasEngine := strings.Contains(joined, "mitiru update")
			hasCLI := strings.Contains(joined, "mitiru self-update")
			if hasEngine != c.wantEngine {
				t.Errorf("engine notice = %v; want %v (lines: %q)", hasEngine, c.wantEngine, got)
			}
			if hasCLI != c.wantCLI {
				t.Errorf("cli notice = %v; want %v (lines: %q)", hasCLI, c.wantCLI, got)
			}
		})
	}
}
