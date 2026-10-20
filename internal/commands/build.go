package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"github.com/mogmog-0110/mitiru-cli/internal/deeppath"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var (
	buildRelease    bool
	buildConfigName string
	buildGenerator  string
	// dist が configure に注入する追加分 (run/build/watch では空のまま)。
	buildExtraDefines []string
	buildOutDir       string
)

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the current project",
		Long: `Build the project in the current directory.

Looks for mitiru.toml in the current directory (or any parent), fetches the
requested MitiruEngine source into ~/.mitiru/cache/ if needed, generates a
CMakeLists.txt under build/cmake/, then invokes cmake configure + build.
Configure runs only when its inputs changed (the generated CMakeLists.txt,
mitiru.toml, the engine, the compiler); -v prints how long each step took.

Parts of the engine that are not built by default are turned on in mitiru.toml:

  [engine]
  features = ["nav", "online"]

  nav       navmesh paths and crowds (Detour) in the game DLL
  navbake   bake navmeshes inside the game DLL (Recast)
  jolt      check that the engine was fetched with Jolt Physics
  online    online co-op in mitiru_host (--net, hud.net*; rollback via GekkoNet)
  dawn      WebGPU (Dawn) rendering layer; fetched on first use; left out of Debug builds
  onnx      NeuralStyle / NeuralDepth (ONNX Runtime; adds onnxruntime.dll and DirectML.dll to dist)
  directml  DirectML neural post-process in the DX12 frame (adds DirectML.dll to dist)
  effekseer .efkefc effects through drawModel (Effekseer)
  cubism    Live2D models (Cubism SDK)

Examples:
  mitiru build              # Debug build (default)
  mitiru build --release    # Release build (build/out-release; the Debug build in build/out is kept)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runBuildOnly()
			if err != nil {
				return err
			}
			// [build] bake = true (★4-1): 配置 JSON を焼いて起動時の再現性を上げる。
			// standalone は Spawner 前提が無いので対象外。
			if res.Config.Build.Bake && !res.Config.Standalone() {
				if err := runBakeAll(res.Artifacts, console.VerboseWriter(os.Stdout)); err != nil {
					return err
				}
			}
			// 成功時に出すのは、できたものの場所の 1 行だけにする。
			fmt.Printf("ビルドしました。%s にあります。\n", builtOutputPath(res.Artifacts))
			maybeNotifyUpdates(res.Config.Project.Engine, console.VerboseWriter(os.Stdout))
			return nil
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "build with Release configuration")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo); overrides --release")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (e.g. \"Visual Studio 17 2022\", \"NMake Makefiles\"); default is Ninja")
	return cmd
}

// buildResult はパース済み project と生成された artifact path をまとめる。
// これにより `mitiru run` / `mitiru watch` が再パースなしで `mitiru build`
// に連鎖できる。
type buildResult struct {
	ProjectRoot string
	Config      *config.ProjectConfig
	Artifacts   *build.Artifacts
	EngineRoot  string // host 型のときだけ。standalone では空
}

// runBuild は host 型の project を build する。standalone 型は断る: 呼ぶ側は
// mitiru_host と game DLL の layout (Artifacts の DLL 側) を前提にしている。
func runBuild() (*buildResult, error) {
	return runBuildTo(os.Stdout, os.Stderr)
}

// runAnyBuild は host 型も standalone 型も build する。`mitiru build` と
// `mitiru run` だけがこちらを使う。
func runAnyBuild() (*buildResult, error) {
	return buildProject(os.Stdout, os.Stderr, true, true)
}

// runBuildOnly は組むだけで起動しない `mitiru build` 用。深すぎる置き場は起動できないので、
// 止めずに知らせるだけにする (組んだ物を別の場所へ移して使える)。
func runBuildOnly() (*buildResult, error) {
	return buildProject(os.Stdout, os.Stderr, true, false)
}

// runBuildTo は build の進捗 / cmake 出力を任意の writer に流す。watch は
// io.MultiWriter で console + buffer に tee し、失敗時に buffer からエラー行を
// 抽出してエラーファイルへ書く (ビルドエラーのゲーム画面表示)。
func runBuildTo(stdout, stderr io.Writer) (*buildResult, error) {
	return buildProject(stdout, stderr, false, true)
}

// buildProject の launch は、組んだ host をこの後で起動する呼び出しか。起動する呼び出しは、
// 置き場が GPU のドライバーを壊すほど深いとき、組む前に止める (internal/deeppath)。
func buildProject(stdout, stderr io.Writer, allowStandalone, launch bool) (*buildResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}

	manifestPath, projectRoot, err := config.FindManifest(cwd)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(manifestPath)
	if err != nil {
		return nil, err
	}

	if cfg.Standalone() {
		if !allowStandalone {
			return nil, fmt.Errorf(
				"%s は standalone のプロジェクト ([build] kind = \"standalone\") なので、使えるのは mitiru build と mitiru run だけです。",
				cfg.Project.Name)
		}
		artifacts, err := build.RunStandalone(build.StandaloneOptions{
			ProjectRoot: projectRoot,
			SourceDir:   filepath.Join(projectRoot, filepath.FromSlash(cfg.Build.Source)),
			Target:      cfg.Build.Target,
			Config:      resolveBuildConfig(cfg),
			Generator:   buildGenerator,
			Cache:       cfg.Build.Cache,
			Stdout:      stdout,
			Stderr:      stderr,
		})
		if err != nil {
			return nil, err
		}
		if err := checkHostPlace(artifacts.HostExePath, launch, stderr); err != nil {
			return nil, err
		}
		console.Fverbosef(stdout, "Build OK: %s\n", artifacts.HostExePath)
		return &buildResult{ProjectRoot: projectRoot, Config: cfg, Artifacts: artifacts}, nil
	}

	engineRoot, err := engine.EnsureSource(cfg.EngineTag(), stdout)
	if err != nil {
		return nil, fmt.Errorf("エンジンのソースを用意できません。%w", err)
	}

	cfgName := resolveBuildConfig(cfg)
	features, dawnDir, err := resolveFeatures(cfg, cfgName, engineRoot, stdout)
	if err != nil {
		return nil, err
	}
	// 組む前に置き場を測る。深すぎる所で数十分組んでも、できた host は起動できない。
	// dist (buildOutDir あり) はこの host を起動せず、配布物の置き場を自分で測る。
	if buildOutDir == "" {
		predictedHost := filepath.Join(build.OutDir(projectRoot, cfgName), "Release", "mitiru_host.exe")
		if err := checkHostPlace(predictedHost, launch, stderr); err != nil {
			return nil, err
		}
	}
	opts := build.Options{
		ProjectRoot:     projectRoot,
		ProjectName:     cfg.Project.Name,
		EngineRoot:      engineRoot,
		Config:          cfgName,
		Generator:       buildGenerator,
		Cache:           cfg.Build.Cache,
		ExtraDefines:    buildExtraDefines, // dist が GUI subsystem のフラグを注入 (通常は空)
		OutDir:          buildOutDir,       // dist は別 out dir (通常は空 = 構成ごとの build.OutDir)
		Features:        features,
		DawnDir:         dawnDir,
		NavSource:       cfg.Nav.Source,
		NavArgs:         cfg.Nav.Args,
		LightingSources: cfg.Lighting.Source,
		LightingArgs:    cfg.Lighting.Args,
		Stdout:          stdout,
		Stderr:          stderr,
	}
	if envRoot, explicit, ok := engine.OverrideRoot(); ok && sameDir(envRoot, engineRoot) {
		opts.EngineFromEnv, opts.EngineSwitchExplicit = true, explicit
	}

	artifacts, err := build.Run(opts)
	if err != nil {
		return nil, err
	}

	if buildOutDir == "" {
		if err := checkHostPlace(artifacts.HostExePath, launch, stderr); err != nil {
			return nil, err
		}
	}
	console.Fverbosef(stdout, "Build OK: %s\n", artifacts.DllPath)
	return &buildResult{
		ProjectRoot: projectRoot,
		Config:      cfg,
		Artifacts:   artifacts,
		EngineRoot:  engineRoot,
	}, nil
}

// sameDir は 2 つのパスが同じ場所を指すかを、区切りと大文字小文字を無視して返す。
func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// checkHostPlace は host の置き場が深すぎないかを見る。launch なら error で止め、
// 組むだけなら警告にとどめる。
func checkHostPlace(hostExe string, launch bool, stderr io.Writer) error {
	err := deeppath.Check(hostExe)
	if err == nil || launch {
		return err
	}
	fmt.Fprintf(stderr, "警告: %v\n", err)
	return nil
}

// builtOutputPath は host 型なら game DLL、standalone 型なら exe を返す。
func builtOutputPath(art *build.Artifacts) string {
	if art.DllPath != "" {
		return art.DllPath
	}
	return art.HostExePath
}

// resolveBuildConfig は --config、--release、mitiru.toml の [build] config、Debug の順に決める。
func resolveBuildConfig(cfg *config.ProjectConfig) string {
	if buildConfigName != "" {
		return buildConfigName
	}
	if buildRelease {
		return "Release"
	}
	if cfg != nil && cfg.Build.Config != "" {
		return cfg.Build.Config
	}
	return "Debug"
}

// resolveFeatures は [engine] features を、この構成で組める形にして返す。
// Debug の構成では Dawn を外して 1 行だけ知らせる。Release 系で dawn が残っているときは
// 固定した版の Dawn を用意し、その場所を返す (環境変数も手作業も要らない)。
func resolveFeatures(cfg *config.ProjectConfig, buildConfig, engineRoot string, stdout io.Writer) (features []string, dawnDir string, err error) {
	features, dropped := config.DropForConfig(cfg.Engine.Features, buildConfig)
	if len(dropped) > 0 {
		fmt.Fprintf(stdout, "%s の構成なので、%s を外して組みます。Release と RelWithDebInfo では入ります。\n",
			buildConfig, strings.Join(dropped, ", "))
	}
	if !slices.ContainsFunc(features, func(n string) bool { return strings.EqualFold(strings.TrimSpace(n), "dawn") }) {
		return features, "", nil
	}
	dawnDir, err = engine.EnsureDawn(engineRoot, stdout)
	if errors.Is(err, engine.ErrNoDawnPin) {
		// Dawn の層を持たない engine。engine 自身の案内 (target が無い) に任せる
		return features, "", nil
	}
	return features, dawnDir, err
}
