package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

func configWithFeatures(names ...string) *config.ProjectConfig {
	return &config.ProjectConfig{Engine: config.EngineSection{Features: names}}
}

// Debug では dawn を外して 1 行だけ知らせる。Dawn の用意 (取得) もしない。
func TestResolveFeatures_DebugDropsDawnWithOneLine(t *testing.T) {
	var out bytes.Buffer
	features, dawnDir, err := resolveFeatures(configWithFeatures("nav", "dawn"), "Debug", t.TempDir(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(features, ",") != "nav" || dawnDir != "" {
		t.Errorf("features=%v dawnDir=%q", features, dawnDir)
	}
	if n := strings.Count(out.String(), "\n"); n != 1 || !strings.Contains(out.String(), "dawn") {
		t.Errorf("知らせは dawn を言う 1 行のはず: %q", out.String())
	}
}

// Dawn の版を固定していない engine は、取得せず engine 自身の案内 (target が無い) に任せる。
func TestResolveFeatures_EngineWithoutPinKeepsDawn(t *testing.T) {
	t.Setenv("MITIRU_CACHE_DIR", t.TempDir())
	var out bytes.Buffer
	features, dawnDir, err := resolveFeatures(configWithFeatures("dawn"), "RelWithDebInfo", t.TempDir(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(features, ",") != "dawn" || dawnDir != "" || out.Len() != 0 {
		t.Errorf("features=%v dawnDir=%q out=%q", features, dawnDir, out.String())
	}
}
