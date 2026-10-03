//go:build windows

package install

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// enableLongPaths は HKLM\SYSTEM\CurrentControlSet\Control\FileSystem\
// LongPathsEnabled = 1 を設定し、build 中に MAX_PATH (260) を超える
// engine のパスで破綻しないようにする。
//
// admin が必要。書き込みを試み、error は caller に伝える
// (orchestrator が warning に格下げする)。
func enableLongPaths(opts Options) error {
	const keyPath = `SYSTEM\CurrentControlSet\Control\FileSystem`
	const valueName = "LongPathsEnabled"

	fmt.Fprintf(detailWriter(opts), "  registry: HKLM\\%s\\%s = 1\n", keyPath, valueName)

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("HKLM\\%s を開けません。管理者の権限が要ります (%w)", keyPath, err)
	}
	defer k.Close()

	if cur, _, err := k.GetIntegerValue(valueName); err == nil && cur == 1 {
		fmt.Fprintln(detailWriter(opts), "  already 1")
		return nil
	}

	if err := k.SetDWordValue(valueName, 1); err != nil {
		return fmt.Errorf("%s を書き込めません。管理者の権限が要ります (%w)", valueName, err)
	}

	fmt.Fprintln(detailWriter(opts), "  done")
	return nil
}
