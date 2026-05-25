//go:build windows

package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// envReport は「今このマシンに何があるか」の snapshot。Step 1 で取得し、
// 以降の installer の動作を駆動する。
type envReport struct {
	hasWinget bool
	hasMsvc   bool
	hasCMake  bool
	hasSDK    bool

	// mitiruPath は PATH 上の既存 `mitiru` の絶対パス (なければ空)。
	// 「target dir に既に install 済み」と「別の location に install 済み —
	// それを shadow することになる」を区別する。
	mitiruPath string
}

// snapshot は現在の environment 状態を返す。
func snapshot() (*envReport, error) {
	r := &envReport{}

	if _, err := exec.LookPath("winget"); err == nil {
		r.hasWinget = true
	}
	r.hasMsvc = hasVcvars64()
	r.hasCMake = hasCMake()
	r.hasSDK = hasWindowsSDK()

	if p, err := exec.LookPath("mitiru"); err == nil {
		// target dir との比較が効くよう絶対パスに解決する。
		if abs, absErr := filepath.Abs(p); absErr == nil {
			r.mitiruPath = abs
		} else {
			r.mitiruPath = p
		}
	}
	return r, nil
}

func (r *envReport) hasMitiru() bool { return r.mitiruPath != "" }

func (r *envReport) print(w io.Writer) {
	mark := func(ok bool) string {
		if ok {
			return "[OK     ]"
		}
		return "[MISSING]"
	}
	fmt.Fprintf(w, "  %s Windows + winget\n", mark(r.hasWinget))
	fmt.Fprintf(w, "  %s MSVC Build Tools 2022 (C++ workload)\n", mark(r.hasMsvc))
	fmt.Fprintf(w, "  %s CMake\n", mark(r.hasCMake))
	fmt.Fprintf(w, "  %s Windows SDK\n", mark(r.hasSDK))
	if r.hasMitiru() {
		fmt.Fprintf(w, "  [OK     ] mitiru on PATH (%s)\n", r.mitiruPath)
	} else {
		fmt.Fprintln(w, "  [MISSING] mitiru on PATH")
	}
}

// hasVcvars64 — internal/commands/doctor.go からの複製。
// (循環依存を避けるため inline に保持: commands が install を import している。)
func hasVcvars64() bool {
	candidates := []string{
		`C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat`,
		`C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Auxiliary\Build\vcvars64.bat`,
		`C:\Program Files\Microsoft Visual Studio\2022\Professional\VC\Auxiliary\Build\vcvars64.bat`,
		`C:\Program Files\Microsoft Visual Studio\2022\Enterprise\VC\Auxiliary\Build\vcvars64.bat`,
		`C:\Program Files\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	matches, _ := filepath.Glob(`C:\Program Files\Microsoft Visual Studio\*\*\VC\Auxiliary\Build\vcvars64.bat`)
	return len(matches) > 0
}

func hasCMake() bool {
	if _, err := exec.LookPath("cmake"); err == nil {
		return true
	}
	matches, _ := filepath.Glob(
		`C:\Program Files\Microsoft Visual Studio\*\*\Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe`)
	return len(matches) > 0
}

func hasWindowsSDK() bool {
	if os.Getenv("WindowsSdkDir") != "" {
		return true
	}
	st, err := os.Stat(`C:\Program Files (x86)\Windows Kits\10`)
	return err == nil && st.IsDir()
}
