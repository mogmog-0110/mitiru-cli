package commands

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"github.com/spf13/cobra"
)

var (
	distOut  string
	distZip  bool
	distExe  bool
	distBat  bool
	distPack bool
	distOne  bool
	// distDebug は Debug ビルドを配る (テスト機で落ちた所を追うため)。Debug 版ランタイムは再頒布できない
	distDebug bool
	// distCheck は作った配布物を一時フォルダへ写し、素の PC に近い環境で headless に走らせて確かめる
	distCheck bool
)

// distShipExe は top-level で配布してよい exe (host と、CEF 世代の engine の helper)。他のツール exe
// (mitiru_inspector / mitiru_perf / mitiru_mixer / mitiru_replay / mitiru_scene_tree 等)
// は配布物に含めない。
var distShipExe = map[string]bool{
	"mitiru_host.exe": true, "MitiruCefHelper.exe": true,
}

// distRuntimeDirs は exe の隣のうち、ゲームの dir 以外で配布物に入れる dir。
var distRuntimeDirs = map[string]bool{"locales": true, "assets": true, "dxc": true, "slang": true}

// distJunkExt は配布物に含めない build linker 中間物と、書きかけの一時ファイル。
var distJunkExt = map[string]bool{".ilk": true, ".pdb": true, ".exp": true, ".lib": true, ".tmp": true}

// isDistRuntimeJunk は ゲーム dir 配下の相対パス (スラッシュ区切り) が
// 開発専用ファイルかを判定する。engine の web runtime は HUD の実行に使う
// binder だけ配り、テストページ・fixtures・debug 用 JS は落とす。
func isDistRuntimeJunk(rel string) bool {
	low := strings.ToLower(filepath.ToSlash(rel))
	if !strings.Contains(low, "mitiru_runtime/") {
		return false
	}
	switch {
	case strings.Contains(low, "mitiru_runtime/tests/"):
		return true
	case strings.Contains(low, "mitiru_test_"),
		strings.Contains(low, "mitiru_debug.js"),
		strings.Contains(low, "mitiru_crash_reporter.js"):
		return true
	default:
		return false
	}
}

// isDistDropTopLevel は top-level ファイル (rel に "/" なし) を配布から外すか判定する。
// DeployDir は cmake 出力 dir なので CMakeCache.txt / build.ninja / *.cmake / 他ツール exe /
// build log 等が同居する。drop ルールに当たらないものは全て KEEP — 特に host が実際に
// import 依存する全 *.dll (vcpkg SDL2.dll 等) と runtime data (*.pak/*.dat/*.bin/*.json) を
// allowlist で取りこぼさないため、deny 方式に倒す。
func isDistDropTopLevel(base string) bool {
	low := strings.ToLower(base)
	ext := strings.ToLower(filepath.Ext(base))
	switch {
	case distJunkExt[ext]: // .ilk/.pdb/.exp/.lib/.tmp
		return true
	case base == "CMakeCache.txt", base == "build.ninja", base == "cmake_install.cmake":
		return true
	case strings.HasPrefix(base, "CMakeDoxy"): // CMakeDoxyfile.in / CMakeDoxygenDefaults.cmake
		return true
	case ext == ".cmake": // *.cmake (cmake 生成物)
		return true
	case strings.HasSuffix(low, ".ninja_log"), low == ".ninja_deps":
		return true // build log
	case ext == ".stamp", low == "compile_commands.json", low == "mitiru_build.json":
		return true // ビルドの印と記録。compile_commands.json はビルドした機械の絶対パスを持つ
	case base == "mitiru_start.exe":
		return true // ランチャ stub は data/ ではなくトップに置く (別途コピー)
	case ext == ".exe" && !distShipExe[base]:
		return true // host 以外の exe は配布しない
	default:
		return false
	}
}

func newDistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dist",
		Short: "Package the current project into a distributable folder",
		Long: `Build the project in Release and assemble a self-contained, runnable
bundle — the host, the engine runtime, your game DLL and assets, plus a
double-clickable launcher .bat.

The top level holds a double-clickable <name>.exe launcher (a tiny GUI stub
that shows NO console window) plus README.txt; all runtime (host, DLLs, UI
stylesheets and fonts, your game, assets) lives in data/. Move/copy the whole folder as one unit.

Use --bat to also emit a console-visible <name>.bat (useful for reading logs
while debugging). --exe additionally drops a Steam-style data/<name>.exe.

Before packing, the bundled host loads the game's assets headless on DX12
(mitiru_host --bake-caches) so the converted models (.clod / .fbx.glb),
BC-compressed textures (.dds) and compiled shaders (data/shader_cache/) ship
inside the bundle: the first run does not stall and a read-only install
works. The list comes from assets/bake.txt when present, otherwise from a
scan of assets/. An asset that fails to load fails the dist; a host that
cannot run (no DX12 GPU) only warns. --no-bake skips this step.

--sign signs the launcher, host and game DLL (and the --onefile exe) with
signtool from the Windows SDK. The certificate comes from environment
variables only: MITIRU_SIGN_CERT_FILE (+ MITIRU_SIGN_CERT_PASSWORD) or
MITIRU_SIGN_CERT_THUMBPRINT. MITIRU_SIGNTOOL and MITIRU_SIGN_TIMESTAMP_URL
are optional.

Examples:
  mitiru dist                 # → dist/<name>/  (no-console <name>.exe)
  mitiru dist --bat           # also add a console-visible <name>.bat
  mitiru dist --zip           # also produce dist/<name>.zip
  mitiru dist --pack=false    # keep loose assets/ (packing is the default)
  mitiru dist --sign          # Authenticode-sign our own binaries
  mitiru dist --no-bake       # skip the cache pre-bake (no GPU on this machine)
  mitiru dist --out build/ship`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDist()
		},
	}
	cmd.Flags().StringVar(&distOut, "out", "dist", "output directory for the bundle")
	cmd.Flags().BoolVar(&distZip, "zip", false, "also produce a .zip next to the bundle")
	cmd.Flags().BoolVar(&distExe, "exe", false,
		"also copy <name>.exe (mitiru_host) into data/ as a Steam entry point")
	cmd.Flags().BoolVar(&distBat, "bat", false,
		"also write a console-visible <name>.bat launcher (handy for log/debug)")
	// pack は既定 ON (2026-08-23)。配布物に assets がバラ置きされると、譜面・台本・
	// 絵が誰でも読めて書き換えられる。開発ビルド (mitiru build / run) はこれまで
	// どおり生ファイルで、dist だけが秘匿形になる。
	cmd.Flags().BoolVar(&distPack, "pack", true,
		"embed assets/ into a single assets.mtpak (default on; --pack=false to keep loose files)")
	cmd.Flags().BoolVar(&distOne, "onefile", false,
		"fold the whole bundle into a single self-extracting <name>.exe (Windows)")
	cmd.Flags().BoolVar(&distDebug, "debug", false,
		"ship Debug binaries (bundles the non-redistributable Debug CRT; for your own test machines only)")
	cmd.Flags().BoolVar(&distCheck, "check", false,
		"after packaging, run the bundle headless from a temp copy with a clean environment and fail on missing files")
	cmd.Flags().BoolVar(&distNoBake, "no-bake", false,
		"skip pre-baking the load caches (converted models, BC-compressed textures, compiled shaders) into the bundle")
	cmd.Flags().BoolVar(&distSign, "sign", false,
		"sign our own exe/DLLs with signtool; env: MITIRU_SIGN_CERT_FILE (+ MITIRU_SIGN_CERT_PASSWORD) "+
			"or MITIRU_SIGN_CERT_THUMBPRINT, optional MITIRU_SIGNTOOL, MITIRU_SIGN_TIMESTAMP_URL (default "+
			defaultTimestamp+")")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (default Ninja)")
	return cmd
}

