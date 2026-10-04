package build

import (
	"path/filepath"
	"testing"
)

// Debug は今までどおり build/out、ほかの構成は隣の dir。Release で組んでも Debug の host と DLL は残る。
func TestOutDir_SeparatesConfigurations(t *testing.T) {
	root := filepath.Join("C:", "proj")
	cases := map[string]string{
		"":               filepath.Join(root, "build", "out"),
		"Debug":          filepath.Join(root, "build", "out"),
		"debug":          filepath.Join(root, "build", "out"),
		"Release":        filepath.Join(root, "build", "out-release"),
		"RelWithDebInfo": filepath.Join(root, "build", "out-relwithdebinfo"),
	}
	for config, want := range cases {
		if got := OutDir(root, config); got != want {
			t.Errorf("OutDir(%q) = %s, want %s", config, got, want)
		}
	}
}

// configure は構成ごとの dir を cmake -B にし、生成する CMakeLists.txt は構成によらず 1 つにする。
func TestConfigure_ReleaseUsesItsOwnOutDir(t *testing.T) {
	projectRoot, engineRoot := fakeProject(t)
	opts := Options{ProjectRoot: projectRoot, ProjectName: "my-first-game", EngineRoot: engineRoot}
	opts.Config = "Debug"
	debugSrc, debugOut, err := Configure(opts)
	if err != nil {
		t.Fatalf("configure Debug: %v", err)
	}
	opts.Config = "Release"
	releaseSrc, releaseOut, err := Configure(opts)
	if err != nil {
		t.Fatalf("configure Release: %v", err)
	}
	if debugOut == releaseOut {
		t.Fatalf("Debug and Release share the build dir %s", debugOut)
	}
	if releaseOut != filepath.Join(projectRoot, "build", "out-release") {
		t.Errorf("Release build dir = %s", releaseOut)
	}
	if debugSrc != releaseSrc {
		t.Errorf("generated source dirs differ: %s / %s", debugSrc, releaseSrc)
	}
}
