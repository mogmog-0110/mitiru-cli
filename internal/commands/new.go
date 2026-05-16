package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

Example:
  mitiru new myGame              create ./myGame/ from the 'hello' template
  mitiru new myGame -t hello     same as above
  mitiru new myGame --force      overwrite an existing directory`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(args[0])
		},
	}

	cmd.Flags().StringVarP(&newTemplateName, "template", "t", "hello",
		"template to use (currently only 'hello')")
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
		ProjectName:      name,
		GameClassName:    toGameClassName(name),
		ScenarioUrlMacro: toMacroName(name),
	}

	if err := scaffold.Expand(newTemplateName, dstDir, data); err != nil {
		return fmt.Errorf("new: expand template: %w", err)
	}

	fmt.Printf("Created %s\n\n", dstDir)
	fmt.Println("Next:")
	fmt.Printf("  cd %s\n", name)
	fmt.Println("  mitiru run")
	return nil
}

// toGameClassName turns "my-game" / "my_game" / "myGame" into "MyGameGame",
// or "myApp" into "MyAppGame". If the project name already ends in "Game"
// the suffix is dropped to avoid "MyGameGame"-style stutter.
func toGameClassName(name string) string {
	camel := toPascalCase(name)
	if strings.HasSuffix(camel, "Game") {
		return camel
	}
	return camel + "Game"
}

// toMacroName turns "my-game" / "myGame" into "MY_GAME_SCENE_URL" — a
// macro-safe identifier suitable for an `#ifndef` guard.
func toMacroName(name string) string {
	return toUpperSnake(name) + "_SCENE_URL"
}

func toPascalCase(s string) string {
	out := make([]rune, 0, len(s))
	upperNext := true
	for _, r := range s {
		switch {
		case r == '-' || r == '_':
			upperNext = true
		case upperNext:
			if r >= 'a' && r <= 'z' {
				r -= 32
			}
			out = append(out, r)
			upperNext = false
		default:
			out = append(out, r)
		}
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
