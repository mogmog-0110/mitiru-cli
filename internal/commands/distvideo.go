package commands

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// distVideoExt は engine の動画 (feature "video") が読む形式。
const distVideoExt = ".mpg"

// distVideoFiles は root/assets/video の下にある動画 (.mpg) を、assets/video からの相対パス (スラッシュ区切り) で返す。
// フォルダが無ければ空。
func distVideoFiles(root string) []string {
	base := filepath.Join(root, "assets", "video")
	var out []string
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), distVideoExt) {
			return nil
		}
		if rel, rerr := filepath.Rel(base, path); rerr == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// checkDistVideoFeature は assets/video に動画があるのに [engine] features に "video" が無いゲームを、組む前に止める。
// features に無い host は動画を映せず、画面が黙って欠けるまま配ることになる。
func checkDistVideoFeature(projectRoot string, hasVideoFeature bool) error {
	videos := distVideoFiles(projectRoot)
	if len(videos) == 0 || hasVideoFeature {
		return nil
	}
	return fmt.Errorf("dist: assets/video に動画 (%s) があるのに、mitiru.toml の [engine] features に \"video\" がありません。"+
		"動画は features に video を書いたゲームだけが映せます。features = [\"video\"] を足してください", distVideoList(videos))
}

// distVideoMissing は want のうち have に無いものを返す。配布物へ動画が写っていないことを、pack に畳む前に見つける。
func distVideoMissing(want, have []string) []string {
	got := make(map[string]bool, len(have))
	for _, h := range have {
		got[strings.ToLower(h)] = true
	}
	var missing []string
	for _, w := range want {
		if !got[strings.ToLower(w)] {
			missing = append(missing, w)
		}
	}
	return missing
}

// distVideoList は動画の名前を 3 本まで並べ、残りは本数にする。
func distVideoList(videos []string) string {
	if len(videos) <= 3 {
		return strings.Join(videos, ", ")
	}
	return fmt.Sprintf("%s ほか %d 本", strings.Join(videos[:3], ", "), len(videos)-3)
}

// checkBundleVideos は project の動画が、配布物のゲームの assets へ写っているかを調べる (pack に畳む前)。
func checkBundleVideos(projectRoot, bundleGameDir string) error {
	want := distVideoFiles(projectRoot)
	if len(want) == 0 {
		return nil
	}
	if missing := distVideoMissing(want, distVideoFiles(bundleGameDir)); len(missing) > 0 {
		return fmt.Errorf("dist --check: 配布物に動画が入っていません (%s)。assets/video の動画は、ビルドが DLL の隣の assets へ写したものだけが入ります",
			distVideoList(missing))
	}
	return nil
}
