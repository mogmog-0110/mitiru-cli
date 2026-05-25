//go:build windows

package install

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// enableLongPaths は HKLM\SYSTEM\CurrentControlSet\Control\FileSystem\
// LongPathsEnabled = 1 を設定し、build 中に MAX_PATH (260) を超える
// CEF / engine のパスで破綻しないようにする。
//
// admin が必要。書き込みを試み、error は caller に伝える
// (orchestrator が warning に格下げする)。
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
