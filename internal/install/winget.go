//go:build windows

package install

import (
	"fmt"
	"os"
	"os/exec"
)

// installBuildTools は winget を呼び、C++ workload 付きで
// Microsoft.VisualStudio.2022.BuildTools を install する (--includeRecommended
// により CMake + Windows SDK + ATL 等も入る)。UAC が出る — それが唯一の
// 対話的な瞬間。
//
// winget は冪等: Build Tools が既に install 済みなら "found existing"
// メッセージとともに素早く返る。exit code 0 を成功として扱う。
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

// quoteArgs はログ出力用に argv の shell フレンドリーな表現を組み立てる。
// 実行には使わない — 「何を実行したか」のトレース用。
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
