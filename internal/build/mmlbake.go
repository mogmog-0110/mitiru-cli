package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// mmlBake はビルドの一工程で assets/audio の .mml を WAV に焼くための値。
type mmlBake struct {
	AudioDirAbs string // 焼く先の assets/audio (CMake の書き方)
	ToolAbs     string // mitiru_mmlbake のソース (CMake の書き方)
}

// resolveMmlBake は assets/audio の下に .mml があり、engine が mitiru_mmlbake を持つときだけ値を返す。
// .mml が無いゲームの生成物は今までと同じにするため、ツールごと足さない。
func resolveMmlBake(opts Options) *mmlBake {
	if !opts.BakeMml {
		return nil
	}
	audio := filepath.Join(opts.ProjectRoot, "assets", "audio")
	if !hasMmlFile(audio) {
		return nil
	}
	tool := resolveEngineSource(opts.EngineRoot, "mitiru_mmlbake", "main.cpp")
	if tool == "" {
		return nil
	}
	return &mmlBake{AudioDirAbs: toCMakePath(audio), ToolAbs: toCMakePath(tool)}
}

// hasMmlFile は dir の下 (入れ子も) に .mml が 1 つでもあるかを返す。dir が無ければ false。
// 読めない場所があるときは true にして、焼く工程に読めない理由を出させる (黙って焼かずに通さない)。
func hasMmlFile(dir string) bool {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return false
	}
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || strings.EqualFold(filepath.Ext(path), ".mml") && !d.IsDir() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}
