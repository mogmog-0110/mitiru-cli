package commands

import "testing"

func TestResolveInspectPage(t *testing.T) {
	cases := []struct {
		name    string
		flagVal string
		args    []string
		want    string
		wantErr bool
	}{
		{"flag 未指定 → 窓なし", "", nil, "", false},
		{"flag 未指定 + positional はエラー", "", []string{"perf"}, "", true},
		{"--inspect 単独 (NoOptDefVal)", "inspect", nil, "inspect", false},
		{"--inspect=perf", "perf", nil, "perf", false},
		{"--inspect perf (空白区切り)", "inspect", []string{"perf"}, "perf", false},
		{"--inspect inspector → inspect に別名解決", "inspector", nil, "inspect", false},
		{"--inspect gameplay → inspect", "inspect", []string{"gameplay"}, "inspect", false},
		{"大文字も許容", "inspect", []string{"PERF"}, "perf", false},
		{"--inspect=mixer scene の二重指定はエラー", "mixer", []string{"scene"}, "", true},
		{"窓名 2 個はエラー", "inspect", []string{"perf", "mixer"}, "", true},
		{"未知の窓名はエラー", "inspect", []string{"bogus"}, "", true},
		{"timetravel", "timetravel", nil, "timetravel", false},
		{"replay", "inspect", []string{"replay"}, "replay", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveInspectPage(c.flagVal, c.args)
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("page = %q, want %q", got, c.want)
			}
		})
	}
}
