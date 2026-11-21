package build

import (
	"path/filepath"
	"strings"
	"testing"
)

// assets/audio に .mml があり engine が mitiru_mmlbake を持つとき、DLL より先に焼く工程が入る。
func TestConfigure_MmlBakeRunsBeforeDll(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeFile(t, filepath.Join(projectRoot, "assets", "audio", "bgm", "field.mml"), "@SSG V10 T120 O4 C D E\n")
	writeFile(t, filepath.Join(engineRoot, "apps", "mitiru_mmlbake", "main.cpp"), "// mmlbake\n")
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) { o.BakeMml = true })
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	audio := toCMakePath(filepath.Join(projectRoot, "assets", "audio"))
	for _, want := range []string{
		`add_executable(mitiru_mmlbake "` + toCMakePath(filepath.Join(engineRoot, "apps", "mitiru_mmlbake", "main.cpp")) + `")`,
		"target_link_libraries(mitiru_mmlbake PRIVATE Mitiru::mitiru)",
		"add_custom_target(my_first_game_mmlbake ALL",
		`COMMAND mitiru_mmlbake "` + audio + `"`,
		"add_dependencies(my_first_game_mmlbake mitiru_mmlbake)",
		"add_dependencies(my_first_game my_first_game_mmlbake)",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
	// 焼いた WAV を assets/ の写しに入れるため、写し (DLL の POST_BUILD) より前に走る依存でなければならない
	if strings.Index(cmake, "add_dependencies(my_first_game my_first_game_mmlbake)") < 0 {
		t.Error("the game DLL must depend on the bake step")
	}
}

func TestConfigure_MmlBakeSkipped(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, projectRoot, engineRoot string)
		bake  bool
	}{
		{"mml が無い", func(t *testing.T, p, e string) {
			writeFile(t, filepath.Join(e, "apps", "mitiru_mmlbake", "main.cpp"), "//\n")
			writeFile(t, filepath.Join(p, "assets", "audio", "coin.wav"), "RIFF")
		}, true},
		{"engine がツールを持たない", func(t *testing.T, p, e string) {
			writeFile(t, filepath.Join(p, "assets", "audio", "a.mml"), "@SSG C\n")
		}, true},
		{"[build] bake_mml = false", func(t *testing.T, p, e string) {
			writeFile(t, filepath.Join(e, "apps", "mitiru_mmlbake", "main.cpp"), "//\n")
			writeFile(t, filepath.Join(p, "assets", "audio", "a.mml"), "@SSG C\n")
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projectRoot, engineRoot := fakeProject(t)
			c.setup(t, projectRoot, engineRoot)
			cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) { o.BakeMml = c.bake })
			if err != nil {
				t.Fatalf("Configure: %v", err)
			}
			if strings.Contains(cmake, "mitiru_mmlbake") {
				t.Error("焼く工程を足してはいけない")
			}
		})
	}
}

func TestHasMmlFile(t *testing.T) {
	dir := t.TempDir()
	if hasMmlFile(filepath.Join(dir, "none")) {
		t.Error("フォルダが無ければ false のはず")
	}
	writeFile(t, filepath.Join(dir, "a.WAV"), "RIFF")
	if hasMmlFile(dir) {
		t.Error(".mml が無ければ false のはず")
	}
	writeFile(t, filepath.Join(dir, "deep", "x", "boss.MML"), "@SSG C\n")
	if !hasMmlFile(dir) {
		t.Error("入れ子の .mml (大文字の拡張子) を見つけるはず")
	}
}

// [build] sources と .mml の焼き込みは、同じ生成 CMake に両方入る (どちらかが落ちない)。
func TestConfigure_MmlBakeAndGameSources(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	writeFile(t, filepath.Join(projectRoot, "assets", "audio", "a.mml"), "@SSG C\n")
	writeFile(t, filepath.Join(projectRoot, "src", "game.cpp"), "// game\n")
	writeFile(t, filepath.Join(engineRoot, "apps", "mitiru_mmlbake", "main.cpp"), "//\n")
	cmake, err := generatedCMakeWith(t, projectRoot, engineRoot, func(o *Options) {
		o.BakeMml = true
		o.GameSources = []string{"src/*.cpp"}
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	for _, want := range []string{
		`"` + toCMakePath(filepath.Join(projectRoot, "src", "game.cpp")) + `"`,
		"target_sources(my_first_game PRIVATE",
		"add_dependencies(my_first_game my_first_game_mmlbake)",
	} {
		if !strings.Contains(cmake, want) {
			t.Errorf("generated CMake is missing:\n%s", want)
		}
	}
}
