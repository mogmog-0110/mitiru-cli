package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/scaffold"
)

// action3d は features と [nav] source が入った mitiru.toml を出し、ゲームは DLL の
// フォルダ名 (build.TargetName) から始まるパスでレベルと焼いたナビメッシュを開く。
func TestAction3DTemplate_ManifestAndPathsAgree(t *testing.T) {
	dir := t.TempDir()
	name := "my-Action"
	data := scaffold.Data{ProjectName: name, ProjectIdent: toLowerSnake(name), UpperIdent: toUpperSnake(name),
		TargetName: build.TargetName(name), EngineVersion: defaultEngineVersion}
	if err := scaffold.Expand("action3d", dir, data); err != nil {
		t.Fatalf("expand: %v", err)
	}

	cfg, err := config.Load(filepath.Join(dir, config.ManifestFilename))
	if err != nil {
		t.Fatalf("scaffolded mitiru.toml does not load: %v", err)
	}
	if !cfg.HasFeature("nav") {
		t.Errorf("features = %v, want nav", cfg.Engine.Features)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(cfg.Nav.Source))); err != nil {
		t.Errorf("nav.source %q is not scaffolded: %v", cfg.Nav.Source, err)
	}

	src, err := os.ReadFile(filepath.Join(dir, "src", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"my_Action/` + cfg.Nav.Source + `"`,
		`"my_Action/` + cfg.NavMeshPath() + `"`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.cpp does not open %s", want)
		}
	}

	// MSVC の中間物を無視する *.obj が、レベルのメッシュまで無視しないこと
	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "!/assets/**/*.obj") {
		t.Error(".gitignore hides assets/level.obj")
	}
}
