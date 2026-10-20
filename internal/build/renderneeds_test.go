package build

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// engine の機能の表の写し。行の書式は RenderFeatures.hpp と同じ 1 行 1 要素。
const fakeFeatureTable = `
struct FeatureRow {};
inline constexpr std::array<FeatureRow, 6> kRows = {{
	FeatureRow{Feature::MeshDraw, "mesh_draw", "メッシュ", Region::Core, Undeclared::NotApplicable, {Full, Full, Full},
		"",
		0, BuildFlag::None, "",
		"drawMesh",
		"drawMesh"},
	FeatureRow{Feature::Oit, "oit", "半透明の WBOIT", Region::Declared, Undeclared::Fallback, {Full, Full, Reduced},
		"互換は遠い順の混ぜ",
		0, BuildFlag::None, "",
		"",
		""},
	FeatureRow{Feature::RenderScale, "render_scale", "内部解像度", Region::Declared, Undeclared::Fallback, {Full, Reduced, Reduced},
		"倍率は双線形 + FXAA で引き伸ばす",
		0, BuildFlag::None, "taa_motion_vectors",
		"setRequestedRenderScale",
		""},
	FeatureRow{Feature::Fsr3, "fsr3", "FSR 3.1", Region::Declared, Undeclared::Skip, {Full, None, None},
		"黙って双線形 + FXAA",
		0, BuildFlag::Fsr3, "render_scale",
		"",
		""},
	FeatureRow{Feature::Effekseer, "effekseer", "Effekseer", Region::Declared, Undeclared::Skip, {Full, None, None},
		".efkefc のエフェクトを描かない",
		0, BuildFlag::Effekseer, "",
		"",
		""},
	FeatureRow{Feature::NeuralFx, "neural_fx", "ニューラルの後処理", Region::Declared, Undeclared::Skip, {Full, None, None},
		"黙って何もしない",
		0, BuildFlag::DirectMl, "",
		"enableNeuralFx",
		"enableNeuralFx"},
}};
`

func writeEngineTable(t *testing.T, engineRoot string) {
	t.Helper()
	p := filepath.Join(engineRoot, filepath.FromSlash(renderFeaturesHeader))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(fakeFeatureTable), 0o644); err != nil {
		t.Fatal(err)
	}
}

func generatedRenderNeedsSource(t *testing.T, projectRoot string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectRoot, "build", "cmake", "mitiru_render_needs.cpp"))
	if err != nil {
		t.Fatalf("read generated mitiru_render_needs.cpp: %v", err)
	}
	return string(b)
}

// needs を書いたプロジェクトだけが、宣言の export を DLL に足す。
func TestConfigure_RenderNeedsAddsTheExport(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTable(t, engineRoot)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"render_scale", "oit", "OIT"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	src := toCMakePath(filepath.Join(projectRoot, "build", "cmake", "mitiru_render_needs.cpp"))
	if want := `target_sources(my_first_game PRIVATE "` + src + `")`; !strings.Contains(cmake, want) {
		t.Errorf("generated CMake is missing:\n%s", want)
	}
	body := generatedRenderNeedsSource(t, projectRoot)
	if !strings.Contains(body, `return "render_scale,oit";`) {
		t.Errorf("duplicates should be dropped and the order kept:\n%s", body)
	}
	if !strings.Contains(body, `extern "C" MITIRU_RENDER_NEEDS_EXPORT const char* mitiru_module_render_needs()`) {
		t.Errorf("export is missing:\n%s", body)
	}
}

// needs を書かなければ export を足さず、host は今までの時期に全部を作る。
func TestConfigure_NoRenderNeedsAddsNoExport(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTable(t, engineRoot)
	cmake := generatedCMake(t, projectRoot, engineRoot)
	if strings.Contains(cmake, "mitiru_render_needs") || strings.Contains(cmake, "[render] needs") {
		t.Errorf("generated CMake mentions the declaration without [render] needs:\n%s", cmake)
	}
}

// 空の宣言 (needs = []) は export を足す。何も宣言しない新体制の印になる。
func TestConfigure_EmptyRenderNeedsStillAddsTheExport(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTable(t, engineRoot)
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) { o.RenderNeedsSet = true })
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if body := generatedRenderNeedsSource(t, projectRoot); !strings.Contains(body, `return "";`) {
		t.Errorf("an empty declaration should export an empty string:\n%s", body)
	}
}

