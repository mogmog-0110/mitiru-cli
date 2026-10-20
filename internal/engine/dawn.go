package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// dawnReleaseBase は Dawn の公式のビルド済みを置く場所。test が差し替える。
var dawnReleaseBase = "https://github.com/google/dawn/releases/download"

// DawnDirEnv は Dawn の展開先を手で指す環境変数。engine の cmake/MitiruDawn.cmake も同じ名前を読む。
const DawnDirEnv = "MITIRU_DAWN_DIR"

// ErrNoDawnPin は engine が Dawn の版を固定していない (src/dawn の無い古い engine)。
// 取得はせず、engine 自身の「target が無い」案内に任せる。
var ErrNoDawnPin = errors.New("engine has no cmake/dawn_version.cmake")

type dawnPin struct{ Tag, Commit, SHA256 string }

func (p dawnPin) archiveName() string {
	return "Dawn-" + p.Commit + "-windows-latest-Release.tar.gz"
}

func (p dawnPin) url() string { return dawnReleaseBase + "/" + p.Tag + "/" + p.archiveName() }

// 置き場の名前に使うコミットの長さ。深い置き場を避けるため、展開先の名前は短くする。
const dawnDirCommitLen = 12

var dawnPinRe = func(name string) *regexp.Regexp {
	return regexp.MustCompile(`set\(\s*MITIRU_DAWN_` + name + `\s+"([^"]+)"\s*\)`)
}

// readDawnPin は engine の cmake/dawn_version.cmake から、固定した版を読む。
// 版の正は engine 側に 1 つだけ置き、CLI は写さない。
func readDawnPin(engineRoot string) (dawnPin, error) {
	data, err := os.ReadFile(filepath.Join(engineRoot, "cmake", "dawn_version.cmake"))
	if err != nil {
		if os.IsNotExist(err) {
			return dawnPin{}, ErrNoDawnPin
		}
		return dawnPin{}, fmt.Errorf("read dawn_version.cmake: %w", err)
	}
	get := func(name string) string {
		if m := dawnPinRe(name).FindSubmatch(data); m != nil {
			return string(m[1])
		}
		return ""
	}
	pin := dawnPin{Tag: get("TAG"), Commit: get("COMMIT"), SHA256: strings.ToLower(get("ARCHIVE_SHA256"))}
	if pin.Tag == "" || len(pin.Commit) < dawnDirCommitLen || len(pin.SHA256) != 64 {
		return dawnPin{}, fmt.Errorf("%s/cmake/dawn_version.cmake から Dawn の版 (TAG・COMMIT・ARCHIVE_SHA256) を読めません",
			engineRoot)
	}
	return pin, nil
}

// dawnDirUsable は dir が engine の MitiruDawn.cmake が求める 2 つのファイルを持つか。
func dawnDirUsable(dir string) bool {
	for _, rel := range []string{"include/webgpu/webgpu_cpp.h", "lib/webgpu_dawn.lib"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return false
		}
	}
	return true
}

// EnsureDawn は engine が固定した版の Dawn のビルド済みの展開先を返す。無ければ取得して
// ~/.mitiru/cache/dawn-<コミット> に展開する (engine の取得と同じ cache)。探す順は、
// 環境変数 MITIRU_DAWN_DIR、cache、取得。progress には取得のときだけ書く。
func EnsureDawn(engineRoot string, progress io.Writer) (string, error) {
	if progress == nil {
		progress = io.Discard
	}
	pin, err := readDawnPin(engineRoot)
	if err != nil {
		return "", err
	}
	if env := strings.TrimSpace(os.Getenv(DawnDirEnv)); env != "" {
		if abs, aerr := filepath.Abs(env); aerr == nil && dawnDirUsable(abs) {
			return abs, nil
		}
		fmt.Fprintf(progress, "%s=%s に Dawn が見つからないので、cache の物を使います。\n", DawnDirEnv, env)
	}
	cacheRoot, err := CacheRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheRoot, "dawn-"+pin.Commit[:dawnDirCommitLen])
	if dawnDirUsable(dir) {
		return dir, nil
	}
	if err := fetchDawn(pin, dir, progress); err != nil {
		return "", fmt.Errorf("Dawn を用意できません。%w\n"+
			"  手で用意するなら %s を展開して、%s にその場所を入れてください。",
			err, pin.url(), DawnDirEnv)
	}
	return dir, nil
}

func fetchDawn(pin dawnPin, dir string, progress io.Writer) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	archive, err := os.CreateTemp(parent, "dawn-*.part")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	if err := downloadVerified(pin, archive, progress); err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	fmt.Fprintln(progress, "Dawn を展開しています。")
	// 別の mitiru が同時に取っていても互いの展開先を踏まないよう、展開は専用の一時の場所で行う
	staging, err := os.MkdirTemp(parent, "dawn-*.dir")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := extractTarGz(archive, staging, io.Discard); err != nil {
		return err
	}
	root, err := findDawnRoot(staging)
	if err != nil {
		return err
	}
	if dawnDirUsable(dir) {
		return nil // 同時に取っていた別の mitiru が先に置いた。使われているかもしれないので消さない
	}
	_ = os.RemoveAll(dir) // 壊れた前回の展開
	if err := os.Rename(root, dir); err != nil {
		if dawnDirUsable(dir) {
			return nil
		}
		return fmt.Errorf("move %s to %s: %w", root, dir, err)
	}
	return nil
}

func downloadVerified(pin dawnPin, dst io.Writer, progress io.Writer) error {
	url := pin.url()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mitiru-cli")
	resp, err := (&http.Client{Timeout: dawnHTTPTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s を取得できません (%s)。", url, resp.Status)
	}
	if resp.ContentLength > 0 {
		fmt.Fprintf(progress, "Dawn (%d MB) をダウンロードしています。この版では初回だけです。\n", resp.ContentLength>>20)
	} else {
		fmt.Fprintln(progress, "Dawn をダウンロードしています。この版では初回だけです。")
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != pin.SHA256 {
		return fmt.Errorf("ダウンロードした Dawn の SHA-256 が固定した値と違います (%s、固定は %s)。", got, pin.SHA256)
	}
	return nil
}

// findDawnRoot は展開した所のうち、include と lib を持つ階層を返す。公式の archive は
// Dawn-<コミット>-windows-latest-Release/ で包んである。
func findDawnRoot(staging string) (string, error) {
	if dawnDirUsable(staging) {
		return staging, nil
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() && dawnDirUsable(filepath.Join(staging, e.Name())) {
			return filepath.Join(staging, e.Name()), nil
		}
	}
	return "", fmt.Errorf("展開した Dawn に include/webgpu/webgpu_cpp.h と lib/webgpu_dawn.lib がありません。")
}
