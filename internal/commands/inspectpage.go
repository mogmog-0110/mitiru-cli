package commands

import (
	"fmt"
	"strings"
)

// inspectPageAliases は --inspect の窓名 → mitiru_tool_cef --page 名の対応。
// ページ集合は engine の ToolRegistry.hpp kToolTable と同一 (+ 自然な別名)。
var inspectPageAliases = map[string]string{
	"inspect":    "inspect",
	"inspector":  "inspect",
	"gameplay":   "inspect",
	"input":      "input",
	"timetravel": "timetravel",
	"scene":      "scene",
	"replay":     "replay",
	"perf":       "perf",
	"mixer":      "mixer",
}

// inspectWindowNames はエラーメッセージ用の窓名一覧 (表示順固定)。
const inspectWindowNames = "perf, inspector, timetravel, mixer, scene, replay, input"

// resolveInspectPage は --inspect フラグ値と positional 引数から tool page 名を決める。
// 返り値 "" は「窓を開かない」。`--inspect` 単独は NoOptDefVal で flagVal="inspect"、
// `--inspect perf` (空白区切り) は cobra 上 flagVal="inspect" + args=["perf"] になる。
func resolveInspectPage(flagVal string, args []string) (string, error) {
	if flagVal == "" {
		if len(args) > 0 {
			return "", fmt.Errorf("unexpected argument %q (window names follow --inspect, e.g. --inspect perf)", args[0])
		}
		return "", nil
	}
	name := flagVal
	if len(args) > 0 {
		if flagVal != "inspect" {
			return "", fmt.Errorf("got both --inspect=%s and argument %q; pass one window name", flagVal, args[0])
		}
		if len(args) > 1 {
			return "", fmt.Errorf("--inspect takes one window name, got %d", len(args))
		}
		name = args[0]
	}
	page, ok := inspectPageAliases[strings.ToLower(name)]
	if !ok {
		return "", fmt.Errorf("unknown window %q (valid: %s)", name, inspectWindowNames)
	}
	return page, nil
}
