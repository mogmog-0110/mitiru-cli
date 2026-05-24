package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
mitiru_host launcher — see ADR 0005 for the host/game contract.

Example:
  mitiru new myGame             create ./myGame/ from the 'hello' template
  mitiru new myGame --force     overwrite an existing directory`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(args[0])
		},
	}

	cmd.Flags().StringVarP(&newTemplateName, "template", "t", "hello",
		"template to use (only 'hello' is shipped today)")
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
		ProjectName:  name,
		ProjectIdent: toLowerSnake(name),
		UpperIdent:   toUpperSnake(name),
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
	fmt.Println("  mitiru run --inspect       also open the sub-window inspector")
	fmt.Println("")
	fmt.Println("Stuck? Run 'mitiru doctor' to verify your toolchain.")
	return nil
}

// toLowerSnake turns "my-game" / "myGame" / "My_Game" into "my_game" — a
// C++-safe identifier for use as a namespace.
func toLowerSnake(s string) string {
	upper := toUpperSnake(s)
	out := make([]rune, 0, len(upper))
	for _, r := range upper {
		if r >= 'A' && r <= 'Z' {
			r += 32
		}
		out = append(out, r)
	}
	return string(out)
}

func toUpperSnake(s string) string {
	out := make([]rune, 0, len(s)*2)
	prevWasUpper := true
	for i, r := range s {
		isUpper := r >= 'A' && r <= 'Z'
		switch {
		case r == '-' || r == '_':
			if len(out) > 0 && out[len(out)-1] != '_' {
				out = append(out, '_')
			}
			prevWasUpper = true
		case isUpper:
			if i > 0 && !prevWasUpper && len(out) > 0 && out[len(out)-1] != '_' {
				out = append(out, '_')
			}
			out = append(out, r)
			prevWasUpper = true
		default:
			if r >= 'a' && r <= 'z' {
				r -= 32
			}
			out = append(out, r)
			prevWasUpper = false
		}
	}
	return string(out)
}
