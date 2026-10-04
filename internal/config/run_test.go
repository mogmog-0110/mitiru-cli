package config

import "testing"

func TestLoad_WatchAssetsDefaultsOnAndCanBeTurnedOff(t *testing.T) {
	base := "[project]\nname = \"g\"\nengine = \"0.42.0\"\n"
	cfg, err := Load(writeManifest(t, base))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WatchAssets() {
		t.Error("watch_assets should default to true")
	}
	cfg, err = Load(writeManifest(t, base+"\n[run]\nwatch_assets = false\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WatchAssets() {
		t.Error("[run] watch_assets = false was ignored")
	}
}
