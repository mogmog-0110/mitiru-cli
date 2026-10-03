package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generatedCMakeWith(t *testing.T, projectRoot, engineRoot string, mod func(*Options)) (string, error) {
	t.Helper()
	opts := Options{
		ProjectRoot: projectRoot,
		ProjectName: "my-first-game",
		EngineRoot:  engineRoot,
		Config:      "Debug",
	}
	mod(&opts)
	srcDir, _, err := Configure(opts)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(srcDir, "CMakeLists.txt"))
	if err != nil {
		t.Fatalf("read generated CMakeLists.txt: %v", err)
	}
	return string(b), nil
}

// 何も選ばなければ、DLL は engine 本体だけを link し、焼く工程も出ない。
func TestConfigure_NoFeaturesLinksOnlyMitiru(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake := generatedCMake(t, projectRoot, engineRoot)
	for _, unwanted := range []string{"mitiru_nav", "mitiru_navbake", "[engine] features", "TARGET Jolt"} {
		if strings.Contains(cmake, unwanted) {
			t.Errorf("generated CMake mentions %q without any feature", unwanted)
		}
	}
}

// features は DLL に link し、target の無い engine では configure で止まる。jolt は確かめるだけ。
func TestConfigure_FeaturesLinkGuardedTargets(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"jolt", "navbake"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, want := range []string{
		"if(TARGET mitiru_nav_bake)\n    target_link_libraries(my_first_game PRIVATE mitiru_nav_bake)\nelse()",
		`message(FATAL_ERROR "mitiru.toml: [engine] features has \"navbake\", but this engine has no CMake target mitiru_nav_bake.\n"`,
		"if(TARGET Jolt)\nelse()",
		"git submodule update --init external/jolt",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
	if strings.Contains(cmake, "PRIVATE Jolt") {
		t.Error("jolt is already linked into mitiru; the DLL should not link it again")
	}
	// link は DLL の定義より後
	if strings.Index(cmake, "add_library(my_first_game SHARED") > strings.Index(cmake, "PRIVATE mitiru_nav_bake") {
		t.Error("features must come after the game DLL target is defined")
	}
}

func TestConfigure_UnknownFeatureFails(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"teleport"}
	})
	if err == nil || !strings.Contains(err.Error(), `unknown feature "teleport"`) {
		t.Fatalf("want an unknown-feature error, got %v", err)
	}
}

// [nav] source は mitiru_navbake を建て、DLL の隣の同じ相対位置へ .navmesh を焼く。
func TestConfigure_NavSourceBakesNextToDll(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeFile(t, filepath.Join(projectRoot, "assets", "maps", "arena.obj"), "v 0 0 0\n")
	writeFile(t, filepath.Join(engineRoot, "apps", "mitiru_navbake", "main.cpp"), "// navbake\n")
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"nav"}
		o.NavSource = "assets/maps/arena.obj"
		o.NavArgs = []string{"--radius", "0.4", `odd "$x"`}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	src := toCMakePath(filepath.Join(projectRoot, "assets", "maps", "arena.obj"))
	for _, want := range []string{
		"if(NOT TARGET mitiru_nav_bake)",
		`add_executable(mitiru_navbake "` + toCMakePath(filepath.Join(engineRoot, "apps", "mitiru_navbake", "main.cpp")) + `")`,
		"target_link_libraries(mitiru_navbake PRIVATE Mitiru::mitiru mitiru_nav_bake)",
		`COMMAND ${CMAKE_COMMAND} -E make_directory "${_game_runtime_dir}/assets/maps"`,
		`COMMAND mitiru_navbake "` + src + `" -o "${_game_runtime_dir}/assets/maps/arena.navmesh" "--radius" "0.4" "odd \"\$x\""`,
		`DEPENDS mitiru_navbake "` + src + `"`,
		"add_custom_target(my_first_game_navbake ALL",
		"add_dependencies(my_first_game my_first_game_navbake)",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
	// _game_runtime_dir を使うので、その定義より後に出る
	if strings.Index(cmake, "set(_game_runtime_dir") > strings.Index(cmake, "mitiru_navbake \"") {
		t.Error("the bake step must come after _game_runtime_dir is defined")
	}
}

func TestConfigure_NavSourceMissingFileFails(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	_, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"nav"}
		o.NavSource = "assets/level.obj"
	})
	if err == nil || !strings.Contains(err.Error(), "level mesh not found") {
		t.Fatalf("want a missing-mesh error, got %v", err)
	}
}

// navbake のソースを持たない古い engine では、configure で版の上げ方を出す。
func TestConfigure_NavSourceOnOldEngineFailsAtConfigure(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeFile(t, filepath.Join(projectRoot, "assets", "level.obj"), "v 0 0 0\n")
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"nav"}
		o.NavSource = "assets/level.obj"
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if strings.Contains(cmake, "add_executable(mitiru_navbake") {
		t.Error("should not build mitiru_navbake without its source")
	}
	if !strings.Contains(cmake, "needs apps/mitiru_navbake, which this engine does not have") {
		t.Error("missing the configure-time error for old engines")
	}
}

// online は engine に GekkoNet を作らせ、host に link する。外したら option を OFF に戻す。
func TestConfigure_OnlineTurnsOnGekkoNetAndLinksTheHost(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.Features = []string{"online"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	on := `set(MITIRU_WITH_GEKKONET ON CACHE BOOL "" FORCE)`
	if i, j := strings.Index(cmake, on), strings.Index(cmake, "add_subdirectory(\"${MITIRU_ENGINE_ROOT}\""); i < 0 || j < 0 || i > j {
		t.Errorf("the option must be set before the engine is added:\n%s", cmake)
	}
	for _, want := range []string{
		"if(TARGET mitiru_rollback_net)\n    list(APPEND _mitiru_host_links mitiru_rollback_net)\nelse()",
		"target_link_libraries(mitiru_host PRIVATE Mitiru::mitiru ${_mitiru_host_links})",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
	if strings.Contains(cmake, "PRIVATE mitiru_rollback_net") {
		t.Error("the game DLL does not need the netcode; only the host runs it")
	}

	off, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if !strings.Contains(off, `set(MITIRU_WITH_GEKKONET OFF CACHE BOOL "" FORCE)`) {
		t.Error("removing online must turn the engine option off again")
	}
}
