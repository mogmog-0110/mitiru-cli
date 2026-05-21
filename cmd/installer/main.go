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
	"time"
	"unsafe"

	"github.com/mogmog-0110/mitiru-cli/internal/install"
)

const usage = `MitiruEngine Installer

USAGE:
  MitiruEngine_Installer.exe [flags]

検出: install 済の component は default で skip されます。各 component を
独立に制御したい場合は対応する flag を指定。

FLAGS:
  --dry-run               プランを表示するだけ、何も書き換えない
  --target-dir <path>     mitiru.exe を置く先
                          (default: %LOCALAPPDATA%\Programs\MitiruEngine\bin)
  --force                 「既に install 済」 による skip を無視 (repair install)
  --skip-winget           MSVC install を skip (既に入ってる人向け、自動検出も同等)
  --skip-deploy           mitiru.exe を target dir に copy しない
  --skip-pathenv          HKCU\Environment\Path を触らない
  --skip-precache         engine source pre-cache を skip
  --skip-longpaths        LongPaths registry を skip
  --yes, -y               実行前 prompt を skip (CI 用)
  --help, -h              この help

詳しい仕様: https://github.com/mogmog-0110/MitiruEngine/blob/main/docs/INSTALLER.md
`

func main() {
	var opts install.Options
	flag.BoolVar(&opts.DryRun, "dry-run", false, "")
	flag.StringVar(&opts.TargetDir, "target-dir", "", "")
	flag.BoolVar(&opts.Force, "force", false, "")
	flag.BoolVar(&opts.SkipWinget, "skip-winget", false, "")
	flag.BoolVar(&opts.SkipDeploy, "skip-deploy", false, "")
	flag.BoolVar(&opts.SkipPathEnv, "skip-pathenv", false, "")
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
		// Failure path: keep the console open if launched via double-click
		// so the user can read the error before the window closes.
		if isConsoleStarted() {
			fmt.Fprintln(os.Stderr, "\nEnter キーで閉じます...")
			_, _ = fmt.Scanln()
		}
		os.Exit(1)
	}

	// Success path: auto-close after a short countdown so the user has time
	// to read the "Next: mitiru new ..." block, but doesn't need to press
	// Enter. Only when launched from explorer double-click (own-console).
	if isConsoleStarted() {
		autoCloseCountdown(5)
	}
}

// autoCloseCountdown prints a single-line countdown ticking down to 0, then
// returns so the process can exit. Uses \r to overwrite in place so the
// console doesn't fill with N lines of "閉じます" spam.
func autoCloseCountdown(seconds int) {
	for i := seconds; i > 0; i-- {
		fmt.Fprintf(os.Stdout, "\rこのウィンドウは %d 秒後に閉じます ... ", i)
		time.Sleep(1 * time.Second)
	}
	fmt.Fprintln(os.Stdout)
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
