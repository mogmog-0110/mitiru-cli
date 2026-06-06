package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/spf13/cobra"
)

// newMenuCommand は対話ランチャー (`mitiru menu`)。引数なしの `mitiru` でも起動する
// (root.RunE 経由)。毎回コマンド名を覚えて手打ちしなくて済むよう、文脈 (プロジェクト
// 内/外) に応じた選択肢だけを出す pulled UI。哲学: 必要なものしか出さない・CLI 一級。
func newMenuCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "menu",
		Aliases: []string{"m"},
		Short:   "Interactive launcher — pick a command instead of typing it",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMenu()
		},
	}
}

type menuEntry struct {
	label  string   // 表示ラベル
	args   []string // 実行する mitiru サブコマンド
	prompt string   // 非空なら、実行前にこの文言で 1 引数を尋ねる (例: new <name>)
}

func runMenu() error {
	reader := bufio.NewReader(os.Stdin)
	for {
		entries, header := menuEntriesForContext()
		fmt.Printf("\n  %s\n", header)
		for i, e := range entries {
			fmt.Printf("   %2d) %s\n", i+1, e.label)
		}
		fmt.Print("    q) quit\n\n  > ")

		line, err := reader.ReadString('\n')
		if err != nil { // EOF / 非 TTY
			fmt.Println()
			return nil
		}
		choice := strings.TrimSpace(line)
		if choice == "" || choice == "q" || choice == "quit" {
			return nil
		}
		idx, perr := strconv.Atoi(choice)
		if perr != nil || idx < 1 || idx > len(entries) {
			fmt.Println("  ? その番号は無い")
			continue
		}

		e := entries[idx-1]
		runArgs := append([]string{}, e.args...)
		if e.prompt != "" {
			fmt.Print("  " + e.prompt)
			argLine, aerr := reader.ReadString('\n')
			if aerr != nil {
				fmt.Println()
				return nil
			}
			arg := strings.TrimSpace(argLine)
			if arg == "" {
				fmt.Println("  キャンセル")
				continue
			}
			runArgs = append(runArgs, arg)
		}

		fmt.Printf("\n  $ mitiru %s\n", strings.Join(runArgs, " "))
		if rerr := runSelf(runArgs...); rerr != nil {
			fmt.Fprintf(os.Stderr, "  (mitiru %s 終了: %v)\n", strings.Join(runArgs, " "), rerr)
		}
		// コマンド終了後はメニューに戻る (run/watch を抜けたらまた選べる)。
	}
}

// menuEntriesForContext は cwd がプロジェクト内かどうかで選択肢を変える。
func menuEntriesForContext() (entries []menuEntry, header string) {
	if mp, _, err := config.FindManifest("."); err == nil {
		name := "(project)"
		if pc, lerr := config.Load(mp); lerr == nil && pc.Project.Name != "" {
			name = pc.Project.Name
		}
		header = fmt.Sprintf("mitiru — %s", name)
		entries = []menuEntry{
			{"run      build & run", []string{"run"}, ""},
			{"watch    hot-reload dev loop (rebuild on save)", []string{"watch"}, ""},
			{"debug    debug build & run", []string{"debug"}, ""},
			{"build    build only", []string{"build"}, ""},
			{"dist     package for release (配布フォルダ生成; --pack で秘匿/--exe で exe 化)", []string{"dist"}, ""},
			{"ui       preview HTML/CSS UI in browser", []string{"ui"}, ""},
			{"lint     check data-m-* bindings", []string{"lint"}, ""},
			{"clean    remove build/", []string{"clean"}, ""},
			{"doctor   check toolchain", []string{"doctor"}, ""},
			{"version  version info", []string{"version"}, ""},
		}
		return entries, header
	}
	header = "mitiru — (no project in this directory)"
	entries = []menuEntry{
		{"new      create a new project", []string{"new"}, "project name: "},
		{"ui       preview an HTML/CSS file", []string{"ui"}, ""},
		{"doctor   check toolchain", []string{"doctor"}, ""},
		{"version  version info", []string{"version"}, ""},
	}
	return entries, header
}

// runSelf は同じ mitiru バイナリを別サブコマンドで再実行する (stdio 継承)。
func runSelf(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	c := exec.Command(self, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