func runDist() error {
	cwd, _ := os.Getwd()
	_, projectRoot, ferr := config.FindManifest(cwd)
	if ferr != nil {
		return ferr
	}
	var signCfg signConfig
	if distSign {
		cfg, err := loadSignConfig(os.Getenv, exec.LookPath)
		if err != nil {
			return err
		}
		signCfg = cfg
	}

	// dist 専用ビルド: コンソール窓を出さない GUI host にする。dev の build/out を
	// 汚さないよう別 out dir (configure-time オプションの thrash 回避)。
	// 既定は Release。Debug は別の out dir にして、Release の配布物へ Debug 版ランタイムが混ざらないようにする
	buildRelease = !distDebug
	buildOutDir = filepath.Join(projectRoot, "build", "dist-out")
	if distDebug {
		buildOutDir += "-debug"
		fmt.Println("--debug は Debug ビルドを配布物にします。Debug 版のランタイムは再頒布できないので、自分のテスト機だけで使ってください。")
	}
	buildExtraDefines = []string{"MITIRU_HOST_GUI=ON"}
	defer func() { buildOutDir = ""; buildExtraDefines = nil }() // 後続コマンドへ漏らさない

	result, err := runBuild()
	if err != nil {
		return err
	}
	cfg, art := result.Config, result.Artifacts

	name := distBundleName(cfg.Project.Name)
	bundleRoot, err := filepath.Abs(filepath.Join(distOut, name))
	if err != nil {
		return fmt.Errorf("dist: resolve out dir: %w", err)
	}
	if err := os.RemoveAll(bundleRoot); err != nil {
		return fmt.Errorf("dist: clear %s: %w", bundleRoot, err)
	}
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return fmt.Errorf("dist: mkdir %s: %w", bundleRoot, err)
	}

	gameDir := strings.SplitN(filepath.ToSlash(art.DllRel), "/", 2)[0]

	// ランタイム一式 (host + 全 DLL + UI の RCSS と書体 + ゲーム) は data/ サブフォルダに隔離し、
	// トップ階層はランチャーだけにする (DLL の散らかりを隠す)。host は自分の exe dir
	// (= data/) を cwd に固定するので、data/ 内で全パスが完結する。
	dataDir := filepath.Join(bundleRoot, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("dist: mkdir data: %w", err)
	}
	n, err := copyDeploy(art.DeployDir, dataDir, gameDir)
	if err != nil {
		return err
	}
	notices, err := writeBundleNotices(projectRoot, bundleRoot, dataDir)
	if err != nil {
		return err
	}

	// セーブと設定は %APPDATA%/<name>/ に置く。指定しないと data/save/ に書くので、
	// Program Files に入れると書けず、onefile では展開し直すたびに消える。
	hostArgs := append(hostArgsFromConfig(cfg), "--game-name", name)

	// 顔つき: window title は project.name。project root に icon.ico があれば
	// data/ へ同梱し --icon で window icon にも使う (無ければ既定のまま = 正当)。
	iconSrc := filepath.Join(projectRoot, "icon.ico")
	hasIcon := false
	if _, statErr := os.Stat(iconSrc); statErr == nil {
		if err := copyFile(iconSrc, filepath.Join(dataDir, "icon.ico")); err != nil {
			return fmt.Errorf("dist: copy icon.ico: %w", err)
		}
		hasIcon = true
		n++
	}
	// 窓の表題は [window] title。project.name はファイル名の
	// ASCII 識別子で、遊ぶ側に見せる名前ではない。
	hostArgs = append(hostArgs, distFaceArgs(cfg.Window.Title, hasIcon)...)

	// 既定ランチャ: トップ階層の <name>.exe = GUI stub (mitiru_start)。コンソール窓を
	// 一切出さずに data\mitiru_host.exe を起動する。stub は data\launch.mtargs から
	// host への argv を読む (cwd=data 相対)。
	launchArgs := filepath.ToSlash(art.DllRel)
	if len(hostArgs) > 0 {
		launchArgs += " " + mtargsJoin(hostArgs)
	}
	stubSrc := filepath.Join(art.DeployDir, "mitiru_start.exe")
	stubUsed := false
	if _, statErr := os.Stat(stubSrc); statErr == nil {
		stubDst := filepath.Join(bundleRoot, name+".exe")
		if err := copyFile(stubSrc, stubDst); err != nil {
			return fmt.Errorf("dist: copy launcher stub: %w", err)
		}
		if hasIcon {
			// stub exe 自体の PE リソースにも埋める (explorer の顔)。失敗しても
			// アイコン無し配布は正当なので警告のみで続行。
			if err := embedExeIcon(stubDst, iconSrc); err != nil {
				fmt.Printf("exe にアイコンを埋め込めませんでした (%v)。アイコン無しで続けます。\n", err)
			}
		}
		if err := os.WriteFile(filepath.Join(dataDir, "launch.mtargs"),
			[]byte(launchArgs+"\n"), 0o644); err != nil {
			return fmt.Errorf("dist: write launch.mtargs: %w", err)
		}
		stubUsed = true
		n += 2
	}

	// 依存 DLL を確かめ、VC ランタイムを data/ に置く。欠けたまま配ると、遊ぶ側の PC で起動前に落ちる。
	runtimeDLLs, err := ensureDistRuntime(dataDir, distDebug)
	if err != nil {
		return err
	}
	n += runtimeDLLs
	if stubUsed {
		if err := checkStandaloneExe(filepath.Join(bundleRoot, name+".exe")); err != nil {
			return err
		}
	}

	// stub が無い (古い engine / 非 Windows) ときは .bat にフォールバック。
	batName := name + ".bat"
	writeBat := distBat || !stubUsed
	if writeBat {
		if err := writeLauncher(filepath.Join(bundleRoot, batName), art.DllRel, hostArgs); err != nil {
			return err
		}
		n++
	}

	if distExe {
		// Steam 等の .exe 起点向けに、host を data/<name>.exe としても置く (sidecar mtargs)。
		if err := writeExeLauncher(dataDir, name, art.DllRel, hostArgs); err != nil {
			return err
		}
		n++
	}

	// cache は pack に畳む前に、バラ置きの資産の隣へ作る。DDS・clod・glb は pack に入らずバラ置きで残る
	if !distNoBake {
		if err := bakeDistCaches(dataDir, art.DllRel, gameDir, hostArgsFromConfig(cfg),
			execDistBakeRunner, os.Stdout); err != nil {
			return err
		}
	}

	// 署名は exe へアイコンを埋めたあと、onefile がバイナリを畳む前に済ませる。
	// 署名後に PE を書き換えると署名が壊れる。
	if distSign {
		files, err := distSignTargets(bundleRoot, dataDir, name, gameDir, distExe)
		if err != nil {
			return err
		}
		if err := signFiles(signCfg, files, distSignRunner, os.Stdout); err != nil {
			return err
		}
	}

	if distPack {
		// <gameDir>/assets/ を <gameDir>/assets.mtpak に畳んで、バラ置きを除去する。
		// キーは host / native loader が要求する cwd 相対パス "<gameDir>/assets/..."。
		// assets/ui/ と、ディスクから直に読まれる種類 (keepLooseInDist) はバラ置きのまま残す。
		assetsDir := filepath.Join(dataDir, gameDir, "assets")
		if _, statErr := os.Stat(assetsDir); statErr == nil {
			packOut := filepath.Join(dataDir, gameDir, "assets.mtpak")
			packed, loose, perr := packAssets(assetsDir, packOut, gameDir+"/assets")
			if perr != nil {
				return fmt.Errorf("dist --pack: %w", perr)
			}
			if rmErr := removePackedAssets(assetsDir, packed); rmErr != nil {
				return fmt.Errorf("dist --pack: remove loose assets: %w", rmErr)
			}
			if len(packed) == 0 {
				console.Verbosef("dist --pack: nothing to pack (%d loose files)\n", loose)
			} else {
				console.Verbosef("Packed %d assets into %s (assets/ui/ and %d files read from disk stay loose)\n",
					len(packed), packOut, loose)
			}
		} else {
			console.Verbosef("dist --pack: no assets/ to pack\n")
		}
	}

	// 起動方法を決める (README / 最終メッセージ共通)。
	primary := batName
	if stubUsed {
		primary = name + ".exe"
	}

	// トップに README を置き、構成を 1 行で説明する (中身は data/)。
	readme := name + " — MitiruEngine game\r\n\r\n" +
		primary + " をダブルクリックで起動。\r\n" +
		"data/ にランタイム一式が入っています (移動・削除しないでください)。\r\n"
	// 頒布物の README はサークル名・利用規約・連絡先を載せる作者の文書で、
	// ここで生成した 3 行に毎回上書きされては書く意味が無い。
	// プロジェクトに README.dist.txt があればそれをそのまま配る。
	if custom, rerr := os.ReadFile(filepath.Join(projectRoot, "README.dist.txt")); rerr == nil {
		readme = string(custom)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "README.txt"), []byte(readme), 0o644); err != nil {
		return err
	}
	n++

	if distCheck {
		shot := filepath.Join(projectRoot, "build", "dist-check", name+".png")
		if err := checkDistBundle(bundleRoot, shot); err != nil {
			return err
		}
	}

	// zip は README を書いたあとに作る。頒布セットは
	//   dist/README.txt      ← アップロード先で zip の隣に並べる
	//   dist/<name>.zip      ← 中は exe + data/ だけ
	// の最小構成にする。README は bundleRoot (フォルダのまま配る人向け) には残し、
	// zip には入れない。
	// ── 単一 exe 化 (--onefile) ────────────────────────────────────────
	// bundle 一式を selfrun (自己展開ランチャ) の末尾へ連結し、dist/<name>.exe
	// 1 つにする。配布先にアセットや DLL が生のフォルダとして現れない。
	// host と DLL は exe から直接は動かせないので、初回起動でローカルへ展開する器になる。
	onefileExe := ""
	if distOne {
		selfrun := filepath.Join(art.DeployDir, "mitiru_selfrun.exe")
		selfpack := filepath.Join(art.DeployDir, "mitiru_selfpack.exe")
		for _, tool := range []string{selfrun, selfpack} {
			if _, statErr := os.Stat(tool); statErr != nil {
				return fmt.Errorf("dist --onefile: %s が無い。engine が古いか "+
					"apps/mitiru_selfrun・mitiru_selfpack を持っていない", filepath.Base(tool))
			}
		}
		// 配布 exe の顔は selfrun 側に焼く。selfpack は渡された stub を複製して
		// 末尾へ連結するだけなので、複製元にアイコンを入れておけば付いてくる。
		stub := selfrun
		if hasIcon {
			stub = filepath.Join(filepath.Dir(bundleRoot), ".selfrun_face.exe")
			if err := copyFile(selfrun, stub); err != nil {
				return fmt.Errorf("dist --onefile: copy selfrun: %w", err)
			}
			if err := embedExeIcon(stub, iconSrc); err != nil {
				fmt.Printf("exe にアイコンを埋め込めませんでした (%v)。アイコン無しで続けます。\n", err)
			}
			defer os.Remove(stub)
		}
		// 配布 exe の名前は [dist] exe_name。遊ぶ側が受け取るファイルなので、
		// ASCII 識別子の project.name とは分けられるようにしてある。
		exeName := strings.TrimSpace(cfg.Dist.ExeName)
		if exeName == "" {
			exeName = name
		}
		onefileExe = filepath.Join(filepath.Dir(bundleRoot), exeName+".exe")
		_ = os.Remove(onefileExe)
		packCmd := exec.Command(selfpack, onefileExe, stub, bundleRoot,
			name, "data/mitiru_host.exe", launchArgs, "data")
		packCmd.Stdout, packCmd.Stderr = os.Stdout, os.Stderr
		if err := packCmd.Run(); err != nil {
			return fmt.Errorf("dist --onefile: selfpack: %w", err)
		}
		if distSign {
			if err := signFiles(signCfg, []string{onefileExe}, distSignRunner, os.Stdout); err != nil {
				return err
			}
		}
		// 展開元のフォルダは配布物ではない。残すと「exe と data/ の両方を配る」
		// 形に見えて、単一 exe にした意味が消える。
		if err := os.RemoveAll(bundleRoot); err != nil {
			return fmt.Errorf("dist --onefile: remove bundle dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(bundleRoot), "README.txt"),
			[]byte(readme), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(bundleRoot), distNoticesFile),
			notices, 0o644); err != nil {
			return err
		}
		info, _ := os.Stat(onefileExe)
		console.Verbosef("Onefile OK: %s (%.1f MB)\n", onefileExe, float64(info.Size())/(1024*1024))
	}

	if distZip && onefileExe != "" {
		// onefile では bundle フォルダはもう無い。頒布セットは
		//   dist/README.txt  ← zip の外
		//   dist/<name>.zip  ← 単一 exe + 第三者ライセンス表記
		zipPath := filepath.Join(filepath.Dir(bundleRoot),
			strings.TrimSuffix(filepath.Base(onefileExe), ".exe")+".zip")
		members := []string{onefileExe, filepath.Join(filepath.Dir(bundleRoot), distNoticesFile)}
		if err := zipFiles(members, zipPath); err != nil {
			return err
		}
		fmt.Printf("%s に zip でまとめました。README.txt は zip の外に置いてあります。\n", zipPath)
	} else if distZip {
		zipPath := bundleRoot + ".zip"
		if err := zipDir(bundleRoot, filepath.Dir(bundleRoot), zipPath,
			name+"/README.txt"); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(bundleRoot), "README.txt"),
			[]byte(readme), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s に zip でまとめました。README.txt は zip の外に置いてあります。\n", zipPath)
	}

	launch := primary
	if stubUsed && writeBat {
		launch += " か " + batName
	}
	if onefileExe != "" {
		info, _ := os.Stat(onefileExe)
		fmt.Printf("%s に配布物を作りました。配るのは %s (%.1f MB) と README.txt の 2 つで、"+
			"ダブルクリックで起動します (初回だけ中身を展開します)。\n",
			filepath.Dir(bundleRoot), filepath.Base(onefileExe), float64(info.Size())/(1024*1024))
		return nil
	}
	fmt.Printf("%s に配布物を作りました (%d 個のファイル)。%s をダブルクリックすると起動します。\n",
		bundleRoot, n, launch)
	return nil
}

