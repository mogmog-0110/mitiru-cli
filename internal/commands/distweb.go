package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var (
	// distWeb は配布物をブラウザ向け (WebAssembly) にする。署名、installer、VC++ ランタイムは要らない
	distWeb bool
	// distSingleFile は --web で、wasm と素材を埋め込んだ index.html 1 つにする
	distSingleFile bool
)

// distWebConflicts は --web と一緒に使えないフラグ。ブラウザ向けの配布物は 1 つのフォルダ (index.html、
// index.js、index.wasm、素材) で、素材は wasm の仮想ファイルシステムに載せるので、pack や onefile の出番が無い。
var distWebConflicts = []string{"bat", "exe", "onefile", "debug", "sign", "no-bake", "check", "check-script",
	"check-frames", "strict-i18n"}

// rejectWebConflicts は --web と衝突するフラグが明示されていたら、その名前を並べた error を返す。
func rejectWebConflicts(changed func(name string) bool) error {
	var bad []string
	for _, f := range distWebConflicts {
		if changed(f) {
			bad = append(bad, "--"+f)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("--web はブラウザ向けの配布物を作るだけなので、%s は一緒に使えません。", strings.Join(bad, "、"))
}

// webIgnoredNotices は mitiru.toml のうち、ブラウザ向けでは効かない設定への知らせ。
func webIgnoredNotices(cfg *config.ProjectConfig) []string {
	var notes []string
	if len(cfg.Engine.Features) > 0 {
		notes = append(notes, "[engine] features はブラウザ向けでは効きません (無視します)。")
	}
	if cfg.Render.NeedsSet {
		notes = append(notes, "[render] needs はブラウザ向けでは効きません (WebGL2 は宣言を使いません)。")
	}
	return notes
}

// webMmlNotices は、WAV が隣に無い .mml を並べる。dist --web は .mml を焼かず assets/ をそのまま載せるので、
// mitiru build がまだ WAV を焼いていないと、ブラウザでその音が鳴らない。
func webMmlNotices(audioDir string) []string {
	var notes []string
	_ = filepath.WalkDir(audioDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".mml") {
			return nil
		}
		wav := strings.TrimSuffix(path, filepath.Ext(path)) + ".wav"
		if !fileExists(wav) {
			notes = append(notes, fmt.Sprintf("%s の WAV がありません。dist --web は .mml を焼かないので、先に mitiru build で焼いてください。", path))
		}
		return nil
	})
	return notes
}

func emsdkMissingError(cause error) error {
	return fmt.Errorf("%v\n"+
		"ブラウザ向けの配布には Emscripten の SDK (emsdk) が要ります。入れるもの: emsdk 本体と、そこから入れる Emscripten のツールチェーン (ディスクに約 2 GB)。\n"+
		"手順は https://emscripten.org/docs/getting_started/downloads.html にあります。入れたら、場所を環境変数 EMSDK に設定するか、E:/emsdk、C:/emsdk、~/emsdk のどれかに置いてください。\n"+
		"mitiru は emsdk を勝手には入れません。", cause)
}

func runDistWeb() error {
	cwd, _ := os.Getwd()
	manifestPath, projectRoot, err := config.FindManifest(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(manifestPath)
	if err != nil {
		return err
	}
	if cfg.Standalone() {
		return fmt.Errorf("%s は standalone のプロジェクト ([build] kind = \"standalone\") なので、mitiru dist --web は使えません。", cfg.Project.Name)
	}
	for _, note := range webIgnoredNotices(cfg) {
		fmt.Println(note)
	}

	engineRoot, err := engine.EnsureSource(cfg.EngineTag(), os.Stdout)
	if err != nil {
		return fmt.Errorf("エンジンのソースを用意できません。%w", err)
	}
	if err := build.CheckWebEngine(engineRoot); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	emsdk, err := build.FindEmsdk(build.EmsdkCandidates(os.Getenv, home))
	if err != nil {
		return emsdkMissingError(err)
	}
	mainCpp := filepath.Join(projectRoot, "src", "main.cpp")
	if !fileExists(mainCpp) {
		return fmt.Errorf("%s がありません。mitiru dist --web は src/main.cpp を 1 つの wasm にします。", mainCpp)
	}
	assets := filepath.Join(projectRoot, "assets")
	if !dirExists(assets) {
		assets = ""
	}

	extraSources, err := build.ExpandGameSources(projectRoot, mainCpp, cfg.Build.Sources)
	if err != nil {
		return err
	}
	if assets != "" {
		for _, note := range webMmlNotices(filepath.Join(assets, "audio")) {
			fmt.Println(note)
		}
	}

	cmakeText, err := build.RenderWebCMake(build.WebOptions{
		ProjectName: cfg.Project.Name,
		EngineRoot:  engineRoot,
		MainCpp:     mainCpp,
		AssetsDir:   assets,
		Title:       cfg.Window.Title,
		Width:       cfg.Window.Width,
		Height:      cfg.Window.Height,
		SingleFile:  distSingleFile,
		Sources:     extraSources,
	})
	if err != nil {
		return err
	}
	srcDir, outDir := build.WebDirs(projectRoot)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return fmt.Errorf("dist --web: %s を作れません: %w", srcDir, err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "CMakeLists.txt"), []byte(cmakeText), 0o644); err != nil {
		return fmt.Errorf("dist --web: CMakeLists.txt を書けません: %w", err)
	}

	target := build.WebTargetName(cfg.Project.Name)
	if !fileExists(filepath.Join(outDir, "CMakeCache.txt")) {
		fmt.Println("ブラウザ向けのエンジン全体を初めて組むので、数分かかります。")
	}
	if err := buildWebGame(emsdk, srcDir, outDir, target); err != nil {
		return err
	}

	bundle, err := filepath.Abs(filepath.Join(distOut, distBundleName(cfg.Project.Name)+"-web"))
	if err != nil {
		return fmt.Errorf("dist --web: 出力先を絶対パスにできません: %w", err)
	}
	n, err := installWebBundle(filepath.Join(outDir, target), bundle)
	if err != nil {
		return err
	}
	fmt.Printf("%d 個のファイルを %s に書きました。\n", n, bundle)

	if urls, uerr := build.CountNetworkURLs(bundle); uerr == nil && urls > 0 {
		fmt.Fprintf(os.Stderr, "注意: 配布物に http:// か https:// の文字列が %d 個あります。ネットワークの先を読んでいないか確かめてください。\n", urls)
	}
	if distZip {
		zipPath := bundle + ".zip"
		if err := zipDir(bundle, filepath.Dir(bundle), zipPath); err != nil {
			return fmt.Errorf("dist --web: zip を作れません: %w", err)
		}
		fmt.Printf("%s を書きました。\n", zipPath)
	}
	return nil
}

