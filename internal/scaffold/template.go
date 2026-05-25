// Package scaffold は埋め込まれたプロジェクトテンプレートを disk へ展開する。
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:templates
var embedded embed.FS

// Data は *.tmpl ファイルに公開されるテンプレート payload。
type Data struct {
	// ProjectName は `mitiru new <name>` で渡される人間向けの名前。window
	// title や画面上のラベルにそのまま使われる。
	ProjectName string

	// ProjectIdent は ProjectName の C++ で安全な識別子形式 — lower
	// snake_case で、`namespace <ident> { ... }` に適する。
	ProjectIdent string

	// UpperIdent は ProjectName の UPPER_SNAKE_CASE 形式。`#define
	// <UPPER>_DLL_EXPORT` のような macro guard の接頭辞に使う。
	UpperIdent string

	// EngineVersion は scaffold したプロジェクトが mitiru.toml で pin する
	// engine release (例 "0.5.0")。defaultEngineVersion を source とし、新規
	// プロジェクトがテンプレートの要求より古い engine に当たらないようにする。
	EngineVersion string
}

// Expand は templates/<template>/ を walk し、すべてのファイルを dstDir へ
// 書き出す (*.tmpl は text/template でレンダリングし、それ以外は
// byte-for-byte で copy する)。
//
// gitignore.tmpl という名前のファイルは disk 上で `.gitignore` になる
// (go:embed は dotfile を無視するため、リテラルの `.gitignore` は配布できない)。
func Expand(templateName, dstDir string, data Data) error {
	rootInEmbed := "templates/" + templateName

	if _, err := fs.Stat(embedded, rootInEmbed); err != nil {
		return fmt.Errorf("scaffold: template %q not found: %w", templateName, err)
	}

	return fs.WalkDir(embedded, rootInEmbed, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == rootInEmbed {
			return nil
		}

		rel, err := filepath.Rel(rootInEmbed, srcPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dstPath := filepath.Join(dstDir, rewriteFilename(rel))

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		body, err := embedded.ReadFile(srcPath)
		if err != nil {
			return err
		}

		if strings.HasSuffix(srcPath, ".tmpl") {
			rendered, err := renderTemplate(srcPath, body, data)
			if err != nil {
				return err
			}
			body = rendered
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dstPath, body, 0o644)
	})
}

func rewriteFilename(rel string) string {
	rel = strings.TrimSuffix(rel, ".tmpl")
	parts := strings.Split(rel, "/")
	last := parts[len(parts)-1]
	if last == "gitignore" {
		parts[len(parts)-1] = ".gitignore"
	}
	return filepath.Join(parts...)
}

func renderTemplate(name string, body []byte, data Data) ([]byte, error) {
	t, err := template.New(name).
		Option("missingkey=error").
		Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("scaffold: parse %s: %w", name, err)
	}

	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return nil, fmt.Errorf("scaffold: execute %s: %w", name, err)
	}
	return []byte(sb.String()), nil
}
