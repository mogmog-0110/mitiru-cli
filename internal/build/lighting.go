package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

// lightingBake はビルドの一工程で、[lighting] source の *.lighting.json を全部焼くための値。
type lightingBake struct {
	Items   []lightingBakeItem
	Args    string // mitiru_lightbake への追加 option (CMake の引用済み、先頭に空白)
	ToolAbs string // mitiru_lightbake のソース。無い engine では空文字
}

type lightingBakeItem struct {
	JSONAbs   string   // *.lighting.json
	DepsAbs   []string // json の "level" が指すレベル。焼き直しの判定に足す
	OutRel    string   // 焼いた .lighting.bin の、DLL の隣からの相対
	OutDirRel string   // OutRel の親 (make_directory 用)。DLL の隣そのものなら "."
	StampKey  string   // 構成ごとの印のファイル名に使う、OutRel から作った識別子
}

var stampKeyUnsafe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// resolveLightingBake は [lighting] source の glob を広げ、生成 CMake に渡す値にする。source が無ければ nil。
// 何にも当たらないパターンと読めない json はここで止める (ninja の「missing and no known rule」より先に分かるように)。
func resolveLightingBake(opts Options) (*lightingBake, error) {
	if len(opts.LightingSources) == 0 {
		return nil, nil
	}
	files, err := expandLightingSources(opts.ProjectRoot, opts.LightingSources)
	if err != nil {
		return nil, err
	}
	items := make([]lightingBakeItem, 0, len(files))
	for _, abs := range files {
		item, err := lightingItemFor(opts.ProjectRoot, abs)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	tool := ""
	if p := resolveEngineSource(opts.EngineRoot, "mitiru_lightbake", "main.cpp"); p != "" {
		tool = toCMakePath(p)
	}
	var args strings.Builder
	for _, a := range opts.LightingArgs {
		args.WriteString(" ")
		args.WriteString(cmakeQuote(a))
	}
	return &lightingBake{Items: items, Args: args.String(), ToolAbs: tool}, nil
}

// expandLightingSources はパターンを project 内の実ファイルに広げ、重複を除いて並べる (生成 CMake を安定させるため)。
func expandLightingSources(projectRoot string, patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, pat := range patterns {
		matches, err := filepath.Glob(filepath.Join(projectRoot, filepath.FromSlash(pat)))
		if err != nil {
			return nil, fmt.Errorf("[lighting] source %q: %w", pat, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("[lighting] source %q: no *.lighting.json found", pat)
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func lightingItemFor(projectRoot, jsonAbs string) (lightingBakeItem, error) {
	rel, err := filepath.Rel(projectRoot, jsonAbs)
	if err != nil {
		return lightingBakeItem{}, fmt.Errorf("[lighting] source: %w", err)
	}
	rel = filepath.ToSlash(rel)
	deps, err := lightingLevelDeps(jsonAbs, rel)
	if err != nil {
		return lightingBakeItem{}, err
	}
	outRel := config.LightingBinPathFor(rel)
	return lightingBakeItem{
		JSONAbs:   toCMakePath(jsonAbs),
		DepsAbs:   deps,
		OutRel:    outRel,
		OutDirRel: path.Dir(outRel),
		StampKey:  strings.Trim(stampKeyUnsafe.ReplaceAllString(outRel, "_"), "_"),
	}, nil
}

// lightingLevelDeps は json の "level" (json の隣からの相対) を返す。レベルを書き出し直しただけでも焼き直すため。
func lightingLevelDeps(jsonAbs, rel string) ([]string, error) {
	data, err := os.ReadFile(jsonAbs)
	if err != nil {
		return nil, fmt.Errorf("[lighting] %s: %w", rel, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("[lighting] %s: not valid JSON: %w", rel, err)
	}
	name, _ := doc["level"].(string) // engine も文字列でない level は読まない
	if name == "" {
		return nil, nil
	}
	level := filepath.Join(filepath.Dir(jsonAbs), filepath.FromSlash(name))
	if _, err := os.Stat(level); err != nil {
		return nil, fmt.Errorf("[lighting] %s: level %q not found: %w", rel, name, err)
	}
	return []string{toCMakePath(level)}, nil
}
