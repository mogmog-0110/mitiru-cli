package commands

import (
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

func TestResolveBuildConfigOrder(t *testing.T) {
	defer func() { buildConfigName, buildRelease = "", false }()
	pc := &config.ProjectConfig{}
	pc.Build.Config = "RelWithDebInfo"

	buildConfigName, buildRelease = "", false
	if got := resolveBuildConfig(nil); got != "Debug" {
		t.Fatalf("既定は Debug のはず: %q", got)
	}
	if got := resolveBuildConfig(pc); got != "RelWithDebInfo" {
		t.Fatalf("mitiru.toml の [build] config を使うはず: %q", got)
	}
	buildRelease = true
	if got := resolveBuildConfig(pc); got != "Release" {
		t.Fatalf("--release が mitiru.toml より先のはず: %q", got)
	}
	buildConfigName = "Debug"
	if got := resolveBuildConfig(pc); got != "Debug" {
		t.Fatalf("--config が一番先のはず: %q", got)
	}
}
