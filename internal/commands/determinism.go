package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// detPattern は determinism/replay を壊す source pattern。
type detPattern struct {
	token      string
	suggestion string
}

var detPatterns = []detPattern{
	{"rand(", "ゲームの状態の struct に、種を決めた std::mt19937 を持たせて使ってください。"},
	{"srand(", "ゲームの状態の struct に、種を決めた std::mt19937 を持たせて使ってください。"},
	{"std::random_device", "実行のたびに違う種になります。決まった種か、リプレイに記録した種を使ってください。"},
	{"std::chrono::system_clock", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"std::chrono::high_resolution_clock", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"steady_clock::now", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"chrono::now()", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"time(", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"::time(", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"GetTickCount", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"timeGetTime", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
	{"QueryPerformanceCounter", "壁時計の時刻ではなく、エンジンが渡す dt (前のフレームからの経過時間) を使ってください。"},
}

// detFinding は flag された 1 行。
type detFinding struct {
	file       string
	line       int
	token      string
	suggestion string
}

// runDeterminismLint は projectRoot 下の src/**/*.cpp と src/**/*.hpp を scan する。
// 全 findings を返す (error は返さない — src/ が無い場合は findings ゼロ扱い)。
func runDeterminismLint(projectRoot string) []detFinding {
	srcDir := filepath.Join(projectRoot, "src")
	var findings []detFinding

	_ = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".cpp" && ext != ".hpp" {
			return nil
		}
		findings = append(findings, scanFile(path)...)
		return nil
	})

	return findings
}

// scanFile は単一ファイル内の全 determinism findings を返す。
func scanFile(path string) []detFinding {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var findings []detFinding
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)

		// 単一行 comment を skip (制限: block comment は扱わない)。
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		// string literal を heuristic に skip: token が double-quote 内にしか
		// 現れないなら skip。簡易手法 — match 前に double-quote 内の内容を
		// 除去する。
		stripped := stripStringLiterals(trimmed)

		for _, p := range detPatterns {
			if strings.Contains(stripped, p.token) {
				findings = append(findings, detFinding{
					file:       path,
					line:       lineNum,
					token:      p.token,
					suggestion: p.suggestion,
				})
				// 重複回避のため 1 行につき最初に match した pattern のみ report。
				break
			}
		}
	}

	return findings
}

// stripStringLiterals は double-quote 文字列内の内容を除去し、pattern text を
// たまたま含む log message からの false positive を避ける。
// best-effort な heuristic で、raw string literal は扱わない。
func stripStringLiterals(line string) string {
	var b strings.Builder
	inString := false
	prev := rune(0)
	for _, ch := range line {
		if ch == '"' && prev != '\\' {
			inString = !inString
			b.WriteRune(ch)
		} else if !inString {
			b.WriteRune(ch)
		}
		prev = ch
	}
	return b.String()
}

// printDeterminismReport は lint 出力を stdout に書く。
// error は返さない — findings は warning のみ。
func printDeterminismReport(findings []detFinding) {
	fmt.Println()
	if len(findings) == 0 {
		fmt.Println("src/ に、リプレイや巻き戻しの結果を変えるコードは見つかりませんでした。")
		return
	}

	fmt.Printf("src/ に、リプレイや巻き戻しで同じ結果を出せなくするコードが %d 件あります。\n", len(findings))
	for _, f := range findings {
		fmt.Printf("  %s:%d  %s  %s\n", f.file, f.line, f.token, f.suggestion)
	}
}