// 表に無い名前は組む前に止め、近い綴りを添える。
func TestConfigure_RenderNeedsRejectsUnknownNames(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTable(t, engineRoot)
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"oit", "render_scal"}
	})
	if err == nil {
		t.Fatal("an unknown name should stop the build")
	}
	for _, want := range []string{`"render_scal"`, `"render_scale"`, "mitiru_host --render-features"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, "build", "cmake", "mitiru_render_needs.cpp")); statErr == nil {
		t.Error("nothing should be generated when the declaration is wrong")
	}
}

// 基本の機能の名前は害が無いので警告にとどめ、宣言には入れない。
func TestConfigure_RenderNeedsWarnsOnCoreRows(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTable(t, engineRoot)
	var stderr bytes.Buffer
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"mesh_draw", "oit"}
		o.Stderr = &stderr
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if !strings.Contains(stderr.String(), "mesh_draw は基本の機能") {
		t.Errorf("warning missing: %q", stderr.String())
	}
	if body := generatedRenderNeedsSource(t, projectRoot); !strings.Contains(body, `return "oit";`) {
		t.Errorf("core rows should be dropped from the declaration:\n%s", body)
	}
}

// 外部の部品を要る行は、それを組み込む feature を足す。足せない部品は警告にとどめる。
func TestConfigure_RenderNeedsPullsInTheBuildFeature(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTable(t, engineRoot)
	var stderr bytes.Buffer
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"effekseer", "neural_fx", "fsr3"}
		o.Stderr = &stderr
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, want := range []string{"set(MITIRU_WITH_EFFEKSEER ON", "set(MITIRU_WITH_DIRECTML ON"} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing %q", want)
		}
	}
	if strings.Contains(cmake, "set(MITIRU_WITH_ONNX ON") {
		t.Error("onnx was not declared")
	}
	if !strings.Contains(stderr.String(), "fsr3") {
		t.Errorf("fsr3 cannot be added from mitiru.toml; expected a warning, got %q", stderr.String())
	}
}

// 宣言を読めない古い engine には export を足さず、警告を 1 行出す。
func TestConfigure_RenderNeedsOnOldEngineIsSkipped(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t) // 表のヘッダーが無い engine
	var stderr bytes.Buffer
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"oit"}
		o.Stderr = &stderr
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if strings.Contains(cmake, "mitiru_render_needs") {
		t.Error("an engine without the table cannot read the export")
	}
	if !strings.Contains(stderr.String(), "[render] needs を読めない") {
		t.Errorf("warning missing: %q", stderr.String())
	}
}

func TestEditDistance(t *testing.T) {
	cases := map[[2]string]int{
		{"oit", "oit"}:                      0,
		{"render_scal", "render_scale"}:     1,
		{"sky_atmosfear", "sky_atmosphere"}: 4,
		{"", "abc"}:                         3,
		{"volumetricfog", "volumetric_fog"}: 1,
	}
	for pair, want := range cases {
		if got := editDistance(pair[0], pair[1]); got != want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", pair[0], pair[1], got, want)
		}
	}
}

// 表の書式を説明する注釈が行の目印と同じ字面を含んでも、行として読まない。文字列の中の // は残す。
func TestReadFeatureTable_IgnoresCommentsThatMentionTheRowMarker(t *testing.T) {
	engineRoot := t.TempDir()
	p := filepath.Join(engineRoot, filepath.FromSlash(renderFeaturesHeader))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "/// 表の行は `FeatureRow{Feature::X, \"key\"` の書式\n" + fakeFeatureTable +
		"// FeatureRow{Feature::Z, \"zzz\", 末尾の注釈\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := readFeatureTable(engineRoot)
	if err != nil {
		t.Fatalf("readFeatureTable: %v", err)
	}
	if len(rows) != 6 || rows[0].Key != "mesh_draw" || rows[5].Key != "neural_fx" {
		t.Errorf("rows = %+v", rows)
	}
	if got := stripLineComments(`a "http://x" // b` + "\nc"); got != "a \"http://x\" \nc" {
		t.Errorf("stripLineComments = %q", got)
	}
}
