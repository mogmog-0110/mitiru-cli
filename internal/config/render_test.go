package config

import (
	"strings"
	"testing"
)

// needs を書かなければ宣言なし。書いた (空の配列でも) ら宣言あり。
func TestLoad_RenderNeedsSetOnlyWhenWritten(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantSet bool
		want    []string
	}{
		{"absent", hostHeader, false, nil},
		{"section only", hostHeader + "\n[render]\n", false, nil},
		{"empty list", hostHeader + "\n[render]\nneeds = []\n", true, nil},
		{"names", hostHeader + "\n[render]\nneeds = [\"oit\", \"render_scale\"]\n", true, []string{"oit", "render_scale"}},
	}
	for _, c := range cases {
		cfg, err := Load(writeManifest(t, c.toml))
		if err != nil {
			t.Fatalf("%s: Load: %v", c.name, err)
		}
		if cfg.Render.NeedsSet != c.wantSet {
			t.Errorf("%s: NeedsSet = %v, want %v", c.name, cfg.Render.NeedsSet, c.wantSet)
		}
		if strings.Join(cfg.Render.Needs, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: Needs = %v, want %v", c.name, cfg.Render.Needs, c.want)
		}
	}
}

// 書式の誤りは、engine の表を見る前に manifest の段階で止める。
func TestLoad_RenderNeedsRejectsMalformedNames(t *testing.T) {
	for _, bad := range []string{`""`, `"Volumetric Fog"`, `"oit!"`, `"1oit"`} {
		_, err := Load(writeManifest(t, hostHeader+"\n[render]\nneeds = ["+bad+"]\n"))
		if err == nil || !strings.Contains(err.Error(), "render.needs[0]") {
			t.Errorf("needs = [%s]: want an error naming render.needs[0], got %v", bad, err)
		}
	}
}

func TestLoad_RenderNeedsIsNotForStandaloneProjects(t *testing.T) {
	_, err := Load(writeManifest(t, hostHeader+"\n[build]\nkind = \"standalone\"\n\n[render]\nneeds = [\"oit\"]\n"))
	if err == nil || !strings.Contains(err.Error(), "only applies to host projects") {
		t.Fatalf("want a host-only error, got %v", err)
	}
}
