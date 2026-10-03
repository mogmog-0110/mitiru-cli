package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/scaffold"
	"github.com/spf13/cobra"
)

var (
	newTemplateName string
	newForce        bool
)

var projectNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func newNewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new MitiruEngine project",
		Long: `Create a new MitiruEngine project from a template.

The scaffolded project is built as a SHARED library (DLL) and run via the
mitiru_host launcher.

Templates: welcome (C++ art + RML/RCSS UI, the default), hello (minimal),
clicker (incremental loop), shooter (vertical STG),
objects (classes + virtual components via MITIRU_GAME_OBJECTS; engine >= 0.33),
action3d (3D character + camera rig + one navmesh enemy; engine >= 0.35).

Example:
  mitiru new myGame                create ./myGame/ from the 'welcome' template
  mitiru new myGame -t clicker     start from the clicker template
  mitiru new myGame --force        overwrite an existing directory`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(args[0])
		},
	}

	cmd.Flags().StringVarP(&newTemplateName, "template", "t", "welcome",
		"template to use: welcome | hello | clicker | shooter | objects | action3d")
	cmd.Flags().BoolVar(&newForce, "force", false,
		"overwrite the target directory if it already exists")

	return cmd
}

func runNew(name string) error {
	if !projectNamePattern.MatchString(name) {
		return fmt.Errorf("new: invalid project name %q: must start with a letter and contain only A-Z, a-z, 0-9, '_', '-'", name)
	}

	dstDir, err := filepath.Abs(name)
	if err != nil {
		return fmt.Errorf("new: resolve target path: %w", err)
	}

	if _, err := os.Stat(dstDir); err == nil {
		if !newForce {
			return fmt.Errorf("new: %s already exists (use --force to overwrite)", dstDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("new: stat %s: %w", dstDir, err)
	}

	data := scaffold.Data{
		ProjectName:   name,
		TargetName:    build.TargetName(name),
		EngineVersion: defaultEngineVersion,
	}

	if err := scaffold.Expand(newTemplateName, dstDir, data); err != nil {
		return fmt.Errorf("new: expand template: %w", err)
	}

	fmt.Printf("Created %s\n\n", dstDir)
	fmt.Println("Next:")
	fmt.Printf("  cd %s\n\n", name)
	fmt.Println("Try one of:")
	fmt.Println("  mitiru run                 build + run (first build compiles the")
	fmt.Println("                             engine: ~5-10 min; seconds after that)")
	fmt.Println("  mitiru watch               auto-rebuild + hot-reload on src/ change")
	fmt.Println("  mitiru run --inspect       also open a tool window (--inspect perf, mixer, ...)")
	fmt.Println("")
	fmt.Println("Stuck? Run 'mitiru doctor' to verify your toolchain.")
	return nil
}
