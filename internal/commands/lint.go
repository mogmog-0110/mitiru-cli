package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/spf13/cobra"
)

// bind lint は、緩い C↔HTML 境界 (ADR 0005) が生む silent-failure クラスを捕捉する:
// scene.html の data-m-* binding が、C++ が一度も push しない state key を参照する
// (typo は fallback を表示するだけでエラーにならない)。構造的に壊れた data-m-* markup
// も flag する。best-effort な静的 check であり、デフォルトは warning、`--strict` で失敗。

// dottedPath は "view.hud.hp" のような state-key path (ドット区切り 2 segment 以上) に
// match する。data-m-repeat 内の item-scope な裸 field (例 "name") はドットを持たず、
// 意図的に match しない。
var dottedPath = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+`)

// dataMAttr は行中の data-m-<verb>="<value>" 属性に match する。
var dataMAttr = regexp.MustCompile(`data-m-([a-z]+)\s*=\s*"([^"]*)"`)

// quotedDotted は C++ の文字列リテラル内に現れる dotted path に match する。
// 例 w.set("view.hp", ...) や pushStr(it, "view.hp", ...)。
var quotedDotted = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+)"`)

type bindFinding struct {
	line   int    // 0 = file-level (特定行なし)
	kind   string // 短い category
	detail string
}

