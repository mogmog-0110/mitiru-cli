//go:build windows

package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// envReport is the snapshot of "what's currently on this machine" used at
// Step 1 to drive the rest of the installer.
type envReport struct {
	hasWinget bool
	hasMsvc   bool
	hasCMake  bool
	hasSDK    bool
	hasMitiru bool
	onPath    bool
}

// snapshot returns the current environment state.
func snapshot() (*envReport, error) {
	r := &envReport{}

	if _, err := exec.LookPath("winget"); err == nil {
		r.hasWinget = true
	}
	r.hasMsvc = hasVcvars64()
	r.hasCMake = hasCMake()
	r.hasSDK = hasWindowsSDK()

	if _, err := exec.LookPath("mitiru"); err == nil {
		r.hasMitiru = true
		r.onPath = true
	}
	return r, nil
}

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
	fmt.Fprintf(w, "  %s mitiru.exe on PATH\n", mark(r.hasMitiru))
}

// hasVcvars64 — duplicated from internal/commands/doctor.go.
// (kept inline to avoid a cyclic dependency: commands imports install.)
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
