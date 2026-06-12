package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BuildErrorFileName は `mitiru watch` がビルド失敗時に書き、成功時に削除する
// エラーファイルの名前。engine 側は host の --error-file 経由でこの path を受け、
// ファイルが存在する間だけゲーム画面上部にエラー帯を描く (LÖVE の青画面方式)。
const BuildErrorFileName = ".mitiru_build_error.txt"

// buildErrorMaxLines はエラーファイルに書く最大行数 (帯は先頭数行しか出さない)。
const buildErrorMaxLines = 20

// MSVC のエラー行: "path(12): error C2065: ..." / "fatal error C1083: ..." /
// "error LNK2019: ..."。cmake/ninja の wrapper 行は対象外。
var buildErrorLineRe = regexp.MustCompile(`error (C|LNK)\d+|fatal error`)

// BuildErrorFilePath は projectRoot に対するエラーファイルの絶対 path を返す。
func BuildErrorFilePath(projectRoot string) string {
	return filepath.Join(projectRoot, "build", BuildErrorFileName)
}

// ExtractBuildErrors は MSVC ビルド出力からエラー行 (error C#### / error LNK#### /
// fatal error) とその前後 1 行を抜き出す。エラー行が 1 つも無い場合 (cmake 自体の
// 失敗等) は出力の末尾 maxLines 行を返す — 帯が空になるよりは生ログの方がよい。
func ExtractBuildErrors(output string, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = buildErrorMaxLines
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")

	// エラー行 ±1 行を採用 set に積む (重複は index set で自然に潰れる)。
	keep := map[int]bool{}
	found := false
	for i, line := range lines {
		if !buildErrorLineRe.MatchString(line) {
			continue
		}
		found = true
		for j := i - 1; j <= i+1; j++ {
			if j >= 0 && j < len(lines) {
				keep[j] = true
			}
		}
	}

	var out []string
	if found {
		for i, line := range lines {
			if !keep[i] {
				continue
			}
			if trimmed := strings.TrimRight(line, " \t"); trimmed != "" {
				out = append(out, sanitizeUTF8(trimmed))
			}
			if len(out) >= maxLines {
				break
			}
		}
		return out
	}

	// fallback: エラーパターン無し → 空でない末尾 maxLines 行。
	for i := len(lines) - 1; i >= 0 && len(out) < maxLines; i-- {
		if trimmed := strings.TrimRight(lines[i], " \t"); trimmed != "" {
			out = append([]string{sanitizeUTF8(trimmed)}, out...)
		}
	}
	return out
}

// WriteBuildErrorFile はビルド出力からエラーを抽出し、UTF-8 のエラーファイルとして
// <projectRoot>/build/.mitiru_build_error.txt に書く。
func WriteBuildErrorFile(projectRoot, output string) error {
	lines := ExtractBuildErrors(output, buildErrorMaxLines)
	if len(lines) == 0 {
		lines = []string{"build failed (no error lines captured)"}
	}
	path := BuildErrorFilePath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create build dir for error file: %w", err)
	}
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return fmt.Errorf("write build error file: %w", err)
	}
	return nil
}

// ClearBuildErrorFile はエラーファイルを削除する (無ければ no-op)。
// engine 側は次の poll で消滅を検知し、帯を消す。
func ClearBuildErrorFile(projectRoot string) {
	_ = os.Remove(BuildErrorFilePath(projectRoot))
}

// sanitizeUTF8 は CP932 等の非 UTF-8 バイト (JP locale の MSVC 出力) を '?' に
// 置換し、エラーファイルを常に valid UTF-8 に保つ。
func sanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "?")
}