// buildWebGame は emsdk の環境を通して、web 用の CMake を configure し、target を組む。
// cmake と ninja は VS 同梱のものを使うので、vcvars64 も通す (コンパイラは em++ に替わる)。
func buildWebGame(emsdk, srcDir, outDir, target string) error {
	vcvars, err := build.FindVcvars64()
	if err != nil {
		return err
	}
	body := msvcPrelude(vcvars) + webBuildScript(emsdk, srcDir, outDir, target)
	if err := runMsvcScript("mitiru_web", body); err != nil {
		return fmt.Errorf("ブラウザ向けのビルドに失敗しました (%w)。上の出力を確かめてください。"+
			"ゲームが std::thread や Windows.h を使っていると wasm では組めません (docs/adr/0163-web-dist-static-link.md)。", err)
	}
	return nil
}

// webBuildScript は emsdk_env.bat と、configure と build の 2 行を書いた batch の本文。
func webBuildScript(emsdk, srcDir, outDir, target string) string {
	win := func(p string) string { return strings.ReplaceAll(p, "/", `\`) }
	return fmt.Sprintf(
		"call \"%s\" >NUL 2>&1\r\n"+
			"if errorlevel 1 exit /b %%errorlevel%%\r\n"+
			"cmake -S \"%s\" -B \"%s\" -G Ninja -DCMAKE_TOOLCHAIN_FILE=\"%s\" -DCMAKE_BUILD_TYPE=Release\r\n"+
			"if errorlevel 1 exit /b %%errorlevel%%\r\n"+
			"cmake --build \"%s\" --target %s\r\n",
		win(filepath.Join(emsdk, "emsdk_env.bat")),
		win(srcDir), win(outDir), build.EmsdkToolchainFile(emsdk), win(outDir), target)
}

// installWebBundle は組んだ target の出力フォルダを、配布先に作り直して写す。写したファイル数を返す。
func installWebBundle(builtDir, bundle string) (int, error) {
	entries, err := os.ReadDir(builtDir)
	if err != nil {
		return 0, fmt.Errorf("dist --web: 組んだ物が %s に見つかりません: %w", builtDir, err)
	}
	if !fileExists(filepath.Join(builtDir, "index.html")) {
		return 0, fmt.Errorf("dist --web: %s に index.html がありません。ビルドの出力を確かめてください。", builtDir)
	}
	if err := os.RemoveAll(bundle); err != nil {
		return 0, fmt.Errorf("dist --web: %s を空にできません: %w", bundle, err)
	}
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		return 0, fmt.Errorf("dist --web: %s を作れません: %w", bundle, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := copyFile(filepath.Join(builtDir, name), filepath.Join(bundle, name)); err != nil {
			return 0, fmt.Errorf("dist --web: %s を写せません: %w", name, err)
		}
	}
	return len(names), nil
}

// configureWebFlags は dist のコマンドに --web と --single-file を足す。
func configureWebFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&distWeb, "web", false,
		"build for the browser (WebAssembly) into <out>/<name>-web/: index.html, index.js, index.wasm and the assets")
	cmd.Flags().BoolVar(&distSingleFile, "single-file", false,
		"with --web: embed the wasm and the assets in a single index.html (small games; may exceed a site's size limit)")
}
