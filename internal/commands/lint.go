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

// bind lint は、C++ と UI の緩い境界 (ADR 0005) が生む「エラーにならない失敗」を拾う。
// RML の data model は未定義の変数を空として描くので、hud.set のキーの打ち間違いは
// 画面が空になるだけで何も言わない。main.rml が引く変数と、C++ が hud.set("view.x") で
// 送るキーを突き合わせる。静的な best-effort の検査で、既定は warning、--strict で失敗にする。

// uiModelName は engine が hud.set の値を写す data model の名前。キーは "view.<変数>"。
const uiModelName = "view"

// rmlExprAttr は値が data 式になる属性 (data-if、data-class-x、data-event-click など)。
var rmlExprAttr = regexp.MustCompile(`\bdata-(if|visible|for|value|checked|rml|(?:class|style|attr|event)-[A-Za-z0-9_-]+)\s*=\s*"([^"]*)"`)

// rmlInterp は {{ 式 }} を拾う。
var rmlInterp = regexp.MustCompile(`\{\{(.*?)\}\}`)

// rmlModelAttr は data-model="名前" を拾う。
var rmlModelAttr = regexp.MustCompile(`\bdata-model\s*=\s*"([^"]*)"`)

// legacyBinderAttr は CEF 世代の HTML binder の属性。RmlUi は読まない。
var legacyBinderAttr = regexp.MustCompile(`\bdata-m-[a-z]+\s*=`)

// rmlQuoted は式の中の文字列リテラル。中の語を変数と取り違えないよう先に消す。
var rmlQuoted = regexp.MustCompile(`'[^']*'|"[^"]*"`)

// rmlEntity は &lt; などの文字参照。lt を変数と取り違えないよう先に消す。
var rmlEntity = regexp.MustCompile(`&[A-Za-z]+;`)

// rmlIdent は式の中の識別子。直前の文字で「.の後ろ」(メンバー) を、直後で関数呼び出しを見分ける。
var rmlIdent = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// rmlForAttr は data-for の "v : arr" / "v, i : arr" を分ける。
var rmlForAttr = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:,\s*([A-Za-z_][A-Za-z0-9_]*))?\s*:\s*(.+)$`)

// rmlDispatchEmpty は名前の無い dispatch を拾う。
var rmlDispatchEmpty = regexp.MustCompile(`\bdispatch\(\s*(?:''|"")?\s*[,)]`)

// rmlBuiltins は式の中で C++ が送る変数ではない語。ev はイベント、it / it_index は data-for の既定名、
// ui_confirm_* / ui_prompt_* は engine が confirm() / prompt() のダイアログ用に持つ変数。
var rmlBuiltins = map[string]bool{
	"true": true, "false": true, "ev": true, "it": true, "it_index": true,
	"ui_confirm_open": true, "ui_confirm_title": true, "ui_confirm_text": true,
	"ui_prompt_open": true, "ui_prompt_title": true, "ui_prompt_text": true, "ui_prompt_value": true,
}

// rmlComment は <!-- --> の注釈。中の {{ }} や属性の例を検査しないよう、行数を保ったまま消す。
var rmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

func blankComments(doc string) string {
	return rmlComment.ReplaceAllStringFunc(doc, func(c string) string {
		return strings.Repeat("\n", strings.Count(c, "\n"))
	})
}

// quotedDotted は C++ の文字列リテラル内に現れる dotted path に match する。
// 例 hud.set("view.hp", ...) や pushStr(it, "view.hp", ...)。
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
		Short: "Check assets/ui/main.rml bindings against the keys the C++ pushes",
		Long: `Statically cross-checks the project's RML UI and its C++ pushes.

