package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// detPattern is a source pattern that breaks determinism/replay.
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

// detFinding is a single flagged line.
type detFinding struct {
	file       string
	line       int
	token      string
	suggestion string
}

// runDeterminismLint scans src/**/*.cpp and src/**/*.hpp under projectRoot.
// It returns all findings (never an error — a missing src/ is treated as zero findings).
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

// scanFile returns all determinism findings in a single file.
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

		// Skip single-line comments (limitation: does not handle block comments).
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Skip string literals heuristically: if the token only appears inside
		// double-quoted regions, skip. We use a simple approach — strip content
		// inside double quotes before matching.
		stripped := stripStringLiterals(trimmed)

		for _, p := range detPatterns {
			if strings.Contains(stripped, p.token) {
				findings = append(findings, detFinding{
					file:       path,
					line:       lineNum,
					token:      p.token,
					suggestion: p.suggestion,
				})
				// Only report the first matching pattern per line to avoid duplicates.
				break
			}
		}
	}

	return findings
}

// stripStringLiterals removes content inside double-quoted strings to avoid
// false positives from log messages that happen to contain pattern text.
// This is a best-effort heuristic and does not handle raw string literals.
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

// printDeterminismReport writes the lint output to stdout.
// It does NOT return an error — findings are warnings only.
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
