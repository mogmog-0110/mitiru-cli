package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 化けた接頭辞の出力 (cachecheck.go) は cache の中に控えられていて、hit のたびにそのまま返る。
// server を直しても残るので、一度だけ cache を空にする。印の無い cache dir だけが対象。
const cacheUTF8Stamp = "mitiru_utf8.stamp"

// parseCacheDir は `sccache -s` の出力から cache dir を取り出す。Rust の Debug 形式で \ が二重になっている。
func parseCacheDir(stats string) string {
	const marker = `Local disk: "`
	_, rest, ok := strings.Cut(stats, marker)
	if !ok {
		return ""
	}
	quoted, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return strings.ReplaceAll(quoted, `\\`, `\`)
}

// looksLikeSccacheDir は sccache の cache dir の形 (1 文字の 16 進の dir と印だけ) のときだけ true。
// 間違った dir を空にしないための確認。
func looksLikeSccacheDir(entries []os.DirEntry) bool {
	for _, e := range entries {
		name := e.Name()
		if name == cacheUTF8Stamp {
			continue
		}
		if !e.IsDir() || len(name) != 1 || !strings.Contains("0123456789abcdef", name) {
			return false
		}
	}
	return true
}

// clearStaleCache は印の無い cache dir を空にし、印を置く。cache dir が分からない、
// または sccache の形でないときは何もしない (依存の自己点検が最後の網になる)。
func clearStaleCache(exe string) error {
	out, err := exec.Command(exe, "--show-stats").Output()
	if err != nil {
		return nil
	}
	dir := parseCacheDir(string(out))
	if dir == "" {
		return nil
	}
	stamp := filepath.Join(dir, cacheUTF8Stamp)
	if _, err := os.Stat(stamp); err == nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil || !looksLikeSccacheDir(entries) {
		return nil
	}
	_ = exec.Command(exe, "--stop-server").Run() // 動いている server が消えた file を掴まないように
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("sccache の古い cache を消せませんでした: %w", err)
		}
	}
	return os.WriteFile(stamp, []byte("cl の出力を UTF-8 でそろえた cache\n"), 0o644)
}
