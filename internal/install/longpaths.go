//go:build windows

package install

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// enableLongPaths sets HKLM\SYSTEM\CurrentControlSet\Control\FileSystem\
// LongPathsEnabled = 1 so CEF / engine paths that exceed MAX_PATH (260)
// during build don't blow up.
//
// Requires admin. We attempt the write and surface the error to the caller
// (the orchestrator downgrades it to a warning).
func enableLongPaths(opts Options) error {
	const keyPath = `SYSTEM\CurrentControlSet\Control\FileSystem`
	const valueName = "LongPathsEnabled"

	fmt.Fprintf(opts.Stdout, "  registry: HKLM\\%s\\%s = 1\n", keyPath, valueName)

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKLM\\%s (admin 要): %w", keyPath, err)
	}
	defer k.Close()

	if cur, _, err := k.GetIntegerValue(valueName); err == nil && cur == 1 {
		fmt.Fprintln(opts.Stdout, "  既に 1 — skip")
		return nil
	}

	if err := k.SetDWordValue(valueName, 1); err != nil {
		return fmt.Errorf("write %s (admin 要): %w", valueName, err)
	}

	fmt.Fprintln(opts.Stdout, "  ... done")
	return nil
}
