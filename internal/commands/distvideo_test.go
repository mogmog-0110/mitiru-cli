package commands

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeVideoFile(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDistVideoFilesListsVideosUnderAssetsVideo(t *testing.T) {
	root := t.TempDir()
	writeVideoFile(t, root, "assets/video/title.mpg")
	writeVideoFile(t, root, "assets/video/cut/intro.MPG")
	writeVideoFile(t, root, "assets/video/op.webm")
	writeVideoFile(t, root, "assets/video/cut/ed.MP4")
	writeVideoFile(t, root, "assets/video/clip.mov")
	writeVideoFile(t, root, "assets/video/readme.txt")
	writeVideoFile(t, root, "assets/other/skip.mpg")
	got := distVideoFiles(root)
	want := []string{"cut/ed.MP4", "cut/intro.MPG", "op.webm", "title.mpg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("distVideoFiles = %v, want %v", got, want)
	}
	if len(distVideoFiles(t.TempDir())) != 0 {
		t.Error("a project without assets/video has no videos")
	}
}

// features に video を書き忘れたゲームは、動画があるなら組む前に止まる
func TestCheckDistVideoFeatureStopsWhenTheFeatureIsMissing(t *testing.T) {
	root := t.TempDir()
	writeVideoFile(t, root, "assets/video/title.mpg")
	none := func(string) bool { return false }
	err := checkDistVideoFeature(root, none)
	if err == nil || !strings.Contains(err.Error(), `features に "video" がありません`) || !strings.Contains(err.Error(), "title.mpg") {
		t.Fatalf("want a message naming the feature and the file, got %v", err)
	}
	if err := checkDistVideoFeature(root, hasFeatures("video")); err != nil {
		t.Errorf("with the feature the dist goes on: %v", err)
	}
	if err := checkDistVideoFeature(t.TempDir(), none); err != nil {
		t.Errorf("a game without videos needs no feature: %v", err)
	}
}

func hasFeatures(names ...string) func(string) bool {
	return func(n string) bool { return slices.Contains(names, n) }
}

// .webm は video、.mp4 は video_mf が要る。片方の feature で、もう片方の動画は映らない
func TestCheckDistVideoFeatureMatchesTheExtension(t *testing.T) {
	webm := t.TempDir()
	writeVideoFile(t, webm, "assets/video/op.webm")
	if err := checkDistVideoFeature(webm, hasFeatures("video_mf")); err == nil || !strings.Contains(err.Error(), `"video"`) {
		t.Errorf("webm with only video_mf must stop on video, got %v", err)
	}
	if err := checkDistVideoFeature(webm, hasFeatures("video")); err != nil {
		t.Errorf("webm with video: %v", err)
	}

	mp4 := t.TempDir()
	writeVideoFile(t, mp4, "assets/video/ed.mp4")
	err := checkDistVideoFeature(mp4, hasFeatures("video"))
	if err == nil || !strings.Contains(err.Error(), `"video_mf"`) || !strings.Contains(err.Error(), "ed.mp4") {
		t.Errorf("mp4 with only video must stop on video_mf, got %v", err)
	}
	if err := checkDistVideoFeature(mp4, hasFeatures("video_mf")); err != nil {
		t.Errorf("mp4 with video_mf: %v", err)
	}

	both := t.TempDir()
	writeVideoFile(t, both, "assets/video/op.webm")
	writeVideoFile(t, both, "assets/video/ed.mp4")
	if err := checkDistVideoFeature(both, hasFeatures("video", "video_mf")); err != nil {
		t.Errorf("both features: %v", err)
	}
}

func TestDistVideoListShortensLongLists(t *testing.T) {
	got := distVideoList([]string{"a.mpg", "b.mpg", "c.mpg", "d.mpg", "e.mpg"})
	if got != "a.mpg, b.mpg, c.mpg ほか 2 本" {
		t.Errorf("distVideoList = %q", got)
	}
}

// 配布物へ写っていない動画は、--check が pack の前に見つける
func TestCheckBundleVideosFindsAMissingFile(t *testing.T) {
	project, bundle := t.TempDir(), t.TempDir()
	writeVideoFile(t, project, "assets/video/title.mpg")
	writeVideoFile(t, project, "assets/video/ending.mpg")
	writeVideoFile(t, project, "assets/video/intro.mp4")
	writeVideoFile(t, project, "assets/video/op.webm")
	writeVideoFile(t, bundle, "assets/video/title.mpg")
	writeVideoFile(t, bundle, "assets/video/intro.mp4")
	err := checkBundleVideos(project, bundle)
	if err == nil || !strings.Contains(err.Error(), "ending.mpg") || strings.Contains(err.Error(), "title.mpg") {
		t.Fatalf("want only ending.mpg reported, got %v", err)
	}
	writeVideoFile(t, bundle, "assets/video/ending.mpg")
	if err := checkBundleVideos(project, bundle); err == nil || !strings.Contains(err.Error(), "op.webm") {
		t.Errorf("a missing .webm must be reported, got %v", err)
	}
	writeVideoFile(t, bundle, "assets/video/op.webm")
	if err := checkBundleVideos(project, bundle); err != nil {
		t.Errorf("all videos present: %v", err)
	}
	if err := checkBundleVideos(t.TempDir(), bundle); err != nil {
		t.Errorf("a project without videos has nothing to check: %v", err)
	}
}

// engine が動画を描けないときの知らせ (features の書き忘れ、ファイルが読めない) を --check の欠けとして拾う
func TestFindDistProblemsCatchesVideoNotices(t *testing.T) {
	log := strings.Join([]string{
		`mitiru.toml の [engine] features に "video" がありません (動画を描けません)`,
		"動画 title_video/assets/video/title.mpg が読めません (assets/video に .webm か .mpg か .mp4 を置く)",
		"動画 title_video/assets/video/ed.mp4 を開けません (MP4 (H.264) を開くには mitiru.toml の features に \"video_mf\" が要ります)",
		"動画 title_video/assets/video/bad.mpg を開けません (MPEG-1 の映像が見つかりません)",
		"動画の復号が間に合わず、直前の絵を出し続けました (動画を小さくしてください)",
	}, "\n")
	got := findDistProblems(log)
	if len(got) != 4 {
		t.Fatalf("want the 4 notices that mean something is missing, got %v", got)
	}
}
