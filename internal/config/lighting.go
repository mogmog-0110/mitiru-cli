package config

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// LightingSection はビルドの一工程で、レベルの間接光 (放射照度と反射のプローブ) を焼く設定。
type LightingSection struct {
	// Source は *.lighting.json のパスか glob (project root からの相対)。1 つなら文字列、複数なら配列で書く。
	// 焼いた .lighting.bin は DLL の隣の同じ相対位置に、.lighting.json を .lighting.bin に変えて置く。
	Source PathList `toml:"source"`
	// Args は mitiru_lightbake にそのまま渡す option (例 ["--rays", "512"])。
	Args []string `toml:"args"`
}

// PathList は TOML の文字列 1 つと文字列の配列のどちらでも読める。
type PathList []string

// UnmarshalTOML は BurntSushi/toml の Unmarshaler。
func (p *PathList) UnmarshalTOML(v any) error {
	switch t := v.(type) {
	case string:
		*p = PathList{t}
		return nil
	case []any:
		out := make(PathList, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("must be a string or a list of strings, got an element %v", e)
			}
			out = append(out, s)
		}
		*p = out
		return nil
	default:
		return fmt.Errorf("must be a string or a list of strings, got %T", v)
	}
}

const (
	lightingJSONSuffix = ".lighting.json"
	lightingBinSuffix  = ".lighting.bin"
)

func (c *ProjectConfig) validateLighting(manifest string) error {
	if c.Standalone() && len(c.Lighting.Source) > 0 {
		return fmt.Errorf("%s: [lighting] only applies to host projects; "+
			"a standalone project runs mitiru_lightbake from its own CMakeLists.txt", manifest)
	}
	if len(c.Lighting.Source) == 0 {
		if len(c.Lighting.Args) > 0 {
			return fmt.Errorf("%s: lighting.args needs lighting.source (the *.lighting.json to bake)", manifest)
		}
		return nil
	}
	for _, src := range c.Lighting.Source {
		if err := validateLightingSource(src); err != nil {
			return fmt.Errorf("%s: %w", manifest, err)
		}
	}
	return nil
}

func validateLightingSource(src string) error {
	slashed := toSlash(src)
	if strings.TrimSpace(slashed) == "" {
		return fmt.Errorf("lighting.source has an empty entry")
	}
	if filepath.IsAbs(src) || strings.HasPrefix(slashed, "/") || (len(slashed) > 1 && slashed[1] == ':') {
		return fmt.Errorf("lighting.source must be relative to the project root, got %q", src)
	}
	clean := path.Clean(slashed)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("lighting.source must stay inside the project, got %q", src)
	}
	if !strings.HasSuffix(strings.ToLower(clean), lightingJSONSuffix) {
		return fmt.Errorf("lighting.source must name *%s files, got %q", lightingJSONSuffix, src)
	}
	if _, err := path.Match(clean, ""); err != nil {
		return fmt.Errorf("lighting.source has a broken glob %q: %w", src, err)
	}
	return nil
}

// LightingBinPathFor は *.lighting.json のパスを、焼いた *.lighting.bin のパス (スラッシュ区切り) に変える。
func LightingBinPathFor(source string) string {
	clean := path.Clean(toSlash(source))
	if !strings.HasSuffix(strings.ToLower(clean), lightingJSONSuffix) {
		return strings.TrimSuffix(clean, path.Ext(clean)) + lightingBinSuffix
	}
	return clean[:len(clean)-len(lightingJSONSuffix)] + lightingBinSuffix
}
