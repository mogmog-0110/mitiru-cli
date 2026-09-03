package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// StandaloneOptions は自前の CMakeLists.txt と exe を持つ project
// ([build] kind = "standalone") の build を駆動する。CMakeLists は生成せず、
// engine も取りに行かない。project の CMake がその両方を持っている。
type StandaloneOptions struct {
	// ProjectRoot は mitiru.toml を含む directory の absolute path。
	ProjectRoot string
	// SourceDir は CMakeLists.txt がある directory の absolute path。
	SourceDir string
	// Target は build する CMake target。exe の名前 (拡張子なし) でもある。
	Target string
	// Config は "Debug" または "Release"。
	Config string
	// Generator は明示的な CMake generator の上書き。空なら Ninja。
	Generator string
	// Stdout / Stderr は progress と cmake の出力を受け取る。
	Stdout io.Writer
	Stderr io.Writer
}

// StandaloneOutDir は standalone project の cmake -B。project 自身の build
// script と同じ場所 (<projectRoot>/build) にして、どちらから建てても増分になる
// ようにする。host 型の build/out とは重ならない。
func StandaloneOutDir(projectRoot string) string {
	return filepath.Join(projectRoot, "build")
}

// RunStandalone は configure + build を project 自身の CMake に当て、exe を
// 指した Artifacts を返す。HostExePath がその exe で、DLL 側の項目は空。
func RunStandalone(opts StandaloneOptions) (*Artifacts, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("mitiru build is currently Windows-only (running on %s)",
			runtime.GOOS)
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Config == "" {
		opts.Config = "Debug"
	}
	if _, err := os.Stat(filepath.Join(opts.SourceDir, "CMakeLists.txt")); err != nil {
		return nil, fmt.Errorf("standalone project: no CMakeLists.txt under %s (check [build] source in %s)",
			opts.SourceDir, filepath.Join(opts.ProjectRoot, "mitiru.toml"))
	}

	vcvars, err := FindVcvars64()
	if err != nil {
		return nil, err
	}
	generator := opts.Generator
	if generator == "" {
		generator = generatorForVcvars(vcvars)
	}

	outDir := StandaloneOutDir(opts.ProjectRoot)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create build dir: %w", err)
	}
	if mismatch, cached := generatorMismatch(outDir, generator); mismatch {
		return nil, fmt.Errorf(
			"%s was configured with generator %q; mitiru uses %q.\n"+
				"  pass --generator %q, or remove %s to reconfigure",
			outDir, cached, generator, cached, outDir)
	}

	cmakeOpts := Options{
		ProjectRoot: opts.ProjectRoot,
		ProjectName: opts.Target,
		Config:      opts.Config,
		Generator:   generator,
		Target:      opts.Target,
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
	}
	fmt.Fprintf(opts.Stdout, "Configuring %s (%s)...\n", opts.Target, opts.Config)
	if err := runCMakeConfigure(vcvars, generator, opts.SourceDir, outDir, cmakeOpts); err != nil {
		return nil, err
	}
	fmt.Fprintf(opts.Stdout, "Building %s (%s)...\n", opts.Target, opts.Config)
	if err := runCMakeBuild(vcvars, outDir, cmakeOpts); err != nil {
		return nil, err
	}

	if os.Getenv("MITIRU_DRY_RUN") == "1" {
		exe := filepath.Join(outDir, "bin", opts.Target+".exe")
		return &Artifacts{DeployDir: filepath.Dir(exe), HostExePath: exe}, nil
	}
	exe, err := FindStandaloneExe(outDir, opts.Config, opts.Target)
	if err != nil {
		return nil, err
	}
	return &Artifacts{DeployDir: filepath.Dir(exe), HostExePath: exe}, nil
}

// standaloneExeCandidates は project の CMake が exe を置きそうな場所。
// CMAKE_RUNTIME_OUTPUT_DIRECTORY を bin/ にする流儀、置かない流儀、multi-config
// generator が <Config>/ を挟む流儀の順。
func standaloneExeCandidates(outDir, config, target string) []string {
	name := target + ".exe"
	return []string{
		filepath.Join(outDir, "bin", name),
		filepath.Join(outDir, name),
		filepath.Join(outDir, "bin", config, name),
		filepath.Join(outDir, config, name),
	}
}

// FindStandaloneExe は build 後の exe を探す。無ければ、探した場所を全部並べて
// 返す (target 名の取り違えがすぐ分かるように)。
func FindStandaloneExe(outDir, config, target string) (string, error) {
	candidates := standaloneExeCandidates(outDir, config, target)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf(
		"build succeeded but %s.exe was not found under %s\n"+
			"  expected one of:\n    %s\n"+
			"  ([build] target in mitiru.toml has to be the CMake target that makes the exe)",
		target, outDir, strings.Join(candidates, "\n    "))
}
