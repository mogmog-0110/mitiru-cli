package commands

import (
	"reflect"
	"sort"
	"testing"
)

func TestExprVars(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		{"level", []string{"level"}},
		{"level &lt; 33 ? '#e8338a' : 'x'", []string{"level"}},
		{"dispatch('sakura', !sakura)", []string{"sakura"}},
		{"dispatch('speed', ev.value)", nil},
		{"p.on", []string{"p"}},
		{"(x - 20) / 280 * 100 + '%'", []string{"x"}},
		{"confirm('本当に?', 'かくにん', 'reset')", nil},
		{"ui_confirm_open", nil},
	}
	for _, c := range cases {
		got := exprVars(c.expr)
		if !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("exprVars(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestStripFormatters(t *testing.T) {
	if got := stripFormatters(" time | time "); got != " time " {
		t.Errorf("stripFormatters kept the formatter: %q", got)
	}
	if got := stripFormatters("a || b"); got != "a || b" {
		t.Errorf("stripFormatters cut a logical or: %q", got)
	}
}

func TestLintRMLFindsUnpushedAndStructural(t *testing.T) {
	doc := `<rml><body data-model="view">
  <!-- {{ commented }} is not checked -->
  <div>{{ score | comma }}</div>
  <div data-if="lives &gt; 0">{{ livse }}</div>
  <div class="pip" data-for="p : pips" data-class-lit="p.on"></div>
  <button data-event-click="dispatch('')">x</button>
  <div data-m-text="view.old"></div>
</body></rml>`
	produced := map[string]bool{
		"view.score": true, "view.lives": true, "view.pips": true,
		"view.boss.hp": true, // view. の後ろに点 — RML から引けない
		"pop.wav":      true, // view. で始まらない文字列は無関係
	}

	kinds := map[string][]string{}
	for _, f := range lintRML(doc, produced) {
		kinds[f.kind] = append(kinds[f.kind], f.detail)
	}
	if got := kinds["unpushed"]; len(got) != 1 {
		t.Errorf("want exactly one unpushed finding (livse), got %v", got)
	}
	for _, want := range []string{"nested-key", "empty-action", "legacy-binder"} {
		if len(kinds[want]) == 0 {
			t.Errorf("expected a %q finding, got kinds=%v", want, keys(kinds))
		}
	}
	if len(kinds["no-model"]) != 0 {
		t.Error("data-model=\"view\" is present; no-model must not fire")
	}
}

func TestLintRMLCleanDocument(t *testing.T) {
	doc := `<rml><body data-model="view">
  <span class="value">{{ speed }}</span>
  <input type="range" data-attr-value="speed" data-event-change="dispatch('speed', ev.value)"/>
  <button data-class-on="sakura" data-event-click="dispatch('sakura', !sakura)">x</button>
  <div class="gauge-fill" data-style-width="gauge + '%'"></div>
</body></rml>`
	produced := map[string]bool{"view.speed": true, "view.sakura": true, "view.gauge": true}
	if f := lintRML(doc, produced); len(f) != 0 {
		t.Errorf("clean document produced findings: %+v", f)
	}
}

func TestLintRMLWithoutModel(t *testing.T) {
	f := lintRML(`<rml><body><div>{{ x }}</div></body></rml>`, map[string]bool{"view.x": true})
	if len(f) != 1 || f[0].kind != "no-model" {
		t.Errorf("want a single no-model finding, got %+v", f)
	}
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
