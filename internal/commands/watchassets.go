package commands

import (
	"os"
	"path/filepath"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
)

// watchAssetsArgs は mitiru run と mitiru watch が host に渡す --watch-assets。配置した
// <project>/assets を見させるので、調整値の JSON を書き換えると走っているゲームに
// asset.reloaded が届く。mitiru.toml の [run] watch_assets = false で切れる。
func watchAssetsArgs(cfg *config.ProjectConfig, art *build.Artifacts, engineRoot string) []string {
	if cfg == nil || art == nil || art.DllRel == "" || !cfg.WatchAssets() || !engine.HostWatchesAssets(engineRoot) {
		return nil
	}
	rel := filepath.Join(filepath.Dir(art.DllRel), "assets")
	// host は無いフォルダを渡されると起動を止めるので、assets/ の無いプロジェクトには渡さない
	if st, err := os.Stat(filepath.Join(art.DeployDir, rel)); err != nil || !st.IsDir() {
		return nil
	}
	return []string{"--watch-assets", rel}
}
