package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var (
	runInspectArg  string // --inspect の生値 ("" = 窓なし、単独指定は NoOptDefVal "inspect")
	runInspectPage string // resolveInspectPage 済みの tool page 名 ("" = 窓なし)
	runWithConsole bool
	runRecordFile  string
	runLearn       bool // --learn: scene 窓 (game memory タブ) を自動で開く
)

// tomlHostArgs は cwd の mitiru.toml の [window] / [font] / [lofi] を
// mitiru_host の CLI 引数に変換する。toml が単一の真実になるよう run / watch の
// 両方から使う。manifest が無ければ空。
func tomlHostArgs() []string {
	mp, _, err := config.FindManifest(".")
	if err != nil {
		return nil
	}
	pc, err := config.Load(mp)
	if err != nil {
		return nil
	}
	return hostArgsFromConfig(pc)
}

// hostArgsFromConfig は読み込み済み manifest を host 引数に変換する純関数
// ([window]→--size / [font]→--font / [lofi]→--lofi 系)。テスト容易化のため分離。
func hostArgsFromConfig(pc *config.ProjectConfig) []string {
	var extra []string
	if pc.Window.Width > 0 && pc.Window.Height > 0 {
		extra = append(extra, "--size", fmt.Sprintf("%dx%d", pc.Window.Width, pc.Window.Height))
	}
	if pc.Window.FixedSize {
		extra = append(extra, "--fixed-size") // #50: ユーザのリサイズを禁止 (#44 の host フラグ)
	}
	if atlas := strings.TrimSpace(pc.Font.Atlas); atlas != "" && atlas != "none" {
		extra = append(extra, "--font", atlas)
	}
	// [lofi]: enabled がマスタースイッチ。低解像+量子化+ディザ (#10 の host フラグへ)。
	if pc.Lofi.Enabled {
		extra = append(extra, "--lofi")
		if pc.Lofi.Width > 0 && pc.Lofi.Height > 0 {
			extra = append(extra, "--lofi-size", fmt.Sprintf("%dx%d", pc.Lofi.Width, pc.Lofi.Height))
		}
		if bits := strings.TrimSpace(pc.Lofi.Bits); bits != "" {
			extra = append(extra, "--lofi-bits", bits)
		}
		if pc.Lofi.Dither != nil {
			extra = append(extra, "--lofi-dither",
				strconv.FormatFloat(*pc.Lofi.Dither, 'g', -1, 64))
		}
		if pc.Lofi.Vi {
			extra = append(extra, "--lofi-vi")
		}
		if pc.Lofi.Gamma != nil {
			extra = append(extra, "--lofi-gamma",
				strconv.FormatFloat(*pc.Lofi.Gamma, 'g', -1, 64))
		}
	}
	return extra
}

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Build and run the current project",
		Long: `Build the current project (same as 'mitiru build') and then launch
the resulting executable with the project root as its working directory.

Standard output, standard error, and exit code are forwarded.

The deployed assets/ folder is passed to the host as --watch-assets, so
editing a tuning JSON there reaches the running game as asset.reloaded
([run] watch_assets = false in mitiru.toml turns it off).

With --inspect, also opens a tool window alongside the game and
shuts it down when the game exits. Bare --inspect opens the gameplay
inspector; a window name selects another tool:

  mitiru run --inspect             # gameplay inspector
  mitiru run --inspect perf        # performance window
  mitiru run --inspect rewind      # 巻き戻し窓 (past-frame rewind)
  mitiru run --inspect story       # cutscene / dialogue / flags / quests
                                   # (perf, inspector, rewind, mixer,
                                   #  scene, replay, input, story,
                                   #  side_state, ai, nav, anim)
  mitiru run --learn                # scene 窓を game memory タブで自動で開く (--inspect を知らなくてよい)

A standalone project ([build] kind = "standalone") runs its own exe instead
of mitiru_host; arguments after -- go to that exe:

  mitiru run -- --selftest`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// standalone では引数は exe のもの (`mitiru run -- --selftest`)。
			if standaloneProjectHere() {
				return runStandalone(args)
			}
			page, err := resolveInspectPage(runInspectArg, args)
			if err != nil {
				return err
			}
			runInspectPage = learnInspectPage(page, runLearn)
			if runLearn {
				warnIfNoAutoReflect(".")
			}
			return runRun()
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "build + run with Release configuration")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo); overrides --release")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (e.g. \"Visual Studio 17 2022\", \"NMake Makefiles\"); default is Ninja")
	cmd.Flags().StringVar(&runInspectArg, "inspect", "",
		"also open a tool window: perf|inspector|rewind|mixer|scene|replay|input|story|side_state|ai|nav|anim (bare --inspect = gameplay inspector)")
	cmd.Flags().Lookup("inspect").NoOptDefVal = "inspect"
	cmd.Flags().BoolVar(&runWithConsole, "console", false,
		"open the runtime control panel (pause/step/scale/screenshot) in your default browser")
	cmd.Flags().StringVar(&runRecordFile, "record", "",
		"record this session's input to <file>.mtrr for `mitiru replay --test --game`")
	cmd.Flags().BoolVar(&runLearn, "learn", false,
		"open the scene window on its game memory tab automatically (for first-time exploration)")
	return cmd
}

