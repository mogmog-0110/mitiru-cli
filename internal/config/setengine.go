package config

import (
	"fmt"
	"os"
	"regexp"
)

// engineLineRe は [project] 配下の `engine = "..."` 行にマッチする。`engine` は
// schema 上 [project] にしか現れないため、セクション追跡なしでも安全。
var engineLineRe = regexp.MustCompile(`(?m)^(\s*engine\s*=\s*)"[^"]*"(.*)$`)

// SetEngine は manifest の engine pin を newVersion ("X.Y.Z" 形) に書き換える。
//
// toml.Encode で再 marshal するとコメントが全消滅する (mitiru.toml には設計意図が
// コメントで載っている — ADR 0010 の失敗モード #5)。そのため engine 行だけを
// 正規表現で置換し、他は 1 byte も触らない surgical rewrite を行う。
func SetEngine(manifestPath, newVersion string) error {
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}

	if !engineLineRe.Match(body) {
		return fmt.Errorf("%s: no `engine = \"...\"` line found to update", manifestPath)
	}

	replaced := engineLineRe.ReplaceAll(body, []byte(`${1}"`+newVersion+`"${2}`))

	// パーミッションは既存ファイルに合わせる (なければ 0644)。
	mode := os.FileMode(0o644)
	if st, statErr := os.Stat(manifestPath); statErr == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(manifestPath, replaced, mode); err != nil {
		return fmt.Errorf("write %s: %w", manifestPath, err)
	}
	return nil
}
