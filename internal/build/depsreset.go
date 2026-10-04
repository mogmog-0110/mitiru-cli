package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// depsSchemeFile は、この out dir が今のコードページの決まり (vcvarsPrelude の chcp 65001) で
// configure 済みかを示す。cmake は /showIncludes の接頭辞をコンパイラの検出のときに
// 1 回だけ控え、configure し直しても検出をやり直さない。印が無い out dir は化けた
// 接頭辞を控えたままの可能性があるので、検出の結果と ninja の依存の記録を消して作り直す。
const depsSchemeFile = "mitiru_deps.stamp"

const depsScheme = "showincludes-utf8-v1"

func depsSchemeCurrent(outDir string) bool {
	data, err := os.ReadFile(filepath.Join(outDir, depsSchemeFile))
	return err == nil && strings.TrimSpace(string(data)) == depsScheme
}

func writeDepsScheme(outDir string) error {
	return os.WriteFile(filepath.Join(outDir, depsSchemeFile), []byte(depsScheme+"\n"), 0o644)
}

// resetCompilerDetection は CMakeFiles/<cmake の版> (コンパイラの検出の結果) と .ninja_deps を消す。
// .ninja_deps が無いと ninja は依存を記録する object を全部作り直すので、
// header の依存を持たずに作られた object も残らない。
func resetCompilerDetection(outDir string) error {
	entries, err := os.ReadDir(filepath.Join(outDir, "CMakeFiles"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read CMakeFiles: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && isCMakeVersionDir(e.Name()) {
			if err := os.RemoveAll(filepath.Join(outDir, "CMakeFiles", e.Name())); err != nil {
				return fmt.Errorf("remove compiler detection %s: %w", e.Name(), err)
			}
		}
	}
	if err := os.Remove(filepath.Join(outDir, ".ninja_deps")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove .ninja_deps: %w", err)
	}
	return nil
}

// isCMakeVersionDir は "3.31.6-msvc6" のような、版の数字で始まる名前かを見る。
func isCMakeVersionDir(name string) bool {
	return len(name) > 0 && name[0] >= '0' && name[0] <= '9' && strings.Contains(name, ".")
}
