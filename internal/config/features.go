package config

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
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
	// Option は engine が既定で作らない部品を作らせる CMake の option (空なら要らない)。
	// 生成 CMake が engine を取り込む前に ON にし、feature を外したら OFF に戻す。
	Option string
	// Host が true なら Target を mitiru_host にも link する (host が実行する機能)。
	Host bool
	// ReleaseCRTOnly が true の feature は、Debug の構成では組めない (Release の CRT で作った
	// ビルド済みを静的に持つため、リンクで _ITERATOR_DEBUG_LEVEL が食い違う)。
	ReleaseCRTOnly bool
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
	{
		// オンライン協力プレイ (mitiru_host --net、hud.net*) は host が動かす。GekkoNet は
		// 静的ライブラリで、通信は Windows の Winsock を使うので、配布物に足す DLL は無い。
		Name: "online", Target: "mitiru_rollback_net", Link: false, Host: true,
		Option:  "MITIRU_WITH_GEKKONET",
		Missing: "online play needs an engine with MITIRU_WITH_GEKKONET and external/GekkoNet (git submodule update --init external/GekkoNet)",
	},
	{
		// WebGPU (Dawn) の描画の層。Dawn は静的リンクで exe に入るので、配布物に足す DLL は無い。
		// engine は Dawn の展開先を環境変数 MITIRU_DAWN_DIR (または cache の同名の変数) から読む。
		// Dawn のビルド済みは Release の CRT なので、Debug の構成では使えない。
		// mitiru-cli が固定した版を cache へ取ってきて MITIRU_DAWN_DIR を渡すので、利用者は何も用意しない。
		// Debug の構成では外して組む (Dawn は D3D12 が無い PC への逃げ道で、開発の Debug では要らない)。
		Name: "dawn", Target: "mitiru_dawn", Link: false, ReleaseCRTOnly: true,
		Option:  "MITIRU_WITH_DAWN",
		Missing: "the WebGPU layer needs an engine with src/dawn (ADR 0087); mitiru fetches the prebuilt Dawn the engine pins, or set MITIRU_DAWN_DIR to an unpacked one",
	},
	{
		// NeuralStyle.hpp / NeuralDepth.hpp。onnxruntime.dll と DirectML.dll が host の隣に要るので、使うゲームだけが opt-in する。
		Name: "onnx", Target: "mitiru_onnx", Link: false,
		Option:  "MITIRU_WITH_ONNX",
		Missing: "the neural post-process needs an engine newer than v0.45 (it adds MITIRU_WITH_ONNX) and ONNX Runtime unpacked in external/onnxruntime (include, lib, bin)",
	},
	{
		// DX12DirectML.hpp / DX12NeuralFx.hpp / DX12NeuralRelight.hpp。onnx とは別で、DirectML.dll だけを host の隣に置く。
		Name: "directml", Target: "mitiru_directml", Link: false,
		Option:  "MITIRU_WITH_DIRECTML",
		Missing: "the DirectML post-process needs an engine newer than v0.45 (it adds MITIRU_WITH_DIRECTML) and the DirectML SDK unpacked in external/directml (include, lib)",
	},
	{
		// .efkefc を drawModel で描く。Effekseer は静的リンクで host の exe に入るので、使わないゲームは外す。
		Name: "effekseer", Target: "mitiru_effekseer", Link: false,
		Option:  "MITIRU_WITH_EFFEKSEER",
		Missing: "Effekseer effects need external/Effekseer and its LLGI (git submodule update --init --checkout --depth 1 external/Effekseer)",
	},
	{
		// Live2D のモデルを描く。Cubism Core とその表記が配布物に入るので、使うゲームだけが opt-in する。
		Name: "cubism", Target: "Live2DCubismFrameworkD3D", Link: false,
		Option:  "MITIRU_WITH_CUBISM",
		Missing: "Live2D needs the Cubism SDK for Native unpacked as external/cubism/CubismSdkForNative-*",
	},
	{
		// MPEG-1 (.mpg) の動画を Screen::drawVideo で映す。復号の pl_mpeg は 1 ヘッダーで、host が約 31 KB 太る。
		// 動画を使わないゲームの出荷物には、復号器も pl_mpeg の表記も入らない。
		Name: "video", Target: "mitiru_video", Link: false,
		Option:  "MITIRU_WITH_VIDEO",
		Missing: "video needs an engine newer than v0.47 (it adds MITIRU_WITH_VIDEO and external/pl_mpeg)",
	},
}

// DropForConfig は buildConfig では組めない feature を外した名前の並びと、外した feature の名前を返す。
// 外すのは Debug だけ (MinSizeRel などは Release の CRT なので組める)。
func DropForConfig(names []string, buildConfig string) (kept, dropped []string) {
	if buildConfig != "" && !strings.EqualFold(buildConfig, "Debug") {
		return names, nil
	}
	releaseOnly := map[string]bool{}
	for _, f := range engineFeatures {
		if f.ReleaseCRTOnly {
			releaseOnly[f.Name] = true
		}
	}
	for _, raw := range names {
		if name := strings.ToLower(strings.TrimSpace(raw)); releaseOnly[name] {
			if !slices.Contains(dropped, name) {
				dropped = append(dropped, name)
			}
			continue
		}
		kept = append(kept, raw)
	}
	return kept, dropped
}

// FeatureOptions は Option を持つ feature ごとに、selected に含まれるかを返す (表の順)。
// 生成 CMake はこれで option を ON / OFF に固定する。
func FeatureOptions(selected []EngineFeature) []FeatureOption {
	on := map[string]bool{}
	for _, f := range selected {
		on[f.Name] = true
	}
	var out []FeatureOption
	for _, f := range engineFeatures {
		if f.Option != "" {
			out = append(out, FeatureOption{Name: f.Option, On: on[f.Name]})
		}
	}
	return out
}

// FeatureOption は engine の CMake option 1 つの値。
type FeatureOption struct {
	Name string
	On   bool
}

// 間違えやすい名前には、正しい名前を添えて返す。
var featureHints = map[string]string{
	"crowd":    `NavCrowd is part of "nav"`,
	"detour":   `use "nav"`,
	"navmesh":  `use "nav" to load a .navmesh, and [nav] source to bake one at build time`,
	"recast":   `use "navbake" (bake a navmesh inside the DLL)`,
	"physics":  `use "jolt"`,
	"net":      `use "online"`,
	"netplay":  `use "online"`,
	"rollback": `use "online"`,
	"gekkonet": `use "online"`,
	"fbx":      "FBX import is always part of the engine; remove it",
	"gameai":   `mitiru/gameai is header-only; add "nav" only if enemies follow a navmesh`,
	"action":   "mitiru/action is header-only; remove it",
	"ort":      `use "onnx" (NeuralStyle / NeuralDepth)`,
	"neural":   `use "onnx" (NeuralStyle / NeuralDepth) or "directml" (neural post-process in the DX12 frame)`,
	"efk":      `use "effekseer"`,
	"live2d":   `use "cubism"`,
	"mpeg":     `use "video" (MPEG-1 .mpg through Screen::drawVideo)`,
	"movie":    `use "video" (MPEG-1 .mpg through Screen::drawVideo)`,
	"mp4":      `video plays MPEG-1 (.mpg) only, with the feature "video"; H.264 / MP4 is not supported`,
	"h264":     `video plays MPEG-1 (.mpg) only, with the feature "video"; H.264 / MP4 is not supported`,
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
