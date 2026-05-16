package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
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
			hint: "Install from https://cmake.org/download/ (or 'winget install Kitware.CMake')",
			doneFn: func() bool {
				_, err := exec.LookPath("cmake")
				return err == nil
			},
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
	return nil
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
