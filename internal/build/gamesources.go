package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// expandGameSources は [build] sources の glob を実ファイルに広げ、main.cpp を除いて名前順に並べる
// (生成 CMake を安定させるため)。何にも当たらないパターンはここで止める。
func expandGameSources(projectRoot, mainCpp string, patterns []string) ([]string, error) {
	seen := map[string]bool{filepath.Clean(mainCpp): true}
	var out []string
	for _, pat := range patterns {
		matches, err := filepath.Glob(filepath.Join(projectRoot, filepath.FromSlash(pat)))
		if err != nil {
			return nil, fmt.Errorf("[build] sources %q: %w", pat, err)
		}
		found := false
		for _, m := range matches {
			if info, statErr := os.Stat(m); statErr != nil || info.IsDir() {
				continue
			}
			found = true
			m = filepath.Clean(m)
			if !seen[m] {
				seen[m] = true
				out = append(out, toCMakePath(m))
			}
		}
		if !found {
			return nil, fmt.Errorf("[build] sources %q: no file found", pat)
		}
	}
	sort.Strings(out)
	return out, nil
}