// learnInspectPage は --inspect 解決済み page と --learn を合成する。page が
// 未指定 (窓なし) かつ --learn 指定なら "scene" を返す。C++ には実行時
// リフレクションが無いため独自機構を新設せず、既存の GameMemory reflect JSON
// (scene 窓の「game memory」タブ) をそのまま初見導線に使い回す。
// page が既に何か指定されていれば --learn は何もしない (明示指定を優先)。
func learnInspectPage(page string, learn bool) string {
	if page == "" && learn {
		return "scene?tab=memory" // mitiru_tool は ? 以降をページへの問い合わせとして渡す
	}
	return page
}

// warnIfNoAutoReflect は --learn 使用時、プロジェクトの src/ に MITIRU_REFLECT_AUTO /
// MITIRU_REFLECT が 1 つも無ければヒントを 1 行出す。この宣言が無いと GameMemory の
// フィールドを反射する情報がコンパイル時に存在しないため、inspector を開いても空になる
// (C++ には実行時リフレクションが無く、macro がコード生成した schema 登録に頼っているため)。
// 既定の `mitiru run` (--learn 無し) の挙動は変えない (pulled UI: 頼まれた時だけ言う)。
func warnIfNoAutoReflect(projectRoot string) {
	srcDir := filepath.Join(projectRoot, "src")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".cpp") && !strings.HasSuffix(name, ".hpp") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(srcDir, name))
		if readErr != nil {
			continue
		}
		if strings.Contains(string(data), "MITIRU_REFLECT") {
			return // 見つかった (AUTO/手動どちらでも可)
		}
	}
	fmt.Fprintln(os.Stderr, "src/ に MITIRU_REFLECT_AUTO(YourGameType) が無いので、--learn で開く窓には何も表示されません。"+
		"GameMemory の struct 定義の後ろにこの 1 行を足すと、すべてのフィールドが見えるようになります。")
}

