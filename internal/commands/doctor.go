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
			hint:   "mitiru は今のところ Windows で動かすものです。",
			doneFn: func() bool { return runtime.GOOS == "windows" },
		},
		{
			name: "CMake",
			hint: "https://cmake.org/download/ から入れてください (winget install Kitware.CMake でも入ります)。" +
				"Visual Studio 2022 の「C++ CMake tools for Windows」を入れてもかまいません。",
			doneFn: hasCMake,
		},
		{
			name: "git",
			hint: "https://git-scm.com/download/win から入れてください (winget install Git.Git でも入ります)。",
			doneFn: func() bool {
				_, err := exec.LookPath("git")
				return err == nil
			},
		},
		{
			name:   "Visual Studio Build Tools",
			hint:   "Visual Studio 2022 Build Tools を C++ のワークロード付きで入れてください。vcvars64.bat が要ります。",
			doneFn: hasVcvars64,
		},
		{
			name: "Windows SDK",
			hint: "Visual Studio 2022 と一緒に入ります。",
			doneFn: func() bool {
				return os.Getenv("WindowsSdkDir") != "" ||
					dirExists(`C:\Program Files (x86)\Windows Kits\10`)
			},
		},
	}

	allOK := true
	for _, c := range checks {
		ok := c.doneFn()
		allOK = allOK && ok
		printCheck(ok, c.name, c.hint)
	}

	if !allOK {
		return fmt.Errorf("足りないものがあります。上の説明のとおりに入れてください。")
	}

	fmt.Println("ビルドに要るものはそろっています。")

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

	printSymptomTable()

	return nil
}

// symptom は「よくある症状 → 対処」の 1 行分。原因を断定できない症状 (環境やプロジェクトごとに
// 事情が異なる) はチェック項目化できないため、doctor 本体の自動判定とは別に固定テキストで並べる。
type symptom struct {
	what string
	why  string
	fix  string
}

// printSymptomTable は doctor の自動チェックが拾えない「よくある詰まり」を症状表として出す。
// 対象は最初の数分で最も多く踏まれる 4 種 (2026-09-16 の初心者導線相談で挙がったもの)。
func printSymptomTable() {
	symptoms := []symptom{
		{
			what: "初回のビルドに 5〜10 分かかる",
			why:  "初回はエンジン本体を RmlUi と FreeType ごとコンパイルするからです。2 回目からは数秒から数十秒で終わります。",
			fix:  "初回は待ってください。毎回長いときは、並列にビルドできているか (cmake --build build -j N) を確かめてください。",
		},
		{
			what: "mitiru run で host が起動してすぐ止まり、DLL が見つからないと言われる",
			why:  "mitiru build が失敗したか、まだ実行していないので、host の隣に <game>.dll がありません。",
			fix:  "mitiru build が通ってから mitiru run してください。ほかの DLL が足りないときは上の一覧を見てください。",
		},
		{
			what: "プロセスは動いているのに、窓が出ないか真っ黒のまま",
			why:  "GPU を使う準備に失敗しています。",
			fix:  "mitiru run -v で起動し、GPU について出る文を確かめてから、GPU のドライバを見直してください。",
		},
		{
			what: "--record で撮った入力を再生すると、元と違う動きになる",
			why:  "GameMemory の外に状態 (乱数、時刻、std::vector など) があるか、撮ったあとでゲームのロジックを変えています。",
			fix:  "状態は GameMemory に置き、ロジックを変えたら撮り直してください。",
		},
	}

	fmt.Println()
	fmt.Println("よくあるつまずき")
	for _, s := range symptoms {
		fmt.Printf("  %s\n", s.what)
		fmt.Printf("    %s\n", s.why)
		fmt.Printf("    %s\n", s.fix)
	}
}

// printRuntimeChecks は build 済み host の起動前提を診断する (R-02)。
// host の隣にパッドの DLL (SDL3.dll、古い engine は SDL2.dll) と UI の RCSS が居るか、Debug CRT が VS toolchain PATH で
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
	fmt.Printf("ビルド済みの %s を動かすのに要るもの\n", hostExe)
	hostDir := filepath.Dir(hostExe)
	// RCSS は host の隣か 1 つ上 (multi-config generator の Debug/ の親) にあれば RmlUi が見つける。
	rcss := filepath.Join("assets", "ui", "base.rcss")
	deps := []struct{ name, path, alt string }{
		{"SDL3.dll (gamepads)", filepath.Join(hostDir, "SDL3.dll"), filepath.Join(hostDir, "SDL2.dll")},
		{"assets/ui/base.rcss", filepath.Join(hostDir, rcss), filepath.Join(filepath.Dir(hostDir), rcss)},
	}
	for _, d := range deps {
		ok := fileExists(d.path) || (d.alt != "" && fileExists(d.alt))
		printCheck(ok, d.name+" (mitiru_host.exe の隣)",
			"mitiru build をもう一度実行してください。host の隣に必要なファイルを置き直します。")
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
			hint = "Visual Studio のツールの PATH に Debug CRT が見つかりません。Visual Studio の C++ のワークロードを修復してください。"
		}
	} else {
		hint = vsErr.Error()
	}
	printCheck(crtOK, "Debug ビルドの host が使う Debug CRT (msvcp140d、ucrtbased)", hint)
}

// printCheck は doctor の 1 項目を出す。足りないときだけ、その下に対処を出す。
func printCheck(ok bool, name, hint string) {
	if ok {
		fmt.Printf("  OK    %s\n", name)
		return
	}
	fmt.Printf("  なし  %s\n", name)
	if hint != "" {
		fmt.Printf("        %s\n", hint)
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
