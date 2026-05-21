//go:build windows

// installer — one-shot bootstrap to get a fresh Windows machine from zero
// to `mitiru new my_game && mitiru run` in ~5 minutes.
//
// Spec: docs/INSTALLER.md (engine repo).
// Distribution: ships inside MitiruEngine release zip, side-by-side with
// mitiru.exe. Double-click the file and follow the prompts.
package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/mogmog-0110/mitiru-cli/internal/install"
)

const usage = `MitiruEngine Installer

USAGE:
  MitiruEngine_Installer.exe [flags]

FLAGS:
  --dry-run               プランを表示するだけ、何も書き換えない
  --target-dir <path>     mitiru.exe を置く先
                          (default: %LOCALAPPDATA%\Programs\MitiruEngine\bin)
  --skip-winget           MSVC install を skip (既に入ってる人向け)
  --skip-precache         engine source pre-cache を skip
  --skip-longpaths        LongPaths registry を skip
  --yes, -y               全 prompt を yes (CI 用)
  --help, -h              この help

詳しい仕様: https://github.com/mogmog-0110/MitiruEngine/blob/main/docs/INSTALLER.md
`

func main() {
	var opts install.Options
	flag.BoolVar(&opts.DryRun, "dry-run", false, "")
	flag.StringVar(&opts.TargetDir, "target-dir", "", "")
	flag.BoolVar(&opts.SkipWinget, "skip-winget", false, "")
	flag.BoolVar(&opts.SkipPrecache, "skip-precache", false, "")
	flag.BoolVar(&opts.SkipLongPaths, "skip-longpaths", false, "")
	flag.BoolVar(&opts.AssumeYes, "yes", false, "")
	flag.BoolVar(&opts.AssumeYes, "y", false, "")

	showHelp := false
	flag.BoolVar(&showHelp, "help", false, "")
	flag.BoolVar(&showHelp, "h", false, "")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}
	flag.Parse()

	if showHelp {
		fmt.Fprint(os.Stdout, usage)
		return
	}

	if err := install.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", err)
		// Keep the console open if launched via double-click so the user can
		// read the error before the window closes.
		if isConsoleStarted() {
			fmt.Fprintln(os.Stderr, "\n何かキーを押すと閉じます...")
			_, _ = fmt.Scanln()
		}
		os.Exit(1)
	}

	if isConsoleStarted() {
		fmt.Fprintln(os.Stdout, "\nEnter キーで閉じます...")
		_, _ = fmt.Scanln()
	}
}

// isConsoleStarted reports whether this process is its console's sole
// occupant — i.e. it was launched by double-click rather than from an
// existing terminal. We use this to decide whether to "Press Enter to
// close" pause before exit.
//
// Reliable signal: GetConsoleProcessList. When invoked from explorer
// double-click, the new console hosts this process only (count == 1).
// When invoked from cmd / powershell, the parent shell shares the console
// (count >= 2).
func isConsoleStarted() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetConsoleProcessList")
	pids := make([]uint32, 16)
	ret, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)),
	)
	return ret == 1
}
