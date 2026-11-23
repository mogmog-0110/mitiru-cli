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

// [build] の sources と bake_mml は同じ表に書ける。
func TestLoad_BuildSourcesAndBakeMml(t *testing.T) {
	cfg, err := Load(writeManifest(t, hostHeader+`
[build]
sources = ["src/*/*.cpp", "src/extra.cpp"]
bake_mml = false
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Build.Sources) != 2 || cfg.BakeMML() {
		t.Errorf("build = %+v, bake_mml = %v", cfg.Build, cfg.BakeMML())
	}
	cfg, err = Load(writeManifest(t, hostHeader))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Build.Sources) != 0 || !cfg.BakeMML() {
		t.Errorf("既定は sources なし、bake_mml true のはず: %+v", cfg.Build)
	}
}

func TestLoad_StandaloneRejectsBuildSourcesAndBakeMml(t *testing.T) {
	for _, line := range []string{`sources = ["src/a.cpp"]`, `bake_mml = false`} {
		_, err := Load(writeManifest(t, "[project]\nname = \"w\"\n\n[build]\nkind = \"standalone\"\n"+line+"\n"))
		if err == nil || !strings.Contains(err.Error(), "only apply to host projects") {
			t.Errorf("%s: standalone では断るはず: %v", line, err)
		}
	}
}

// [build] warning_level は 3 か 4 だけ。書かなければ 0 (engine の世代で決まる)。
func TestLoad_BuildWarningLevel(t *testing.T) {
	cfg, err := Load(writeManifest(t, hostHeader+"\n[build]\nwarning_level = 4\n"))
	if err != nil || cfg.Build.WarningLevel != 4 {
		t.Fatalf("warning_level = 4: cfg=%+v err=%v", cfg.Build, err)
	}
	cfg, err = Load(writeManifest(t, hostHeader))
	if err != nil || cfg.Build.WarningLevel != 0 {
		t.Fatalf("既定は 0 のはず: cfg=%+v err=%v", cfg.Build, err)
	}
	for _, bad := range []string{"1", "5", "-3"} {
		_, err := Load(writeManifest(t, hostHeader+"\n[build]\nwarning_level = "+bad+"\n"))
		if err == nil || !strings.Contains(err.Error(), "warning_level must be 3 or 4") {
			t.Errorf("warning_level = %s は断るはず: %v", bad, err)
		}
	}
}

func TestLoad_StandaloneRejectsWarningLevel(t *testing.T) {
	_, err := Load(writeManifest(t, "[project]\nname = \"w\"\n\n[build]\nkind = \"standalone\"\nwarning_level = 4\n"))
	if err == nil || !strings.Contains(err.Error(), "only apply to host projects") {
		t.Errorf("standalone では断るはず: %v", err)
	}
}
