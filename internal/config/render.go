package config

import (
	"fmt"
	"regexp"
	"strings"
)

// RenderSection は 3D 描画で宣言する機能 (ADR 0090)。宣言した重い機能だけを、ゲームを動かす host が
// renderer の初期化で作る。
type RenderSection struct {
	// Needs は mitiru_host --render-features の key (例 "oit", "render_scale")。名前の検査は組むときに、
	// pin した engine の機能の表で行う。
	Needs []string `toml:"needs"`
	// NeedsSet は needs を書いたか。書かなければ DLL に宣言を埋めず、host は今までの時期に全部を作る。
	// 空の配列 (needs = []) は「何も宣言しない」で、重い機能を作らない。
	NeedsSet bool `toml:"-"`
}

var renderNeedPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// validateRender は書式だけを見る。名前が表にあるかは engine の表が要るので、組むときに見る。
func (c *ProjectConfig) validateRender(manifest string) error {
	if c.Standalone() && c.Render.NeedsSet {
		return fmt.Errorf("%s: [render] needs only applies to host projects; "+
			"a standalone project exports mitiru_module_render_needs from its own CMakeLists.txt", manifest)
	}
	for i, raw := range c.Render.Needs {
		name := strings.TrimSpace(raw)
		if name == "" {
			return fmt.Errorf("%s: render.needs[%d] is empty", manifest, i)
		}
		if !renderNeedPattern.MatchString(name) {
			return fmt.Errorf("%s: render.needs[%d] %q must be a feature key such as \"oit\" (lowercase letters, digits, _)", manifest, i, raw)
		}
	}
	return nil
}
