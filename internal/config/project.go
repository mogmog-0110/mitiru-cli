// Package config はプロジェクトごとの mitiru.toml manifest を読み込み・検証する。
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ManifestFilename はプロジェクト manifest のディスク上の名前。常に
// プロジェクトルートの src/ の隣に置かれる。
const ManifestFilename = "mitiru.toml"

// ProjectConfig は scaffold テンプレートに記載された schema を反映する。
// Field tag は BurntSushi/toml の慣習 (小文字・アンダースコア) に従う。
type ProjectConfig struct {
	Project ProjectSection `toml:"project"`
	Window  WindowSection  `toml:"window"`
	CEF     CEFSection     `toml:"cef"`
	Build   BuildSection   `toml:"build"`
}

type ProjectSection struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Engine  string `toml:"engine"`
}

type WindowSection struct {
	Title  string `toml:"title"`
	Width  int    `toml:"width"`
	Height int    `toml:"height"`
	Vsync  bool   `toml:"vsync"`
}

type CEFSection struct {
	StartURL        string `toml:"start_url"`
	SkipDefaultFont bool   `toml:"skip_default_font"`
}

type BuildSection struct {
	Backend string `toml:"backend"`
}

// FindManifest は startDir から上方向に mitiru.toml を探す。manifest の
// 絶対パスとプロジェクトルート (その親ディレクトリ) を返す。filesystem の
// ルートに達するまで見つからなければ error を返す。
func FindManifest(startDir string) (manifestPath, projectRoot string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve cwd: %w", err)
	}
	for {
		candidate := filepath.Join(dir, ManifestFilename)
		if st, statErr := os.Stat(candidate); statErr == nil && !st.IsDir() {
			return candidate, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("%s not found; run 'mitiru new' first", ManifestFilename)
		}
		dir = parent
	}
}

// Load は path にある manifest を読み込み・検証する。
func Load(path string) (*ProjectConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	cfg := &ProjectConfig{}
	if _, err := toml.Decode(string(body), cfg); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", path, err)
	}

	if err := cfg.validate(path); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *ProjectConfig) validate(path string) error {
	if c.Project.Name == "" {
		return fmt.Errorf("%s: project.name is required", path)
	}
	if c.Project.Engine == "" {
		return fmt.Errorf("%s: project.engine is required", path)
	}
	if c.Window.Width < 0 || c.Window.Height < 0 {
		return fmt.Errorf("%s: window.width/height must not be negative", path)
	}
	return nil
}

func (c *ProjectConfig) applyDefaults() {
	if c.Window.Title == "" {
		c.Window.Title = c.Project.Name
	}
	if c.Window.Width == 0 {
		c.Window.Width = 1280
	}
	if c.Window.Height == 0 {
		c.Window.Height = 720
	}
	if c.CEF.StartURL == "" {
		c.CEF.StartURL = "assets/scene.html"
	}
	if c.Build.Backend == "" {
		c.Build.Backend = "auto"
	}
}

// EngineTag は engine version を `v` 接頭辞付きの git tag に正規化して返す。
// project.engine = "0.1.0" → "v0.1.0"; "v0.1.0" → そのまま; "latest" → "latest"。
func (c *ProjectConfig) EngineTag() string {
	v := c.Project.Engine
	if v == "" || v == "latest" {
		return v
	}
	if v[0] == 'v' || v[0] == 'V' {
		return v
	}
	return "v" + v
}
