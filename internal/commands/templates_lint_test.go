package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/scaffold"
)

// `mitiru new` の全テンプレートが RmlUi の UI 文書を出し、HTML の HUD を出さず、
// main.rml が引く変数をすべて C++ が hud.set で送っていること (mitiru lint が 0 件)。
func TestTemplatesScaffoldLintCleanRML(t *testing.T) {
	for _, name := range []string{"welcome", "hello", "clicker", "shooter", "objects"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data := scaffold.Data{ProjectName: "lint-game", ProjectIdent: "lint_game",
				UpperIdent: "LINT_GAME", EngineVersion: defaultEngineVersion}
			if err := scaffold.Expand(name, dir, data); err != nil {
				t.Fatalf("expand: %v", err)
			}
			if fileExists(filepath.Join(dir, filepath.FromSlash(legacySceneRel))) {
				t.Errorf("template still scaffolds %s", legacySceneRel)
			}
			doc, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(uiDocRel)))
			if err != nil {
				t.Fatalf("template does not scaffold %s: %v", uiDocRel, err)
			}
			for _, f := range lintRML(string(doc), scanProducedKeys(filepath.Join(dir, "src"))) {
				t.Errorf("main.rml:%d %s: %s", f.line, f.kind, f.detail)
			}
		})
	}
}
