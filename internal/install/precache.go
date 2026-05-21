//go:build windows

package install

import (
	"fmt"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
)

// precacheEngine downloads the engine source tarball ahead of time so the
// user's first `mitiru build` skips the ~30s download phase. This is
// non-fatal: failure here just means the user pays that 30s on first build.
func precacheEngine(opts Options) error {
	fmt.Fprintln(opts.Stdout, "  MitiruEngine 'latest' を ~/.mitiru/cache/ に展開...")

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	// EnsureSource is idempotent — if the cache already holds this version
	// it returns immediately. So pre-cache is safe to re-run.
	root, err := engine.EnsureSource("latest", opts.Stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "  ... done (%s)\n", root)
	return nil
}