Catches the silent failures the C++/UI boundary allows:
  - a variable used in main.rml ({{ x }}, data-if, data-class-*, ...) that
    the C++ never pushes as hud.set("view.x", ...) (typo)
  - a hud.set key with a dot after "view." (RML cannot reach it)
  - structural slips: no data-model="view", unbalanced {{ }}, a dispatch
    without an action name, leftover data-m-* attributes from the HTML HUD

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
	if _, err := config.Load(manifestPath); err != nil {
		return err
	}

	docPath := filepath.Join(projectRoot, filepath.FromSlash(uiDocRel))
	doc, err := os.ReadFile(docPath)
	var findings []bindFinding
	report := filepath.Base(docPath)
	switch {
	case err == nil:
		findings = lintRML(string(doc), scanProducedKeys(filepath.Join(projectRoot, "src")))
	case fileExists(filepath.Join(projectRoot, filepath.FromSlash(legacySceneRel))):
		report = legacySceneRel
		findings = []bindFinding{{kind: "legacy-html",
			detail: fmt.Sprintf("今のエンジンは %s を読みません。%s に書き直してください。", legacySceneRel, uiDocRel)}}
	default:
		fmt.Printf("%s が無いので、調べるものはありません。\n", docPath)
		return nil
	}

	total := printBindReport(report, findings)
	if strict && total > 0 {
		return fmt.Errorf("main.rml に直すところが %d 件あります。", total)
	}
	return nil
}

// lintRML は main.rml の本文と C++ が送るキーの集合から finding を作る。
func lintRML(doc string, produced map[string]bool) []bindFinding {
	consumed, structural := analyzeRML(doc)
	vars, nested := producedViewVars(produced)

	findings := append([]bindFinding{}, structural...)
	for name, line := range consumed {
		if !vars[name] {
			findings = append(findings, bindFinding{
				line: line, kind: "unpushed",
				detail: fmt.Sprintf("main.rml は %q を使っていますが、C++ は %q を送っていません。綴りを確かめてください。",
					name, uiModelName+"."+name),
			})
		}
	}
	for _, key := range nested {
		findings = append(findings, bindFinding{
			kind: "nested-key",
			detail: fmt.Sprintf("C++ が送る %q は、%q の後ろに点があるので RML から読めません。点の無い名前で送ってください。",
				key, uiModelName+"."),
		})
	}
	return findings
}

// analyzeRML は RML が引く data model の変数 (名前 -> 最初に見た行) と、構造の finding を返す。
// data-for のループ変数は配列の要素なので、C++ のキーとは突き合わせない。
func analyzeRML(doc string) (map[string]int, []bindFinding) {
	consumed := map[string]int{}
	loopVars := map[string]bool{}
	var structural []bindFinding
	sawBinding, sawModel := false, false

	record := func(expr string, line int) {
		for _, name := range exprVars(expr) {
			if _, seen := consumed[name]; !seen {
				consumed[name] = line
			}
		}
	}

	for i, raw := range strings.Split(blankComments(doc), "\n") {
		line := i + 1
		structural = append(structural, lineChecks(raw, line, &sawModel)...)
		for _, m := range rmlInterp.FindAllStringSubmatch(raw, -1) {
			sawBinding = true
			record(stripFormatters(m[1]), line)
		}
		for _, m := range rmlExprAttr.FindAllStringSubmatch(raw, -1) {
			sawBinding = true
			record(forSource(m[1], m[2], loopVars), line)
		}
	}

	for name := range loopVars {
		delete(consumed, name)
	}
	if sawBinding && !sawModel {
		structural = append(structural, bindFinding{
			kind:   "no-model",
			detail: fmt.Sprintf("値を表示する書き方を使っていますが、data-model=%q を持つ要素がありません。", uiModelName),
		})
	}
	return consumed, structural
}

// forSource は data-for の "v : arr" からループ変数を loopVars に足し、配列の式だけを返す。
// data-for 以外の属性は式をそのまま返す。
func forSource(attr, expr string, loopVars map[string]bool) string {
	if attr != "for" {
		return expr
	}
	fm := rmlForAttr.FindStringSubmatch(expr)
	if fm == nil {
		return expr
	}
	loopVars[fm[1]] = true
	if fm[2] != "" {
		loopVars[fm[2]] = true
	}
	return fm[3]
}

