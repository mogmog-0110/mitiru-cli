package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

// watchAssetsFixture は assets/ を配置した deploy dir と、--watch-assets を受け取る世代の engine を作る。
func watchAssetsFixture(t *testing.T) (*build.Artifacts, string) {
	t.Helper()
	dir := t.TempDir()
	deploy := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(deploy, "game", "assets", "tuning"), 0o755); err != nil {
		t.Fatal(err)
	}
	engineRoot := filepath.Join(dir, "engine")
	reload := filepath.Join(engineRoot, "include", "mitiru", "asset", "AssetReload.hpp")
	if err := os.MkdirAll(filepath.Dir(reload), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reload, []byte("#pragma once\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	art := &build.Artifacts{DeployDir: deploy, DllRel: filepath.Join("game", "game.dll")}
	return art, engineRoot
}

func TestWatchAssetsArgs_PassesDeployedAssetsByDefault(t *testing.T) {
	art, engineRoot := watchAssetsFixture(t)
	got := watchAssetsArgs(&config.ProjectConfig{}, art, engineRoot)
	want := []string{"--watch-assets", filepath.Join("game", "assets")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("watchAssetsArgs = %v, want %v", got, want)
	}
}

func TestWatchAssetsArgs_TurnedOffInManifest(t *testing.T) {
	art, engineRoot := watchAssetsFixture(t)
	off := false
	cfg := &config.ProjectConfig{Run: config.RunSection{WatchAssets: &off}}
	if got := watchAssetsArgs(cfg, art, engineRoot); got != nil {
		t.Errorf("watch_assets = false still passes %v", got)
	}
}

// host は無いフォルダを渡されると起動を止め、古い engine の host はこの引数を知らない
func TestWatchAssetsArgs_SkipsMissingFolderAndOldEngine(t *testing.T) {
	art, engineRoot := watchAssetsFixture(t)
	if got := watchAssetsArgs(&config.ProjectConfig{}, art, t.TempDir()); got != nil {
		t.Errorf("old engine got %v", got)
	}
	if err := os.RemoveAll(filepath.Join(art.DeployDir, "game", "assets")); err != nil {
		t.Fatal(err)
	}
	if got := watchAssetsArgs(&config.ProjectConfig{}, art, engineRoot); got != nil {
		t.Errorf("project without assets got %v", got)
	}
}
