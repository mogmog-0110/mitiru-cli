//go:build windows

// installer — まっさらな Windows machine をゼロから
// `mitiru new my_game && mitiru run` まで ~5 分で立ち上げる one-shot bootstrap。
//
// Spec: docs/INSTALLER.md (engine repo)。
// 配布: MitiruEngine release zip 内に mitiru.exe と並べて同梱される。
// ファイルを double-click して prompt に従う。
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
		// 失敗時: double-click 起動なら console を開いたまま保ち、
		// window が閉じる前に user が error を読めるようにする。
		if isConsoleStarted() {
			fmt.Fprintln(os.Stderr, "\nEnter キーで閉じます...")
			_, _ = fmt.Scanln()
		}
		os.Exit(1)
	}

	// 成功時: 短い countdown 後に auto-close する。user が "Next: mitiru new ..."
	// block を読む時間は確保しつつ、Enter を押す必要は無くす。
	// explorer の double-click 起動 (own-console) のときのみ。
	if isConsoleStarted() {
		autoCloseCountdown(5)
	}
}

// autoCloseCountdown は 0 までカウントダウンする 1 行を表示し、その後
// process が exit できるよう return する。\r で同じ行を上書きするので、
// console が "閉じます" の N 行で埋まらない。
func autoCloseCountdown(seconds int) {
	for i := seconds; i > 0; i-- {
		fmt.Fprintf(os.Stdout, "\rこのウィンドウは %d 秒後に閉じます ... ", i)
		time.Sleep(1 * time.Second)
	}
	fmt.Fprintln(os.Stdout)
}

// isConsoleStarted は、この process が console の唯一の占有者か
// — つまり既存 terminal からではなく double-click で起動されたか — を返す。
// exit 前に "Enter で閉じる" pause を入れるかの判断に使う。
//
// 信頼できる signal: GetConsoleProcessList。explorer の double-click 起動なら
// 新しい console はこの process だけを抱える (count == 1)。
// cmd / powershell から起動した場合は親 shell が console を共有する
// (count >= 2)。
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
