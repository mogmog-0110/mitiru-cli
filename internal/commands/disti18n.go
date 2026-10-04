package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// distI18nListMax は訳の点検の一覧に出す行の上限。残りは件数だけを書く。
const distI18nListMax = 20

// i18nIssue は host --check-i18n が出す 1 件。Kind は missing (訳が無い)、glyphs (書体に無い字)、unreadable (表を読めない)。
type i18nIssue struct {
	Kind, File, Lang, Key, Chars string
}

// i18nReport は訳の点検の結果。Checked が false なら、その engine の host は点検を持たない。
type i18nReport struct {
	Checked bool
	Files   int
	Issues  []i18nIssue
}

// parseI18nReport は host の出力から "i18n\t" で始まる行を拾う。最後の done の行が無ければ点検されていない。
func parseI18nReport(out string) i18nReport {
	var r i18nReport
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 2 || f[0] != "i18n" {
			continue
		}
		switch {
		case f[1] == "done" && len(f) >= 3:
			r.Checked = true
			fmt.Sscan(f[2], &r.Files)
		case f[1] == "unreadable" && len(f) >= 3:
			r.Issues = append(r.Issues, i18nIssue{Kind: f[1], File: f[2]})
		case (f[1] == "missing" || f[1] == "glyphs") && len(f) >= 5:
			is := i18nIssue{Kind: f[1], File: f[2], Lang: f[3], Key: f[4]}
			if len(f) >= 6 {
				is.Chars = f[5]
			}
			r.Issues = append(r.Issues, is)
		}
	}
	return r
}

// i18nIssueLine は 1 件を「ファイル  言語  キー: 何が足りないか」の 1 行にする。
func i18nIssueLine(is i18nIssue) string {
	switch is.Kind {
	case "unreadable":
		return fmt.Sprintf("%s: JSON として読めません", is.File)
	case "glyphs":
		return fmt.Sprintf("%s  %s  %s: 書体に無い字があります (%s)", is.File, is.Lang, is.Key, is.Chars)
	default:
		return fmt.Sprintf("%s  %s  %s: 訳がありません", is.File, is.Lang, is.Key)
	}
}

// formatI18nReport は件数と一覧を返す。件数が 0 なら空。
func formatI18nReport(r i18nReport) string {
	if len(r.Issues) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "訳の表 (strings.json) に足りないものが %d 件あります。", len(r.Issues))
	for i, is := range r.Issues {
		if i == distI18nListMax {
			fmt.Fprintf(&b, "\n  ほか %d 件", len(r.Issues)-distI18nListMax)
			break
		}
		b.WriteString("\n  " + i18nIssueLine(is))
	}
	return b.String()
}

// runI18nCheck は配布物の host に、プロジェクトの assets の下の strings.json を点検させる。
// 書体の並びは配布物の中の同梱の書体から始まるので、遊ぶ側の PC で出る字と同じになる。
func runI18nCheck(host, assetsDir string) (i18nReport, error) {
	if assetsDir == "" {
		return i18nReport{}, nil
	}
	if _, err := os.Stat(assetsDir); err != nil {
		return i18nReport{Checked: true}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, host, "--check-i18n", assetsDir)
	cmd.Env = cleanEnviron(os.Environ())
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return i18nReport{}, fmt.Errorf("dist --check: 訳を点検する host を起動できない: %w", err)
	}
	// host はファイルを assets からの相対で出す。プロジェクトから見た場所で見せる
	r := parseI18nReport(out.String())
	for i := range r.Issues {
		r.Issues[i].File = filepath.ToSlash(filepath.Join(filepath.Base(assetsDir), r.Issues[i].File))
	}
	return r, nil
}

// i18nVerdict は点検の結果を表示し、strict のときだけ足りないものをエラーにする。
func i18nVerdict(r i18nReport, strict bool) error {
	if !r.Checked {
		msg := "この engine の host は訳を点検できない (--check-i18n が無い) ので、訳の点検は飛ばしました。"
		if strict {
			return errors.New(msg + " --strict-i18n を外すか、engine を新しくしてください。")
		}
		fmt.Println(msg)
		return nil
	}
	text := formatI18nReport(r)
	if text == "" {
		if r.Files > 0 {
			fmt.Printf("訳の表 %d 個に、訳の抜けと書体に無い字はありませんでした。\n", r.Files)
		}
		return nil
	}
	if strict {
		return errors.New(text)
	}
	fmt.Println(text + "\n  (配布物はこのまま作りました。足りない訳を失敗として扱うには --strict-i18n を付けます)")
	return nil
}