// lineChecks は 1 行に閉じた構造の検査 (model 名、{{ }} の対、名前の無い dispatch、HTML binder の残り)。
func lineChecks(raw string, line int, sawModel *bool) []bindFinding {
	var out []bindFinding
	for _, m := range rmlModelAttr.FindAllStringSubmatch(raw, -1) {
		*sawModel = true
		if m[1] != uiModelName {
			out = append(out, bindFinding{line: line, kind: "model-name",
				detail: fmt.Sprintf("data-model=%q になっています。hud.set の値は %q に入るので、こちらの名前にしてください。", m[1], uiModelName)})
		}
	}
	if strings.Count(raw, "{{") != strings.Count(raw, "}}") {
		out = append(out, bindFinding{line: line, kind: "braces",
			detail: "この行の {{ と }} の数が合いません。"})
	}
	if rmlDispatchEmpty.MatchString(raw) {
		out = append(out, bindFinding{line: line, kind: "empty-action",
			detail: "dispatch() にアクションの名前がありません。"})
	}
	if legacyBinderAttr.MatchString(raw) {
		out = append(out, bindFinding{line: line, kind: "legacy-binder",
			detail: "data-m-* は以前の HTML の書き方です。{{ }}、data-class-*、data-event-click=\"dispatch(...)\" に書き直してください。"})
	}
	return out
}

// stripFormatters は {{ x | int }} の | より後ろ (書式の名前) を落とす。|| は論理和なので残す。
func stripFormatters(expr string) string {
	for i := 0; i < len(expr); i++ {
		if expr[i] != '|' {
			continue
		}
		if i+1 < len(expr) && expr[i+1] == '|' {
			i++
			continue
		}
		return expr[:i]
	}
	return expr
}

// exprVars は data 式が引く data model の変数名 (根の名前) を返す。メンバー (a.b の b)、
// 関数名 (dispatch( 等)、文字列リテラルの中、組み込みの名前、数字に続く単位 (10px) は除く。
func exprVars(expr string) []string {
	clean := rmlEntity.ReplaceAllString(rmlQuoted.ReplaceAllString(expr, "''"), " ")
	var out []string
	for _, loc := range rmlIdent.FindAllStringIndex(clean, -1) {
		name := clean[loc[0]:loc[1]]
		prev := byte(' ')
		if loc[0] > 0 {
			prev = clean[loc[0]-1]
		}
		next := strings.TrimLeft(clean[loc[1]:], " \t")
		switch {
		case rmlBuiltins[name], prev == '.', prev >= '0' && prev <= '9', strings.HasPrefix(next, "("):
			continue
		}
		out = append(out, name)
	}
	return out
}

// scanProducedKeys は srcDir 配下の C++ 文字列リテラル内に現れる全 dotted path を
// 収集する。key は常に quoted literal なので、hud.set の key も自前 push helper も捕捉できる。
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

// producedViewVars は "view.x" のキーから RML の変数名 x の集合を作る。"view.a.b" のように
// view. の後ろにも点があるキーは、model の中で点を含む名前になり式から引けないので別に返す。
func producedViewVars(produced map[string]bool) (map[string]bool, []string) {
	vars := map[string]bool{}
	var nested []string
	prefix := uiModelName + "."
	for key := range produced {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := key[len(prefix):]
		if strings.Contains(rest, ".") {
			nested = append(nested, key)
			continue
		}
		vars[rest] = true
	}
	sort.Strings(nested)
	return vars, nested
}

func printBindReport(doc string, findings []bindFinding) int {
	if len(findings) == 0 {
		fmt.Printf("%s が使う値は、すべて C++ から送られています。\n", doc)
		return 0
	}

	sort.SliceStable(findings, func(i, j int) bool { return findings[i].line < findings[j].line })
	for _, f := range findings {
		if f.line > 0 {
			fmt.Printf("  %s:%d  %s\n", doc, f.line, f.detail)
		} else {
			fmt.Printf("  %s  %s\n", doc, f.detail)
		}
	}
	fmt.Printf("%d 件あります。C++ が送らない値はエラーにならず、空のまま表示されます。\n", len(findings))
	return len(findings)
}
