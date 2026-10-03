package commands

import (
	"fmt"
	"strings"
)

// toolExeTarget はツール窓のホスト (engine の apps/mitiru_tool)。全ツール窓はこの exe の --page で開く。
const toolExeTarget = "mitiru_tool"

// findToolExe は engine の build tree から mitiru_tool.exe を探す。無ければ空文字。
func findToolExe(engineRoot string) string {
	return firstExisting(engineExeCandidates(engineRoot, toolExeTarget, toolExeTarget+".exe"))
}

// inspectPageAliases は --inspect の窓名 → mitiru_tool --page 名の対応。
// ページ集合は engine の ToolRegistry.hpp kToolTable と同一 (+ 自然な別名)。
var inspectPageAliases = map[string]string{
	"inspect":    "inspect",
	"inspector":  "inspect",
	"gameplay":   "inspect",
	"input":      "input",
	"rewind":     "rewind",
	"timetravel": "rewind", // 旧名。engine 側は rewind へ改名済み
	"scene":      "scene",
	"replay":     "replay",
	"perf":       "perf",
	"mixer":      "mixer",
	"story":      "story",
	"side_state": "side_state",
	"side-state": "side_state",
	"ai":         "ai",
	"nav":        "nav",
	"anim":       "anim",
}

// inspectWindowNames はエラーメッセージ用の窓名一覧 (表示順固定)。
const inspectWindowNames = "perf, inspector, rewind, mixer, scene, replay, input, story, side_state, ai, nav, anim"

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
