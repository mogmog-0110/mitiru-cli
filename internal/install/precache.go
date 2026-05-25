//go:build windows

package install

import (
	"fmt"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
)

// precacheEngine は engine source tarball を事前に download し、ユーザの
// 初回 `mitiru build` が ~30s の download フェーズを skip できるようにする。
// 非致命的: ここで失敗しても、ユーザが初回 build で 30s 払うだけ。
func precacheEngine(opts Options) error {
	fmt.Fprintln(opts.Stdout, "  MitiruEngine 'latest' を ~/.mitiru/cache/ に展開...")

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	// EnsureSource は冪等 — cache が既にこの version を保持していれば
	// 即座に返る。よって pre-cache は再実行しても安全。
	root, err := engine.EnsureSource("latest", opts.Stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "  ... done (%s)\n", root)
	return nil
}
