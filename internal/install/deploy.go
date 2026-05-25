//go:build windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// deployMitiru は installer の隣 (同じ release zip 内) にある `mitiru.exe`
// を opts.TargetDir へ copy する。
//
// source の特定: installer.exe と mitiru.exe は release zip 内で隣り合って
// 配布されるので、os.Executable() + "/../mitiru.exe" が canonical なパス。
// installer を `go run ./cmd/installer` から起動した場合、このパスは実在の
// .exe に解決しない — その場合は cwd を見るフォールバックを行い、開発時の
// `--dry-run` も動くようにする。
func deployMitiru(opts Options) error {
	src, srcErr := findMitiruSource()
	dst := filepath.Join(opts.TargetDir, "mitiru.exe")

	if opts.DryRun {
		if srcErr != nil {
			fmt.Fprintln(opts.Stdout, "  source: <not located — release zip では同フォルダの mitiru.exe を copy>")
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
	fmt.Fprintf(opts.Stdout, "  source: %s\n", src)
	fmt.Fprintf(opts.Stdout, "  dest:   %s\n", dst)

	if err := os.MkdirAll(opts.TargetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", opts.TargetDir, err)
	}

	if err := copyFile(src, dst); err != nil {
		return err
	}
	fmt.Fprintln(opts.Stdout, "  ... done")
	return nil
}

func findMitiruSource() (string, error) {
	// まず installer の隣を試す (release-zip レイアウト)。
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "mitiru.exe")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	// cwd にフォールバック (`go run` での開発時実行)。
	cwd, err := os.Getwd()
	if err == nil {
		cand := filepath.Join(cwd, "mitiru.exe")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", errors.New("mitiru.exe が見つかりません (installer と同じフォルダに置いてください)")
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
