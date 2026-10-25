package commands

import (
	"os"
	"path/filepath"
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

func TestDistVideoFilesListsMpgUnderAssetsVideo(t *testing.T) {
	root := t.TempDir()
	writeVideoFile(t, root, "assets/video/title.mpg")
	writeVideoFile(t, root, "assets/video/cut/intro.MPG")
	writeVideoFile(t, root, "assets/video/readme.txt")
	writeVideoFile(t, root, "assets/other/skip.mpg")
	got := distVideoFiles(root)
	want := []string{"cut/intro.MPG", "title.mpg"}
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
	err := checkDistVideoFeature(root, false)
	if err == nil || !strings.Contains(err.Error(), `features に "video" がありません`) || !strings.Contains(err.Error(), "title.mpg") {
		t.Fatalf("want a message naming the feature and the file, got %v", err)
	}
	if err := checkDistVideoFeature(root, true); err != nil {
		t.Errorf("with the feature the dist goes on: %v", err)
	}
	if err := checkDistVideoFeature(t.TempDir(), false); err != nil {
		t.Errorf("a game without videos needs no feature: %v", err)
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
	writeVideoFile(t, bundle, "assets/video/title.mpg")
	err := checkBundleVideos(project, bundle)
	if err == nil || !strings.Contains(err.Error(), "ending.mpg") || strings.Contains(err.Error(), "title.mpg") {
		t.Fatalf("want only ending.mpg reported, got %v", err)
	}
	writeVideoFile(t, bundle, "assets/video/ending.mpg")
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
		"動画 title_video/assets/video/title.mpg が読めません (assets/video に .mpg を置く)",
		"動画 title_video/assets/video/bad.mpg を開けません (MPEG-1 の映像が見つかりません)",
		"動画の復号が間に合わず、直前の絵を出し続けました (動画を小さくしてください)",
	}, "\n")
	got := findDistProblems(log)
	if len(got) != 3 {
		t.Fatalf("want the 3 notices that mean something is missing, got %v", got)
	}
}
