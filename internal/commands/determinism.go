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
	{"rand(", "use a seeded std::mt19937 stored in GameMemory"},
	{"srand(", "use a seeded std::mt19937 stored in GameMemory"},
	{"std::random_device", "non-deterministic seed; use a fixed/replay seed"},
	{"std::chrono::system_clock", "use the engine's dt (frame delta), not wall-clock"},
	{"std::chrono::high_resolution_clock", "use the engine's dt (frame delta), not wall-clock"},
	{"steady_clock::now", "use the engine's dt (frame delta), not wall-clock"},
	{"chrono::now()", "use the engine's dt (frame delta), not wall-clock"},
	{"time(", "wall-clock breaks determinism/replay; use dt"},
	{"::time(", "wall-clock breaks determinism/replay; use dt"},
	{"GetTickCount", "wall-clock breaks determinism/replay; use dt"},
	{"timeGetTime", "wall-clock breaks determinism/replay; use dt"},
	{"QueryPerformanceCounter", "wall-clock breaks determinism/replay; use dt"},
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
	fmt.Println("  --- determinism lint ---")

	if len(findings) == 0 {
		fmt.Println("  deterministic: no nondeterministic sources found.")
		return
	}

	for _, f := range findings {
		fmt.Printf("  %s:%d  %s  →  %s\n", f.file, f.line, f.token, f.suggestion)
	}

	fmt.Println()
	fmt.Printf("  %d finding(s). These patterns break replay and time-travel. See suggestions above.\n", len(findings))
}
