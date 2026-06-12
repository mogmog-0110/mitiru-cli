// assets.go — build 成功後に <project>/assets/ を deploy 先へ常時同期する。
//
// CMake の asset copy は DLL link ターゲットの POST_BUILD に紐付いており、
// C++ 無変更だと "ninja: no work to do" で走らない (scene.html だけ直しても
// 反映されない)。Go 側で build 後段に毎回同期して papercut を消す。
// 新規/更新ファイルのみ copy し、削除追従はしない。
package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SyncAssets は srcDir (例 <project>/assets) を dstDir (例
// build/out/<game>/assets) へ同期し、copy したファイル数を返す。
// srcDir が無ければ何もしない (0, nil)。
func SyncAssets(srcDir, dstDir string) (int, error) {
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return 0, nil // assets/ を持たない project は正常
	}

	copied := 0
	err := filepath.Walk(srcDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !needsCopy(info, dst) {
			return nil
		}
		if err := copyFile(p, dst, info); err != nil {
			return fmt.Errorf("copy asset %s: %w", rel, err)
		}
		copied++
		return nil
	})
	if err != nil {
		return copied, fmt.Errorf("sync assets %s -> %s: %w", srcDir, dstDir, err)
	}
	return copied, nil
}

// needsCopy は dst が無い、サイズが違う、または src の方が新しいときに true。
// サイズ + mtime 比較で十分 (hash までは不要)。
func needsCopy(srcInfo os.FileInfo, dst string) bool {
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return true
	}
	if dstInfo.Size() != srcInfo.Size() {
		return true
	}
	return srcInfo.ModTime().After(dstInfo.ModTime())
}

// copyFile は src を dst へ copy し、mtime を src に合わせる
// (次回の needsCopy 判定を安定させるため)。
func copyFile(src, dst string, srcInfo os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
}
