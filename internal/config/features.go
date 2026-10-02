package config

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// EngineSection は engine の任意ライブラリのうち、ゲーム DLL へ link するものを選ぶ。
// engine 本体 (mitiru) に入っていない部品は、使うプロジェクトだけが opt-in する。
type EngineSection struct {
	Features []string `toml:"features"`
}

// NavSection はビルドの一工程でレベルのメッシュからナビメッシュを焼く設定。
type NavSection struct {
	// Source はレベルのメッシュ (.obj / .gltf / .glb)。project root からの相対。
	// 焼いた .navmesh は DLL の隣の同じ相対位置に、拡張子だけ変えて置く。
	Source string `toml:"source"`
	// Args は mitiru_navbake にそのまま渡す option (例 ["--radius", "0.4"])。
	Args []string `toml:"args"`
}

// EngineFeature は [engine] features の 1 項目が生成 CMake に求めるもの。
type EngineFeature struct {
	Name string
	// Target は engine が出す CMake target。無ければ configure で止める。
	Target string
	// Link が false の target は engine が mitiru 本体へ既に link しているので、有無だけ確かめる。
	Link bool
	// Missing は Target が無いときに直し方として出す 1 行。
	Missing string
}

var engineFeatures = []EngineFeature{
	{
		Name: "nav", Target: "mitiru_nav", Link: true,
		Missing: "the engine was fetched without external/recastnavigation (git submodule update --init external/recastnavigation)",
	},
	{
		Name: "navbake", Target: "mitiru_nav_bake", Link: true,
		Missing: "the engine was fetched without external/recastnavigation (git submodule update --init external/recastnavigation)",
	},
	{
		Name: "jolt", Target: "Jolt", Link: false,
		Missing: "the engine was fetched without external/jolt (git submodule update --init external/jolt)",
	},
}

// 間違えやすい名前には、正しい名前を添えて返す。
var featureHints = map[string]string{
	"crowd":   `NavCrowd is part of "nav"`,
	"detour":  `use "nav"`,
	"navmesh": `use "nav" to load a .navmesh, and [nav] source to bake one at build time`,
	"recast":  `use "navbake" (bake a navmesh inside the DLL)`,
	"physics": `use "jolt"`,
	"fbx":     "FBX import is always part of the engine; remove it",
	"gameai":  `mitiru/gameai is header-only; add "nav" only if enemies follow a navmesh`,
	"action":  "mitiru/action is header-only; remove it",
}

var navSourceExts = map[string]bool{".obj": true, ".gltf": true, ".glb": true}

// FeatureNames は使える feature の名前を並べて返す (エラー文と README 用)。
func FeatureNames() []string {
	names := make([]string, 0, len(engineFeatures))
	for _, f := range engineFeatures {
		names = append(names, f.Name)
	}
	return names
}

// ResolveFeatures は名前の並びを EngineFeature に引く。重複は 1 つにまとめ、
// 並びは表の順にそろえる (生成 CMake が書き順で変わらないように)。
func ResolveFeatures(names []string) ([]EngineFeature, error) {
	want := map[string]bool{}
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if !isKnownFeature(name) {
			return nil, unknownFeatureError(raw)
		}
		want[name] = true
	}
	out := make([]EngineFeature, 0, len(want))
	for _, f := range engineFeatures {
		if want[f.Name] {
			out = append(out, f)
		}
	}
	return out, nil
}

func isKnownFeature(name string) bool {
	for _, f := range engineFeatures {
		if f.Name == name {
			return true
		}
	}
	return false
}

func unknownFeatureError(name string) error {
	msg := fmt.Sprintf("[engine] features: unknown feature %q (known: %s)",
		name, strings.Join(FeatureNames(), ", "))
	if hint, ok := featureHints[strings.ToLower(strings.TrimSpace(name))]; ok {
		msg += "; " + hint
	}
	return fmt.Errorf("%s", msg)
}

// HasFeature は [engine] features に name があるかを返す (大文字小文字は区別しない)。
func (c *ProjectConfig) HasFeature(name string) bool {
	for _, f := range c.Engine.Features {
		if strings.EqualFold(strings.TrimSpace(f), name) {
			return true
		}
	}
	return false
}

func (c *ProjectConfig) validateEngine(manifest string) error {
	if _, err := ResolveFeatures(c.Engine.Features); err != nil {
		return fmt.Errorf("%s: %w", manifest, err)
	}
	// standalone は自前の CMake が engine を取り込むので、mitiru は何も link できない。
	if c.Standalone() && (len(c.Engine.Features) > 0 || c.Nav.Source != "") {
		return fmt.Errorf("%s: [engine] features and [nav] only apply to host projects; "+
			"a standalone project links engine targets in its own CMakeLists.txt", manifest)
	}
	if c.Nav.Source == "" {
		if len(c.Nav.Args) > 0 {
			return fmt.Errorf("%s: nav.args needs nav.source (the level mesh to bake)", manifest)
		}
		return nil
	}
	if err := validateNavSource(c.Nav.Source); err != nil {
		return fmt.Errorf("%s: %w", manifest, err)
	}
	// 焼いた .navmesh を DLL が読むには Detour を link していないといけない。
	if !c.HasFeature("nav") && !c.HasFeature("navbake") {
		return fmt.Errorf("%s: nav.source bakes a .navmesh, but the game DLL cannot read it without Detour; "+
			"add \"nav\" to [engine] features", manifest)
	}
	return nil
}

func validateNavSource(src string) error {
	slashed := toSlash(src)
	if filepath.IsAbs(src) || strings.HasPrefix(slashed, "/") || (len(slashed) > 1 && slashed[1] == ':') {
		return fmt.Errorf("nav.source must be relative to the project root, got %q", src)
	}
	clean := path.Clean(slashed)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("nav.source must stay inside the project, got %q", src)
	}
	if ext := strings.ToLower(path.Ext(clean)); !navSourceExts[ext] {
		exts := make([]string, 0, len(navSourceExts))
		for e := range navSourceExts {
			exts = append(exts, e)
		}
		sort.Strings(exts)
		return fmt.Errorf("nav.source must be a level mesh (%s), got %q", strings.Join(exts, " / "), src)
	}
	return nil
}

// NavMeshPath は nav.source を焼いた .navmesh の、DLL の隣からの相対パス
// (スラッシュ区切り)。nav.source が無ければ空文字。
func (c *ProjectConfig) NavMeshPath() string {
	if c.Nav.Source == "" {
		return ""
	}
	return NavMeshPathFor(c.Nav.Source)
}

// NavMeshPathFor はレベルのメッシュのパスを、焼いた .navmesh のパスに変える。
func NavMeshPathFor(source string) string {
	clean := path.Clean(toSlash(source))
	return strings.TrimSuffix(clean, path.Ext(clean)) + ".navmesh"
}

// mitiru.toml は Windows で書かれることが多いので、どの OS でも \ を区切りとして読む。
func toSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }
