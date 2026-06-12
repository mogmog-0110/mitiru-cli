package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that prerequisites are installed",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

type check struct {
	name   string
	hint   string
	doneFn func() bool
}

func runDoctor() error {
	checks := []check{
		{
			name:   "OS",
			hint:   "Windows is the primary supported platform",
			doneFn: func() bool { return runtime.GOOS == "windows" },
		},
		{
			name: "CMake",
			hint: "Install from https://cmake.org/download/ (or 'winget install Kitware.CMake'). " +
				"Alternatively, install Visual Studio 2022 with the 'C++ CMake tools for Windows' component.",
			doneFn: hasCMake,
		},
		{
			name: "git",
			hint: "Install from https://git-scm.com/download/win (or 'winget install Git.Git')",
			doneFn: func() bool {
				_, err := exec.LookPath("git")
				return err == nil
			},
		},
		{
			name:   "Visual Studio Build Tools",
			hint:   "Install Visual Studio 2022 Build Tools (C++ workload). vcvars64.bat must exist.",
			doneFn: hasVcvars64,
		},
		{
			name: "Windows SDK",
			hint: "Installed alongside Visual Studio 2022.",
			doneFn: func() bool {
				return os.Getenv("WindowsSdkDir") != "" ||
					dirExists(`C:\Program Files (x86)\Windows Kits\10`)
			},
		},
	}

	allOK := true
	for _, c := range checks {
		ok := c.doneFn()
		mark := "OK"
		if !ok {
			mark = "MISSING"
			allOK = false
		}
		fmt.Printf("  [%-7s] %s\n", mark, c.name)
		if !ok {
			fmt.Printf("            hint: %s\n", c.hint)
		}
	}

	if !allOK {
		fmt.Println()
		fmt.Println("Some prerequisites are missing. See hints above.")
		return fmt.Errorf("doctor: prerequisites missing")
	}

	fmt.Println()
	fmt.Println("All prerequisites look good.")

	// determinism lint — warn のみ、command を fail させない。
	cwd, err := os.Getwd()
	if err == nil {
		_, projectRoot, manifestErr := config.FindManifest(cwd)
		if manifestErr == nil {
			printRuntimeChecks(projectRoot)
			findings := runDeterminismLint(projectRoot)
			printDeterminismReport(findings)
		}
		// mitiru.toml が見つからなければ lint を黙って skip する。
	}

	return nil
}

// printRuntimeChecks は build 済み host の起動前提を診断する (R-02)。
// host の隣に SDL2.dll / libcef.dll が居るか、Debug CRT が VS toolchain PATH で
// 解決できるかを表示する。host 未ビルドなら黙って skip。warn のみで fail させない。
func printRuntimeChecks(projectRoot string) {
	outDir := filepath.Join(projectRoot, "build", "out")
	hostExe := ""
	for _, c := range []string{
		filepath.Join(outDir, "mitiru_host.exe"),
		filepath.Join(outDir, "Debug", "mitiru_host.exe"),
		filepath.Join(outDir, "Release", "mitiru_host.exe"),
	} {
		if _, err := os.Stat(c); err == nil {
			hostExe = c
			break
		}
	}
	if hostExe == "" {
		return // まだ build していない project では診断対象なし
	}

	fmt.Println()
	fmt.Printf("Runtime checks (%s):\n", hostExe)
	hostDir := filepath.Dir(hostExe)
	for _, dll := range []string{"SDL2.dll", "libcef.dll"} {
		mark := "OK"
		if _, err := os.Stat(filepath.Join(hostDir, dll)); err != nil {
			mark = "MISSING"
		}
		fmt.Printf("  [%-7s] %s next to mitiru_host.exe\n", mark, dll)
		if mark == "MISSING" {
			fmt.Println("            hint: re-run `mitiru build` (deploys runtime DLLs next to the host)")
		}
	}

	// Debug CRT: 隣に手動配置済みか、VS toolchain PATH で解決できれば OK。
	// (`mitiru run` / `watch` / `verify` は起動時にこの PATH を自動前置する。)
	crtOK := false
	hint := ""
	if _, err := os.Stat(filepath.Join(hostDir, "ucrtbased.dll")); err == nil {
		crtOK = true
	} else if vsPath, vsErr := build.VsToolchainPath(); vsErr == nil {
		crtOK = build.FindInPathList(vsPath, "ucrtbased.dll") &&
			build.FindInPathList(vsPath, "msvcp140d.dll")
		if !crtOK {
			hint = "Debug CRT not found in the VS toolchain PATH; repair the Visual Studio C++ workload"
		}
	} else {
		hint = "vcvars64.bat not found: " + vsErr.Error()
	}
	mark := "OK"
	if !crtOK {
		mark = "MISSING"
	}
	fmt.Printf("  [%-7s] Debug CRT (msvcp140d/ucrtbased) resolvable for Debug-built hosts\n", mark)
	if hint != "" {
		fmt.Printf("            hint: %s\n", hint)
	}
}

// hasCMake は利用可能な cmake.exe に到達できるか報告する。CMake は
// stand-alone install (PATH 上の cmake) か、Visual Studio 2022 同梱の
// "C++ CMake tools for Windows" component のどちらか由来。両方とも受け入れる。
func hasCMake() bool {
	if _, err := exec.LookPath("cmake"); err == nil {
		return true
	}
	matches, _ := filepath.Glob(
		`C:\Program Files\Microsoft Visual Studio\*\*\Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe`)
	return len(matches) > 0
}

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

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
