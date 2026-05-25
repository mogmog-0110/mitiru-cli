package commands

import (
	"github.com/spf13/cobra"
)

const (
	cliName    = "mitiru"
	cliVersion = "0.1.0"

	// defaultEngineVersion is the engine release a freshly scaffolded project
	// pins in its mitiru.toml. Bump this on every engine release so new
	// projects build against an engine that has the features the templates
	// rely on (e.g. the zero-JS declarative binder, ADR 0007).
	defaultEngineVersion = "0.5.0"
)

func NewRootCommand() *cobra.Command {
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
  mitiru install         bootstrap MSVC + mitiru.exe + PATH (Windows)
  mitiru clean           remove build/ (--all also clears engine cache)
  mitiru doctor          check that prerequisites are installed
  mitiru version         print version`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newNewCommand())
	root.AddCommand(newBuildCommand())
	root.AddCommand(newRunCommand())
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
	root.AddCommand(newCleanCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newVersionCommand())

	return root
}
