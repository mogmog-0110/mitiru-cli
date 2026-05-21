//go:build windows

package install

import (
	"fmt"
	"os"
	"os/exec"
)

// installBuildTools invokes winget to install Microsoft.VisualStudio.2022.BuildTools
// with the C++ workload (which brings CMake + Windows SDK + ATL etc. via
// --includeRecommended). UAC will prompt — that's the only interactive moment.
//
// winget is idempotent: if Build Tools are already installed it returns
// quickly with a "found existing" message. We treat exit code 0 as success.
func installBuildTools(opts Options) error {
	args := []string{
		"install",
		"Microsoft.VisualStudio.2022.BuildTools",
		"--silent",
		"--accept-package-agreements",
		"--accept-source-agreements",
		"--override",
		"--quiet --wait --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended",
	}

	fmt.Fprintf(opts.Stdout, "  実行: winget %s\n", quoteArgs(args))
	fmt.Fprintln(opts.Stdout, "  [UAC が出ます。VS Installer の進捗バーが別 window で表示されます — 待ち時間: 5-15 min]")

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	cmd := exec.Command("winget", args...)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("winget exit: %w", err)
	}
	fmt.Fprintln(opts.Stdout, "  ... done")
	return nil
}

// quoteArgs builds a shell-friendly representation of argv for log output.
// Not used to execute anything — just for the "what we ran" trace.
func quoteArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		needQuote := false
		for _, r := range a {
			if r == ' ' || r == '"' {
				needQuote = true
				break
			}
		}
		if needQuote {
			out += `"` + a + `"`
		} else {
			out += a
		}
	}
	return out
}
