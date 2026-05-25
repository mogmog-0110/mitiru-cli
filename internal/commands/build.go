package commands

import (
	"fmt"
	"os"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var (
	buildRelease    bool
	buildConfigName string
	buildGenerator  string
)

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the current project",
		Long: `Build the project in the current directory.

Looks for mitiru.toml in the current directory (or any parent), fetches the
requested MitiruEngine source into ~/.mitiru/cache/ if needed, generates a
CMakeLists.txt under build/cmake/, then invokes cmake configure + build.

Examples:
  mitiru build              # Debug build (default)
  mitiru build --release    # Release build`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runBuild()
			return err
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
}

func runBuild() (*buildResult, error) {
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

	engineRoot, err := engine.EnsureSource(cfg.EngineTag(), os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("fetch engine source: %w", err)
	}

	cfgName := resolveBuildConfig()
	opts := build.Options{
		ProjectRoot: projectRoot,
		ProjectName: cfg.Project.Name,
		EngineRoot:  engineRoot,
		Config:      cfgName,
		Generator:   buildGenerator,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	artifacts, err := build.Run(opts)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Build OK: %s\n", artifacts.DllPath)
	return &buildResult{
		ProjectRoot: projectRoot,
		Config:      cfg,
		Artifacts:   artifacts,
	}, nil
}

func resolveBuildConfig() string {
	if buildConfigName != "" {
		return buildConfigName
	}
	if buildRelease {
		return "Release"
	}
	return "Debug"
}