// writeExeLauncher は mitiru_host.exe を <name>.exe にコピーし、引数なしで
// 起動されたとき読まれる sidecar <name>.mtargs を書く。host は引数なし起動時に
// この .mtargs を argv として読む (Steam 等の .exe 起点に対応)。
func writeExeLauncher(bundleRoot, name, dllRel string, hostArgs []string) error {
	host := filepath.Join(bundleRoot, "mitiru_host.exe")
	if _, err := os.Stat(host); err != nil {
		return fmt.Errorf("dist --exe: mitiru_host.exe not found in bundle: %w", err)
	}
	if err := copyFile(host, filepath.Join(bundleRoot, name+".exe")); err != nil {
		return fmt.Errorf("dist --exe: copy host: %w", err)
	}
	args := filepath.ToSlash(dllRel)
	if len(hostArgs) > 0 {
		args += " " + mtargsJoin(hostArgs)
	}
	return os.WriteFile(filepath.Join(bundleRoot, name+".mtargs"), []byte(args+"\n"), 0o644)
}

// distBundleName は配布フォルダ名を ASCII-safe にする (zip / bat 名で安全)。
func distBundleName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z',
			r >= '0' && r <= '9', r == '_', r == '-':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	s := strings.Trim(string(out), "_")
	if s == "" {
		s = "game"
	}
	return s
}

