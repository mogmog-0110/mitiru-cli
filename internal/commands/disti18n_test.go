package commands

import (
	"strings"
	"testing"
)

const hostI18nOut = "[mitiru] something else\r\n" +
	"i18n\tunreadable\tassets/ui/pause/strings.json\r\n" +
	"i18n\tmissing\tassets/ui/strings.json\tja\tmenu.quit\r\n" +
	"i18n\tglyphs\tassets/ui/strings.json\tko\tmenu.start\t시작\r\n" +
	"i18n\tdone\t2\t3\r\n"

func TestParseI18nReportReadsTheHostLines(t *testing.T) {
	r := parseI18nReport(hostI18nOut)
	if !r.Checked || r.Files != 2 || len(r.Issues) != 3 {
		t.Fatalf("report = %+v", r)
	}
	want := i18nIssue{Kind: "glyphs", File: "assets/ui/strings.json", Lang: "ko", Key: "menu.start", Chars: "시작"}
	if r.Issues[2] != want {
		t.Errorf("glyph issue = %+v, want %+v", r.Issues[2], want)
	}
	if parseI18nReport("unknown option --check-i18n\n").Checked {
		t.Error("a host without the done line has not checked anything")
	}
}

func TestFormatI18nReportNamesFileLanguageAndKey(t *testing.T) {
	text := formatI18nReport(parseI18nReport(hostI18nOut))
	for _, want := range []string{
		"3 件",
		"assets/ui/strings.json  ja  menu.quit: 訳がありません",
		"assets/ui/strings.json  ko  menu.start: 書体に無い字があります (시작)",
		"assets/ui/pause/strings.json: JSON として読めません",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	var many i18nReport
	for i := 0; i < distI18nListMax+5; i++ {
		many.Issues = append(many.Issues, i18nIssue{Kind: "missing", File: "s.json", Lang: "ja", Key: "k"})
	}
	if text := formatI18nReport(many); !strings.Contains(text, "ほか 5 件") || strings.Count(text, "\n") != distI18nListMax+1 {
		t.Errorf("a long list is cut at %d lines:\n%s", distI18nListMax, text)
	}
}

func TestI18nVerdictFailsOnlyWhenStrict(t *testing.T) {
	r := parseI18nReport(hostI18nOut)
	if err := i18nVerdict(r, false); err != nil {
		t.Errorf("without --strict-i18n the list is a warning, got %v", err)
	}
	if err := i18nVerdict(r, true); err == nil || !strings.Contains(err.Error(), "menu.quit") {
		t.Errorf("--strict-i18n must fail and name the key, got %v", err)
	}
	if err := i18nVerdict(i18nReport{Checked: true, Files: 1}, true); err != nil {
		t.Errorf("a clean table passes even when strict, got %v", err)
	}
	if err := i18nVerdict(i18nReport{}, true); err == nil {
		t.Error("--strict-i18n cannot pass when the host could not check")
	}
	if err := i18nVerdict(i18nReport{}, false); err != nil {
		t.Errorf("an old host only skips the check, got %v", err)
	}
}
