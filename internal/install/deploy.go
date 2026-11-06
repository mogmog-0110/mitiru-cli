//go:build windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// deployMitiru は installer の隣にある mitiru の実行ファイルを opts.TargetDir へ
// mitiru.exe として copy する。
//
// installer を `go run ./cmd/installer` から起動した場合、隣は一時ディレクトリで
// 何も無いので、cwd も見て開発時の `--dry-run` も動くようにする。
func deployMitiru(opts Options) error {
	src, srcErr := findMitiruSource()
	dst := filepath.Join(opts.TargetDir, "mitiru.exe")

	if opts.DryRun {
		if srcErr != nil {
			fmt.Fprintln(opts.Stdout, "  source: (見つかりません。installer と同じフォルダの mitiru.exe か mitiru_windows_amd64.exe を写します)")
		} else {
			fmt.Fprintf(opts.Stdout, "  source: %s\n", src)
		}
		fmt.Fprintf(opts.Stdout, "  dest:   %s\n", dst)
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	if srcErr != nil {
		return srcErr
	}
	fmt.Fprintf(detailWriter(opts), "  source: %s\n", src)
	fmt.Fprintf(detailWriter(opts), "  dest:   %s\n", dst)

	if err := os.MkdirAll(opts.TargetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", opts.TargetDir, err)
	}

	if err := copyFile(src, dst); err != nil {
		return err
	}
	fmt.Fprintln(detailWriter(opts), "  done")
	return nil
}

// mitiruSourceNames は探す名前を優先順に並べたもの。Release の添付は
// mitiru_windows_amd64.exe なので、落としたままの名前でも拾えるようにする。
var mitiruSourceNames = []string{"mitiru.exe", "mitiru_windows_amd64.exe"}

func findMitiruSource() (string, error) {
	var dirs []string
	// installer の隣を先に見て、次に cwd (`go run` での開発時実行) を見る。
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	return findMitiruSourceIn(dirs)
}

func findMitiruSourceIn(dirs []string) (string, error) {
	for _, dir := range dirs {
		for _, name := range mitiruSourceNames {
			cand := filepath.Join(dir, name)
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, nil
			}
		}
	}
	return "", errors.New("mitiru.exe が見つかりません (Release から落とした mitiru_windows_amd64.exe を installer と同じフォルダに置いてください)")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return nil
}