func newLintCommand() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Check scene.html data-m-* bindings against the C++ state keys",
		Long: `Statically cross-checks the project's HTML bindings and its C++ pushes.

Catches the silent failures the C↔HTML boundary allows:
  - a data-m-* key bound in scene.html that the C++ never pushes (typo)
  - structurally broken markup (unbalanced data-m-tpl braces, empty
    data-m-action, data-m-repeat without a <template>, missing binder script)

Warnings only by default. Use --strict to exit non-zero when findings exist
(for CI).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero if any findings")
	return cmd
}

func runLint(strict bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	manifestPath, projectRoot, err := config.FindManifest(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(manifestPath)
	if err != nil {
		return err
	}

	scenePath := filepath.Join(projectRoot, filepath.FromSlash(sceneRelPath(cfg)))
	html, err := os.ReadFile(scenePath)
	if err != nil {
		fmt.Printf("\n  --- bind lint ---\n  no scene found at %s (nothing to check)\n", scenePath)
		return nil
	}

	consumed, structural := analyzeScene(string(html))
	produced := scanProducedKeys(filepath.Join(projectRoot, "src"))

	var missing []bindFinding
	for key, line := range consumed {
		if !producedCovers(produced, key) {
			missing = append(missing, bindFinding{
				line: line, kind: "unpushed",
				detail: fmt.Sprintf("%q is bound in scene.html but never pushed from C++ (typo?)", key),
			})
		}
	}

	total := printBindReport(filepath.Base(scenePath), structural, missing)
	if strict && total > 0 {
		return fmt.Errorf("bind lint: %d finding(s)", total)
	}
	return nil
}

// sceneRelPath は manifest から project 相対の scene path を解決する。
// デフォルトは assets/scene.html。
func sceneRelPath(cfg *config.ProjectConfig) string {
	if cfg.CEF.StartURL != "" {
		return cfg.CEF.StartURL
	}
	return "assets/scene.html"
}

// analyzeScene は data-m-* binding が consume する dotted state key の集合
// (key -> 最初に見た行) と、構造的 finding を抽出する。
func analyzeScene(html string) (map[string]int, []bindFinding) {
	consumed := map[string]int{}
	var structural []bindFinding

	sawBinding := false
	sawRepeat := false
	hasTemplate := strings.Contains(html, "<template")
	hasBinder := false

	lines := strings.Split(html, "\n")
	for i, raw := range lines {
		lineNum := i + 1
		if strings.Contains(raw, "mitiru_bind.js") {
			hasBinder = true
		}
		for _, m := range dataMAttr.FindAllStringSubmatch(raw, -1) {
			verb, value := m[1], m[2]
			sawBinding = true
			if verb == "repeat" {
				sawRepeat = true
			}
			structural = append(structural, structuralChecks(verb, value, lineNum)...)
			for _, p := range dottedPath.FindAllString(value, -1) {
				if _, seen := consumed[p]; !seen {
					consumed[p] = lineNum
				}
			}
		}
	}

	if sawBinding && !hasBinder {
		structural = append(structural, bindFinding{
			kind: "no-binder",
			detail: "scene uses data-m-* but does not load mitiru_runtime/mitiru_bind.js",
		})
	}
	if sawRepeat && !hasTemplate {
		structural = append(structural, bindFinding{
			kind: "repeat-no-template",
			detail: "data-m-repeat present but no <template> child to clone per item",
		})
	}
	return consumed, structural
}

// structuralChecks は単一の data-m-<verb>="value" 属性を検証する。
func structuralChecks(verb, value string, line int) []bindFinding {
	var out []bindFinding
	switch verb {
	case "tpl":
		if strings.Count(value, "{") != strings.Count(value, "}") {
			out = append(out, bindFinding{line: line, kind: "tpl-braces",
				detail: fmt.Sprintf("data-m-tpl has unbalanced { } braces: %q", value)})
		}
	case "action":
		if strings.TrimSpace(value) == "" {
			out = append(out, bindFinding{line: line, kind: "empty-action",
				detail: "data-m-action has no action name"})
		}
	case "arg":
		if strings.TrimSpace(value) == "" {
			out = append(out, bindFinding{line: line, kind: "empty-arg",
				detail: "data-m-arg has no value"})
		}
	}
	return out
}

// scanProducedKeys は srcDir 配下の C++ 文字列リテラル内に現れる全 dotted path を
// 収集する。key は常に quoted literal なので、StateWriter set/array/object/list の
// key も自前 push helper も捕捉できる。
// ここでの過剰捕捉は安全: produced key が多いほど false な "unpushed" flag が減る。
func scanProducedKeys(srcDir string) map[string]bool {
	produced := map[string]bool{}
	_ = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".cpp" && ext != ".hpp" && ext != ".cc" && ext != ".h" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range quotedDotted.FindAllStringSubmatch(string(data), -1) {
			produced[m[1]] = true
		}
		return nil
	})
	return produced
}

// producedCovers は、いずれかの produced key が consumed key を segment-prefix
// (どちらの向きでも) または完全一致で cover するかを返す。push 済み object
// "view.shop" は "view.shop.b0.cost" (JSON object の sub-field) を cover する一方、
// "view.eintent" に対する誤記 "view.eintnet" は依然として flag される。
func producedCovers(produced map[string]bool, key string) bool {
	ks := strings.Split(key, ".")
	for p := range produced {
		if segmentPrefix(strings.Split(p, "."), ks) {
			return true
		}
	}
	return false
}

// segmentPrefix は a/b の短い方が長い方の先頭 segment prefix かを返す
// (長さが等しい場合も prefix とみなす)。
func segmentPrefix(a, b []string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func printBindReport(scene string, structural, missing []bindFinding) int {
	fmt.Println()
	fmt.Println("  --- bind lint ---")

	all := append([]bindFinding{}, structural...)
	all = append(all, missing...)
	if len(all) == 0 {
		fmt.Printf("  ok: %s bindings all resolve to pushed C++ keys.\n", scene)
		return 0
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].line < all[j].line })
	for _, f := range all {
		if f.line > 0 {
			fmt.Printf("  %s:%d  %s\n", scene, f.line, f.detail)
		} else {
			fmt.Printf("  %s  %s\n", scene, f.detail)
		}
	}
	fmt.Println()
	fmt.Printf("  %d finding(s). A bound key with no C++ push renders the HTML fallback silently.\n", len(all))
	return len(all)
}