// copyDeploy は DeployDir から配布に必要なものだけを bundle へコピーする。
// ゲーム dir (gameDir) と locales/ (CEF 世代の engine) は丸ごと、host の隣の assets/ は
// RmlUi が読む RCSS と書体だけ (isDistEngineAsset)、top-level は deny 方式 (isDistDropTopLevel
// に当たらないものは全て KEEP) で全 runtime dll / data を取りこぼさない。
func copyDeploy(src, dst, gameDir string) (int, error) {
	count := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		first := strings.SplitN(rel, "/", 2)[0]
		base := info.Name()

		if info.IsDir() {
			if strings.HasPrefix(base, "cef_cache_") || base == "CMakeFiles" || base == "__pycache__" {
				return filepath.SkipDir
			}
			// top-level dir は gameDir と locales と assets と、exe の隣の dxc/・slang/ (engine が置く DLL) だけ降りる。
			if !strings.Contains(rel, "/") && rel != gameDir && !distRuntimeDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}

		if distJunkExt[strings.ToLower(filepath.Ext(base))] {
			return nil
		}
		switch {
		case first == gameDir, first == "locales":
			// ゲーム dir 配下 / locales は入れる。ただし開発専用の runtime は落とす。
			if isDistRuntimeJunk(rel) {
				return nil
			}
		case first == "assets":
			if !isDistEngineAsset(rel) {
				return nil
			}
		case first == "dxc", first == "slang":
		case !strings.Contains(rel, "/"): // top-level ファイル: drop ルールに当たるものだけ除外
			if isDistDropTopLevel(base) {
				return nil
			}
		default:
			return nil
		}
		if err := copyFile(path, filepath.Join(dst, rel)); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeLauncher は host を game DLL + host 引数で起動する .bat を書く。
func writeLauncher(path, dllRel string, hostArgs []string) error {
	args := dllRel
	if len(hostArgs) > 0 {
		args += " " + mtargsJoin(hostArgs)
	}
	body := "@echo off\r\n" +
		"rem MitiruEngine game launcher\r\n" +
		"cd /d \"%~dp0data\"\r\n" + // ランタイムは data/ に隔離されている
		"mitiru_host.exe " + args + "\r\n" +
		"if errorlevel 1 pause\r\n" + // 起動失敗時はエラーを読めるよう留める (一瞬で消えない)
		""
	return os.WriteFile(path, []byte(body), 0o644)
}

// zipDir は root 以下を、base からの相対パスを arcname にして zip 化する
// (展開すると <name>/ フォルダが現れる)。skip に挙げた arcname (スラッシュ区切り)
// は入れない — 頒布セットは「README + zip」を並べる形なので、README を zip の
// 中に重複させない。
// zipFiles は指定したファイルだけを、ディレクトリ構造を持たない zip に固める。
// onefile 配布は「単一 exe + ライセンス表記」の 2 つだけなので、zipDir の
// ディレクトリ走査は要らない。
func zipFiles(paths []string, zipPath string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	for _, src := range paths {
		in, oerr := os.Open(src)
		if oerr != nil {
			return oerr
		}
		w, cerr := zw.Create(filepath.Base(src))
		if cerr != nil {
			in.Close()
			return cerr
		}
		_, werr := io.Copy(w, in)
		in.Close()
		if werr != nil {
			return werr
		}
	}
	return nil
}

func zipDir(root, base, zipPath string, skip ...string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		arc := filepath.ToSlash(rel)
		for _, sk := range skip {
			if arc == sk {
				return nil
			}
		}
		w, err := zw.Create(arc)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
}

// isDistEngineAsset は host の隣の assets/ (engine の同梱物) のうち、配布に要るものかを返す。
// 配るのは RML の mitiru:*.rcss、engine 自身の画面 (クラッシュ報告の同意など) の RML と文言表の JSON、
// RML の <img src="glyph:..."> が読むボタン絵とそのライセンス、UI の既定書体 (M PLUS Rounded 1c) と
// そのライセンスだけだ。
func isDistEngineAsset(rel string) bool {
	low := strings.ToLower(filepath.ToSlash(rel))
	switch {
	case strings.HasPrefix(low, "assets/ui/") && (strings.HasSuffix(low, ".rcss") ||
		strings.HasSuffix(low, ".rml") || strings.HasSuffix(low, ".json")):
		return true
	case strings.HasPrefix(low, "assets/glyphs/") && strings.HasSuffix(low, ".png"),
		low == "assets/glyphs/license.txt":
		return true
	case strings.HasPrefix(low, "assets/fonts/mplusrounded1c-") && strings.HasSuffix(low, ".ttf"):
		return true
	case low == "assets/fonts/ofl.txt":
		return true
	default:
		return false
	}
}
