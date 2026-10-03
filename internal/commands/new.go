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
		return fmt.Errorf("プロジェクト名 %q は使えません。英字で始め、英数字と _ と - だけで書いてください。", name)
	}

	dstDir, err := filepath.Abs(name)
	if err != nil {
		return fmt.Errorf("new: resolve target path: %w", err)
	}

	if _, err := os.Stat(dstDir); err == nil {
		if !newForce {
			return fmt.Errorf("%s はもうあります。上書きするなら --force を付けてください。", dstDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("new: stat %s: %w", dstDir, err)
	}

	data := scaffold.Data{
		ProjectName:   name,
		ProjectIdent:  toLowerSnake(name),
		UpperIdent:    toUpperSnake(name),
		TargetName:    build.TargetName(name),
		EngineVersion: defaultEngineVersion,
	}

	if err := scaffold.Expand(newTemplateName, dstDir, data); err != nil {
		return fmt.Errorf("テンプレートからプロジェクトを作れませんでした (%w)。", err)
	}

	fmt.Printf("%s を作りました。cd %s で移ってから、次のどれかを実行してください。\n\n", dstDir, name)
	fmt.Println("  mitiru run            ビルドして起動します。初回はエンジンのコンパイルに 5〜10 分かかり、2 回目からは数秒です。")
	fmt.Println("  mitiru watch          src/ を保存するたびにビルドし直し、動いているゲームに反映します。")
	fmt.Println("  mitiru run --inspect  ゲームと一緒に調べる窓を開きます。--inspect perf のように窓を選べます。")
	fmt.Println()
	fmt.Println("うまく動かないときは、mitiru doctor で必要な道具がそろっているか確かめてください。")
	return nil
}

// toLowerSnake は "my-game" / "myGame" / "My_Game" を "my_game" に変換する。
// namespace として使える C++-safe な identifier になる。
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
