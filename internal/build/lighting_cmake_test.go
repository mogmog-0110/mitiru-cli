package build

import (
	"path/filepath"
	"strings"
	"testing"
)

const stageLightingJSON = `{"level": "stage.glb", "probes": {"min": [0,0,0], "max": [1,1,1], "spacing": 1}}`

// [lighting] source の glob は json ごとに焼く工程になり、json とレベルが変わったときだけ焼き直す。
func TestConfigure_LightingSourceBakesNextToDll(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeFile(t, filepath.Join(projectRoot, "assets", "stage.lighting.json"), stageLightingJSON)
	writeFile(t, filepath.Join(projectRoot, "assets", "stage.glb"), "glb")
	writeFile(t, filepath.Join(projectRoot, "assets", "maps", "cave.lighting.json"), `{"boxes": []}`)
	writeFile(t, filepath.Join(engineRoot, "apps", "mitiru_lightbake", "main.cpp"), "// lightbake\n")
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.LightingSources = []string{"assets/*.lighting.json", "assets/maps/cave.lighting.json", "assets/stage.lighting.json"}
		o.LightingArgs = []string{"--rays", "512"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	stage := toCMakePath(filepath.Join(projectRoot, "assets", "stage.lighting.json"))
	glb := toCMakePath(filepath.Join(projectRoot, "assets", "stage.glb"))
	cave := toCMakePath(filepath.Join(projectRoot, "assets", "maps", "cave.lighting.json"))
	for _, want := range []string{
		`add_executable(mitiru_lightbake "` + toCMakePath(filepath.Join(engineRoot, "apps", "mitiru_lightbake", "main.cpp")) + `")`,
		"target_link_libraries(mitiru_lightbake PRIVATE Mitiru::mitiru)",
		`COMMAND mitiru_lightbake "` + stage + `" -o "${_game_runtime_dir}/assets/stage.lighting.bin" "--rays" "512"`,
		`DEPENDS mitiru_lightbake "` + stage + `" "` + glb + `"`,
		`set(_light_stamp "${CMAKE_CURRENT_BINARY_DIR}/my_first_game_lightbake_assets_stage_lighting_bin_$<CONFIG>.stamp")`,
		`COMMAND ${CMAKE_COMMAND} -E make_directory "${_game_runtime_dir}/assets/maps"`,
		`COMMAND mitiru_lightbake "` + cave + `" -o "${_game_runtime_dir}/assets/maps/cave.lighting.bin" "--rays" "512"`,
		`DEPENDS mitiru_lightbake "` + cave + `"` + "\n",
		"add_custom_target(my_first_game_lightbake ALL DEPENDS ${_light_stamps})",
		"add_dependencies(my_first_game_lightbake mitiru_host)",
		"add_dependencies(my_first_game my_first_game_lightbake)",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
	// 2 つのパターンに当たった stage は 1 回だけ焼く
	if n := strings.Count(cmake, `COMMAND mitiru_lightbake "`+stage); n != 1 {
		t.Errorf("stage.lighting.json is baked %d times", n)
	}
	// 並びはパターンの順でなくパスの順 (生成 CMake が書き順で変わらないように)
	if strings.Index(cmake, cave) > strings.Index(cmake, stage) {
		t.Error("bake steps should be sorted by path")
	}
	if strings.Index(cmake, "set(_game_runtime_dir") > strings.Index(cmake, "mitiru_lightbake \"") {
		t.Error("the bake step must come after _game_runtime_dir is defined")
	}
}

func TestConfigure_LightingSourceErrors(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		src   string
		want  string
	}{
		{"glob matches nothing", nil, "assets/*.lighting.json", "no *.lighting.json found"},
		{"broken json", map[string]string{"assets/a.lighting.json": "{"}, "assets/a.lighting.json", "not valid JSON"},
		{"level is missing", map[string]string{"assets/a.lighting.json": `{"level": "gone.glb"}`},
			"assets/a.lighting.json", `level "gone.glb" not found`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projectRoot, engineRoot := fakeProject(t)
			for rel, body := range c.files {
				writeFile(t, filepath.Join(projectRoot, filepath.FromSlash(rel)), body)
			}
			_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
				o.LightingSources = []string{c.src}
			})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
}

// mitiru_lightbake のソースを持たない古い engine では、configure で版の上げ方を出す。
func TestConfigure_LightingSourceOnOldEngineFailsAtConfigure(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeFile(t, filepath.Join(projectRoot, "assets", "a.lighting.json"), `{"boxes": []}`)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.LightingSources = []string{"assets/a.lighting.json"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if strings.Contains(cmake, "add_executable(mitiru_lightbake") {
		t.Error("should not build mitiru_lightbake without its source")
	}
	if !strings.Contains(cmake, "needs apps/mitiru_lightbake, which this engine does not have") {
		t.Error("missing the configure-time error for old engines")
	}
}

func TestConfigure_NoLightingNoBakeStep(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	if cmake := generatedCMake(t, projectRoot, engineRoot); strings.Contains(cmake, "lightbake") {
		t.Error("generated CMake mentions lightbake without [lighting]")
	}
}
