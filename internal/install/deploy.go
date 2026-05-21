//go:build windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// deployMitiru copies the `mitiru.exe` that lives next to the installer (in
// the same release zip) into opts.TargetDir.
//
// Locating the source: installer.exe and mitiru.exe ship side-by-side in the
// release zip, so os.Executable() + "/../mitiru.exe" is the canonical path.
// If the installer is invoked from `go run ./cmd/installer`, that path won't
// resolve to a real .exe — we fall back to looking in the cwd in that case
// so dev-time `--dry-run` still works.
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
	// Try next-to-installer first (release-zip layout).
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "mitiru.exe")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	// Fall back to cwd (dev-time runs via `go run`).
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
