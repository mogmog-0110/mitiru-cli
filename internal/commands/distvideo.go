package commands

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// distVideoFeatureByExt は engine の VideoHost が探す拡張子 (.webm .mpg .mp4) と、その動画を映すのに要る feature。
// AV1 の WebM と MPEG-1 は "video"、H.264 の MP4 は "video_mf" (ADR 0114)。
var distVideoFeatureByExt = map[string]string{
	".webm": "video",
	".mpg":  "video",
	".mp4":  "video_mf",
}

// distVideoFiles は root/assets/video の下にある動画 (.webm .mpg .mp4) を、assets/video からの相対パス (スラッシュ区切り) で返す。
// フォルダが無ければ空。
func distVideoFiles(root string) []string {
	base := filepath.Join(root, "assets", "video")
	var out []string
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if _, ok := distVideoFeatureByExt[strings.ToLower(filepath.Ext(path))]; !ok {
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

// checkDistVideoFeature は assets/video に動画があるのに、それを映す feature が [engine] features に無いゲームを、組む前に止める。
// features に無い host は動画を映せず、画面が黙って欠けるまま配ることになる。
func checkDistVideoFeature(projectRoot string, hasFeature func(name string) bool) error {
	byFeature := map[string][]string{}
	for _, v := range distVideoFiles(projectRoot) {
		feature := distVideoFeatureByExt[strings.ToLower(filepath.Ext(v))]
		if !hasFeature(feature) {
			byFeature[feature] = append(byFeature[feature], v)
		}
	}
	for _, feature := range []string{"video", "video_mf"} {
		if videos := byFeature[feature]; len(videos) > 0 {
			return fmt.Errorf("dist: assets/video に動画 (%s) があるのに、mitiru.toml の [engine] features に \"%s\" がありません。"+
				"この動画は features に %s を書いたゲームだけが映せます。features = [\"%s\"] を足してください",
				distVideoList(videos), feature, feature, feature)
		}
	}
	return nil
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
