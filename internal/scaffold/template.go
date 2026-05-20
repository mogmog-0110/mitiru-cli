// Package scaffold expands the embedded project templates onto disk.
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

// Data is the template payload exposed to *.tmpl files.
type Data struct {
	// ProjectName is the human-facing name from `mitiru new <name>`, used
	// verbatim in window titles and on-screen labels.
	ProjectName string

	// ProjectIdent is the C++-safe identifier form of ProjectName — lower
	// snake_case, suitable for `namespace <ident> { ... }`.
	ProjectIdent string

	// UpperIdent is the UPPER_SNAKE_CASE form of ProjectName, used as the
	// prefix for `#define <UPPER>_DLL_EXPORT` and similar macro guards.
	UpperIdent string
}

// Expand walks templates/<template>/ and writes every file (rendering *.tmpl
// through text/template, copying everything else byte-for-byte) into dstDir.
//
// Files named gitignore.tmpl land as `.gitignore` on disk (embed cannot ship
// a literal `.gitignore` because go:embed ignores dotfiles).
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