func runRun() error {
	result, err := runBuild()
	if err != nil {
		return err
	}

	// build 成功 → 過去の watch セッションが残した stale なエラーファイルを掃除する
	// (残っていると起動直後のゲーム画面に古いエラー帯が出てしまう)。
	build.ClearBuildErrorFile(result.ProjectRoot)

	art := result.Artifacts
	console.Verbosef("Running %s %s\n", filepath.Base(art.HostExePath), art.DllRel)

	hostArgs := []string{art.DllRel}
	// 並走中の `mitiru watch` 等がエラーファイルを書いたら帯を出せるよう、
	// run でも host に場所を渡しておく (ファイルが無い間は完全 no-op)。
	hostArgs = append(hostArgs, "--error-file", build.BuildErrorFilePath(result.ProjectRoot))
	if runRecordFile != "" {
		abs, err := filepath.Abs(runRecordFile)
		if err != nil {
			return fmt.Errorf("--record に指定した %q を絶対パスにできません (%w)。", runRecordFile, err)
		}
		hostArgs = append(hostArgs, "--record", abs)
		console.Verbosef("Recording input to %s\n", abs)
	}

	// mitiru.toml の [window] サイズ / [font] atlas を host へ渡す。
	hostArgs = append(hostArgs, tomlHostArgs()...)
	hostArgs = append(hostArgs, watchAssetsArgs(result.Config, art, result.EngineRoot)...)

	// --console: control panel を既定ブラウザで自動表示する。
	if runWithConsole {
		hostArgs = append(hostArgs, "--console")
	}

	cmd := exec.Command(art.HostExePath, hostArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = art.DeployDir
	// Debug ビルドの host は Debug CRT (msvcp140d 等) に依存し素の shell の PATH
	// では解決できない → ビルドに使う VS toolchain の PATH を前置する (R-01)。
	cmd.Env = build.HostEnv()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s を起動できません (%w)。", art.HostExePath, err)
	}

	var inspectorCmd *exec.Cmd
	if runInspectPage != "" {
		ic, err := startInspectorChild(cmd.Process.Pid, runInspectPage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--inspect の窓を開けませんでした。%v。\n", err)
		} else {
			inspectorCmd = ic
			console.Verbosef("Inspector pid: %d (will close when game exits)\n", ic.Process.Pid)
		}
	}

	waitErr := cmd.Wait()

	if inspectorCmd != nil && inspectorCmd.Process != nil {
		_ = inspectorCmd.Process.Kill()
		_, _ = inspectorCmd.Process.Wait()
	}

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			// 異常終了コードは 16 進デコード + 既知コードのヒント付きで出す (R-02)。
			return fmt.Errorf("%s", hostExitMessage(filepath.Base(art.HostExePath), exitErr.ExitCode()))
		}
		return fmt.Errorf("%s の終わりを待てませんでした (%w)。", filepath.Base(art.HostExePath), waitErr)
	}

	// 更新の知らせは成功時の画面に出さない。-v のときだけ末尾に添える。
	maybeNotifyUpdates(result.Config.Project.Engine, console.VerboseWriter(os.Stdout))
	return nil
}

// standaloneProjectHere は cwd の manifest が standalone 型かを返す。manifest が
// 無い、または読めないときは false にして、いつもの経路にいつもの error を出させる。
func standaloneProjectHere() bool {
	mp, _, err := config.FindManifest(".")
	if err != nil {
		return false
	}
	pc, err := config.Load(mp)
	return err == nil && pc.Standalone()
}

// runStandalone は project 自身の exe を、project root を cwd にして起動する。
// host 向けの窓 (inspector、console、record) は host が無いので付けられない。
func runStandalone(exeArgs []string) error {
	if runInspectArg != "" || runWithConsole || runRecordFile != "" {
		return fmt.Errorf("--inspect、--console、--record は mitiru_host で動かすときだけ使えます。standalone のプロジェクトでは外してください。")
	}
	result, err := runAnyBuild()
	if err != nil {
		return err
	}
	exe := result.Artifacts.HostExePath
	console.Verbosef("Running %s %s\n", filepath.Base(exe), strings.Join(exeArgs, " "))

	cmd := exec.Command(exe, exeArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = result.ProjectRoot
	// Debug ビルドは Debug CRT に依存し、素の shell の PATH では解決できない。
	cmd.Env = build.HostEnv()
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s", hostExitMessage(filepath.Base(exe), exitErr.ExitCode()))
		}
		return fmt.Errorf("%s を起動できません (%w)。", filepath.Base(exe), err)
	}
	return nil
}

// startInspectorChild はツール窓のホスト mitiru_tool.exe を `--page <page>` で起動し、
// 動作中ゲームの pid を指す child process にする。
// launch 前にゲームの snapshot file を短時間 poll し、即「waiting」表示を避ける。
func startInspectorChild(gamePid int, page string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("--inspect は今のところ Windows でだけ使えます")
	}
	engineRoot, err := engine.EnsureSource("latest", os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("エンジンのソースが見つかりません (%w)", err)
	}
	exePath := findToolExe(engineRoot)
	if exePath == "" {
		return nil, fmt.Errorf("%s.exe が見つかりません。一度 `cmake --build <engine>/build --target %s` を実行してください",
			toolExeTarget, toolExeTarget)
	}

	// producer が最初の snapshot file を書くのを短時間待つ (ファイル名は IPC 規約上 inspector_)。
	snap := filepath.Join(os.TempDir(),
		fmt.Sprintf("mitiru_inspector_%d.json", gamePid))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(snap); err == nil {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}

	c := exec.Command(exePath, "--page", page, fmt.Sprintf("%d", gamePid))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = filepath.Dir(exePath)
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("窓を起動できません (%w)", err)
	}
	return c, nil
}
