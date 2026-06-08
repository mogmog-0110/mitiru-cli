package commands

import (
	"github.com/spf13/cobra"
)

const (
	cliName = "mitiru"

	// defaultEngineVersion は、新規 scaffold されたプロジェクトが mitiru.toml に
	// pin する engine release。engine release ごとにこれを bump し、新規プロジェクトが
	// template の依存する機能 (例 zero-JS declarative binder、ADR 0007) を持つ engine
	// に対して build されるようにする。
	defaultEngineVersion = "0.10.0"
)

// cliVersion は mitiru CLI 自身の版。goreleaser が release 時に ldflags
// (-X .../commands.cliVersion=<tag>) で上書きする。手元 build では既定値のまま。
// self-update がこの値と最新 release を比較する (ADR 0010)。
var cliVersion = "0.9.0"

func NewRootCommand() *cobra.Command {
	// 前回の self-update が残した <exe>.old を best-effort で掃除する (ADR 0010 #8)。
	cleanupStaleSelfUpdate()

	root := &cobra.Command{
		Use:   cliName,
		Short: "MitiruEngine project tool",
		Long: `mitiru — MitiruEngine project tool.

Manage MitiruEngine game projects without touching CMakeLists.txt:
  mitiru new <name>      create a new project
  mitiru build           build the current project
  mitiru run             build and run
  mitiru debug           build (Debug) and run with engine debug helpers
  mitiru watch           file watch + auto rebuild + auto relaunch
  mitiru renderer        launch the renderer subsystem standalone (axis 3)
  mitiru audio [file]    launch the audio subsystem standalone (axis 3)
  mitiru input           launch the input subsystem standalone (axis 3)
  mitiru scene           launch the scene subsystem standalone (axis 3)
  mitiru replay          record / play back an input replay (axis 4)
  mitiru ui [scene.html] preview HTML/CSS UI in the browser with mock state, no build needed
  mitiru inspect <pid>   open a sub-window inspector for a running game (axis 5)
  mitiru dist            build a redistributable folder (no-console launcher)
  mitiru lint            check project layout / manifest / HTML bindings
  mitiru install         bootstrap MSVC + mitiru.exe + PATH (Windows)
  mitiru update          bump this project's pinned engine version
  mitiru self-update     update the mitiru CLI binary itself
  mitiru clean           remove build/ (--all also clears engine cache)
  mitiru doctor          check that prerequisites are installed
  mitiru version         print version`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// 引数なしの `mitiru` は対話ランチャー (menu) を開く。コマンド名を覚えて
		// 手打ちしなくて済むように。非 TTY/パイプ時は EOF で即終了する。
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMenu()
		},
	}

	root.AddCommand(newMenuCommand())
	root.AddCommand(newNewCommand())
	root.AddCommand(newBuildCommand())
	root.AddCommand(newRunCommand())
	root.AddCommand(newDistCommand())
	root.AddCommand(newDebugCommand())
	root.AddCommand(newWatchCommand())
	root.AddCommand(newRendererCommand())
	root.AddCommand(newAudioCommand())
	root.AddCommand(newInputCommand())
	root.AddCommand(newSceneCommand())
	root.AddCommand(newReplayCommand())
	root.AddCommand(newUICommand())
	root.AddCommand(newInspectCommand())
	root.AddCommand(newInstallCommand())
	root.AddCommand(newUpdateCommand())
	root.AddCommand(newSelfUpdateCommand())
	root.AddCommand(newCleanCommand())
	root.AddCommand(newLintCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newVersionCommand())

	return root
}
