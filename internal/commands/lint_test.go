package commands

import "testing"

func TestProducedCovers(t *testing.T) {
	produced := map[string]bool{
		"view.hud.hp": true,
		"view.shop":   true, // pushed as a JSON object; sub-fields are bound
		"view.eintent": true,
	}
	cases := []struct {
		key  string
		want bool
	}{
		{"view.hud.hp", true},         // exact
		{"view.shop.b0.cost", true},   // sub-field of a pushed object
		{"view.shop", true},           // exact object
		{"view.eintent", true},        // exact
		{"view.eintnet", false},       // typo — must be flagged
		{"view.hud.mana", false},      // sibling never pushed
		{"player.gold", false},        // unrelated root
	}
	for _, c := range cases {
		if got := producedCovers(produced, c.key); got != c.want {
			t.Errorf("producedCovers(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestSegmentPrefix(t *testing.T) {
	mk := func(s string) []string { return splitDots(s) }
	if !segmentPrefix(mk("view.shop"), mk("view.shop.b0")) {
		t.Error("view.shop should be a segment-prefix of view.shop.b0")
	}
	if segmentPrefix(mk("view.eintent"), mk("view.eintnet")) {
		t.Error("eintent/eintnet differ at last segment; must not match")
	}
	if !segmentPrefix(mk("a.b"), mk("a.b")) {
		t.Error("equal paths are a prefix")
	}
}

func TestAnalyzeSceneExtractsKeysAndStructural(t *testing.T) {
	html := `<!doctype html><html><body>
	  <div data-m-text="view.hud.hp">0</div>
	  <div data-m-tpl="HP {view.hud.hp} / {view.hud.max">x</div>
	  <button data-m-action="">go</button>
	  <div data-m-repeat="view.hand"></div>
	  <span data-m-show="view.phase == player"></span>
	</body></html>`

	consumed, structural := analyzeScene(html)

	if _, ok := consumed["view.hud.hp"]; !ok {
		t.Error("expected view.hud.hp to be consumed")
	}
	if _, ok := consumed["view.phase"]; !ok {
		t.Error("expected view.phase extracted from a comparison expr")
	}
	if _, ok := consumed["player"]; ok {
		t.Error("bare comparison literal 'player' must not be treated as a key")
	}

	kinds := map[string]bool{}
	for _, f := range structural {
		kinds[f.kind] = true
	}
	for _, want := range []string{"tpl-braces", "empty-action", "no-binder", "repeat-no-template"} {
		if !kinds[want] {
			t.Errorf("expected structural finding %q, got kinds=%v", want, kinds)
		}
	}
}

// splitDots mirrors strings.Split(s,".") for test readability.
func splitDots(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	return append(out, cur)
}
