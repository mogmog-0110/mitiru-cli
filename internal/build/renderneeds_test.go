package build

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// engine の render_features.json の写し。engine の tools/gen_render_features_doc.py が出す形と同じ。
const fakeFeatureTable = `{
  "schema": 1,
  "rows": [
    {"key": "mesh_draw", "label": "メッシュ", "region": "core", "declarable": false, "support": {"dx12": "full", "dawn": "full", "dawn_compat": "full"}, "build": "none", "engine_feature": ""},
    {"key": "oit", "label": "半透明の WBOIT", "region": "declared", "declarable": true, "support": {"dx12": "full", "dawn": "full", "dawn_compat": "reduced"}, "build": "none", "engine_feature": ""},
    {"key": "render_scale", "label": "内部解像度", "region": "declared", "declarable": true, "support": {"dx12": "full", "dawn": "reduced", "dawn_compat": "reduced"}, "build": "none", "engine_feature": ""},
    {"key": "fsr3", "label": "FSR 3.1", "region": "declared", "declarable": true, "support": {"dx12": "full", "dawn": "none", "dawn_compat": "none"}, "build": "fsr3", "engine_feature": ""},
    {"key": "effekseer", "label": "Effekseer", "region": "declared", "declarable": true, "support": {"dx12": "full", "dawn": "none", "dawn_compat": "none"}, "build": "effekseer", "engine_feature": "effekseer"},
    {"key": "neural_fx", "label": "ニューラルの後処理", "region": "declared", "declarable": true, "support": {"dx12": "full", "dawn": "none", "dawn_compat": "none"}, "build": "directml", "engine_feature": "directml"}
  ]
}
`

func writeEngineTableBody(t *testing.T, engineRoot, body string) {
	t.Helper()
	p := filepath.Join(engineRoot, filepath.FromSlash(renderFeaturesJSON))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeEngineTable(t *testing.T, engineRoot string) {
	t.Helper()
	writeEngineTableBody(t, engineRoot, fakeFeatureTable)
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

// 空白や並び、知らない項目は読み方に響かない。ヘッダーの字面ではなく JSON の意味を読む。
func TestReadFeatureTable_ReadsTheMeaningNotTheLayout(t *testing.T) {
	engineRoot := t.TempDir()
	writeEngineTableBody(t, engineRoot, `{"rows":[{"future":1,"engine_feature":"onnx","build":"onnx","declarable":true,"key":"neural"}],"schema":1,"extra":{}}`)
	rows, err := readFeatureTable(engineRoot)
	if err != nil {
		t.Fatalf("readFeatureTable: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "neural" || !rows[0].Declarable || rows[0].Build != "onnx" ||
		rows[0].EngineFeature == nil || *rows[0].EngineFeature != "onnx" {
		t.Errorf("rows = %+v", rows)
	}

	engineRoot = t.TempDir()
	writeEngineTable(t, engineRoot)
	rows, err = readFeatureTable(engineRoot)
	if err != nil {
		t.Fatalf("readFeatureTable: %v", err)
	}
	if len(rows) != 6 || rows[0].Key != "mesh_draw" || rows[0].Declarable || rows[5].Key != "neural_fx" {
		t.Errorf("rows = %+v", rows)
	}
}

// 読めない JSON と、読み方の違う版は、宣言の検査を黙って飛ばさず止める。
func TestReadFeatureTable_RejectsUnreadableTables(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"not json":          {`{"schema": 1, "rows": [`, "JSON を読めません"},
		"newer schema":      {`{"schema": 2, "rows": [{"key": "oit", "declarable": true, "build": "none"}]}`, "schema 2"},
		"no schema":         {`{"rows": [{"key": "oit", "declarable": true, "build": "none"}]}`, "schema 0"},
		"no rows":           {`{"schema": 1, "rows": []}`, "表の行がありません"},
		"row without key":   {`{"schema": 1, "rows": [{"declarable": true, "build": "none"}]}`, "key がありません"},
		"row without build": {`{"schema": 1, "rows": [{"key": "oit", "declarable": true}]}`, "build がありません"},
		"part without feature": {
			`{"schema": 1, "rows": [{"key": "oit", "declarable": true, "build": "newpart"}]}`, "engine_feature がありません"},
	}
	for name, c := range cases {
		engineRoot := t.TempDir()
		writeEngineTableBody(t, engineRoot, c.body)
		_, err := readFeatureTable(engineRoot)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, c.want)
		}
	}
}

// 表を読めない engine で configure を止める。古い engine と違い、黙って進めない。
func TestConfigure_RenderNeedsStopsOnUnreadableTable(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTableBody(t, engineRoot, `{"schema": 9, "rows": []}`)
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"oit"}
	})
	if err == nil || !strings.Contains(err.Error(), "機能の表を読めません") {
		t.Fatalf("an unreadable table should stop the build, got %v", err)
	}
}

// ヘッダーだけを持ち JSON を持たない engine (JSON を足す前の版) も、古い engine と同じ扱いにする。
func TestConfigure_RenderNeedsOnEngineWithHeaderOnlyIsSkipped(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	header := filepath.Join(engineRoot, "include", "mitiru", "render", "RenderFeatures.hpp")
	if err := os.MkdirAll(filepath.Dir(header), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, []byte("// FeatureRow{Feature::X, \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"anything_goes"}
		o.Stderr = &stderr
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if strings.Contains(cmake, "mitiru_render_needs") || !strings.Contains(stderr.String(), "render_features.json") {
		t.Errorf("expected a skipped export and a warning naming the JSON, got %q", stderr.String())
	}
}

// BuildFlag を組み込む feature の名前は engine の表が決める。CLI に名前の対応を持たない。
func TestConfigure_RenderNeedsFollowsTheEnginesBuildMapping(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeEngineTableBody(t, engineRoot, `{"schema": 1, "rows": [
		{"key": "custom_fx", "declarable": true, "build": "onnx", "engine_feature": "onnx"},
		{"key": "other_fx", "declarable": true, "build": "directml", "engine_feature": "effekseer"}]}`)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"custom_fx", "other_fx"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, want := range []string{"set(MITIRU_WITH_ONNX ON", "set(MITIRU_WITH_EFFEKSEER ON"} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing %q", want)
		}
	}
	if strings.Contains(cmake, "set(MITIRU_WITH_DIRECTML ON") {
		t.Error("the engine's mapping, not the BuildFlag name, picks the feature")
	}
}

// この CLI が知らない feature を要る行を宣言したら止める。宣言しない行なら通す。
func TestConfigure_RenderNeedsStopsOnUnknownBuildFlag(t *testing.T) {
	table := `{"schema": 1, "rows": [
		{"key": "oit", "declarable": true, "build": "none", "engine_feature": ""},
		{"key": "new_fx", "declarable": true, "build": "newpart", "engine_feature": "newpart"}]}`

	projectRoot, engineRoot := fakeProject(t)
	writeEngineTableBody(t, engineRoot, table)
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"oit", "new_fx"}
	})
	if err == nil {
		t.Fatal("a row that needs an unknown feature should stop the build")
	}
	for _, want := range []string{"new_fx", "newpart", "mitiru-cli"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}

	projectRoot, engineRoot = fakeProject(t)
	writeEngineTableBody(t, engineRoot, table)
	if _, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.RenderNeedsSet = true
		o.RenderNeeds = []string{"oit"}
	}); err != nil {
		t.Errorf("an undeclared row must not matter: %v", err)
	}
}
