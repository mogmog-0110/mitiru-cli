package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/spf13/cobra"
)

// uiDocRel は host が DLL の隣で探す UI 文書 (RmlUi)。
const uiDocRel = "assets/ui/main.rml"

// legacySceneRel は CEF 世代の HTML の HUD。今の engine は読まない。
const legacySceneRel = "assets/scene.html"

func newUICommand() *cobra.Command {
	var frames int
	var outPath string
	var inputScript string

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Capture the game with its RML UI to a PNG, without opening a window",
		Long: `Build the project, run it headless on the GPU for a number of frames, and
save the last frame (the C++ drawing with assets/ui/main.rml on top) as a PNG.
The UI shows the values the game pushes with hud.set, so what you see is what
the player sees.

  mitiru ui                            capture after 90 frames to build/ui.png
  mitiru ui --frames 300               let the game run longer first
  mitiru ui --input-script clicks.txt  drive the UI with scripted clicks
  mitiru ui --out shot.png             choose the output file

While editing the RML, ` + "`mitiru watch`" + ` is faster: saving main.rml or a .rcss
reloads the document in the running game.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(frames, outPath, inputScript)
		},
	}

	cmd.Flags().IntVar(&frames, "frames", 90, "frames to run before the capture")
	cmd.Flags().StringVar(&outPath, "out", "", "output PNG (default build/ui.png)")
	cmd.Flags().StringVar(&inputScript, "input-script", "", "input script to replay before the capture")
	return cmd
}

func runUI(frames int, outPath, inputScript string) error {
	if frames < 1 {
		return fmt.Errorf("ui: --frames must be at least 1")
	}
	scriptAbs, err := absOrEmpty(inputScript)
	if err != nil {
		return fmt.Errorf("ui: resolve --input-script %q: %w", inputScript, err)
	}

	result, err := runBuild()
	if err != nil {
		return err
	}
	if err := checkUIDocument(result.ProjectRoot); err != nil {
		return err
	}
	if outPath == "" {
		outPath = filepath.Join(result.ProjectRoot, "build", "ui.png")
	}
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return fmt.Errorf("ui: resolve --out %q: %w", outPath, err)
	}

	captureDir, err := os.MkdirTemp("", "mitiru_ui_")
	if err != nil {
		return fmt.Errorf("ui: create capture dir: %w", err)
	}
	defer os.RemoveAll(captureDir)

	// host が撮った後に異常終了しても、撮れた絵は残してから終了の失敗を返す (絵と失敗の両方を見せる)。
	// frames 経った絵が撮れるまで、2 フレーム余分に回す
	runErr := runHostCapture(result.Artifacts, captureDir, frames, frames+2, scriptAbs)
	shot, err := lastCapture(captureDir)
	if err != nil {
		if runErr != nil {
			return runErr
		}
		return err
	}
	if err := copyFile(shot, outAbs); err != nil {
		return fmt.Errorf("ui: write %s: %w", outAbs, err)
	}
	fmt.Printf("%s に保存しました。\n", outAbs)
	if runErr != nil {
		return fmt.Errorf("%w (保存した画像は host が止まる前に撮ったものです)", runErr)
	}
	return nil
}

func absOrEmpty(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	return filepath.Abs(p)
}

// runHostCapture は、ビルドした host (構成ごとの build.OutDir の、このプロジェクトのもの) を captureHostArgs で回す。
func runHostCapture(art *build.Artifacts, captureDir string, every, maxFrames int, inputScript string) error {
	c := exec.Command(art.HostExePath, captureHostArgs(art.DllRel, captureDir, every, maxFrames, inputScript)...)
	c.Dir = art.DeployDir
	c.Env = build.HostEnv()
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d = %s",
				filepath.Base(art.HostExePath), exitErr.ExitCode(), hostExitHint(exitErr.ExitCode()))
		}
		return fmt.Errorf("run %s: %w", filepath.Base(art.HostExePath), err)
	}
	return nil
}

// checkUIDocument は UI 文書が無いときに、何を置けばよいかを言う。HTML の HUD だけが
// 残っているプロジェクトには移し方を示す (撮っても UI が写らないため)。
func checkUIDocument(projectRoot string) error {
	if fileExists(filepath.Join(projectRoot, filepath.FromSlash(uiDocRel))) {
		return nil
	}
	if fileExists(filepath.Join(projectRoot, filepath.FromSlash(legacySceneRel))) {
		return fmt.Errorf("ui: %s is no longer read by the engine; rewrite it as %s "+
			"(engine docs/UI_RMLUI.md explains how to move an HTML HUD)", legacySceneRel, uiDocRel)
	}
	return fmt.Errorf("ui: %s not found — the game has no UI layer to capture", uiDocRel)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// lastCapture は host が --capture-dir に書いた連番 PNG の最後の 1 枚を返す。
func lastCapture(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "frame_*.png"))
	if err != nil {
		return "", fmt.Errorf("ui: list captures: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("ui: the host wrote no PNG to %s (did it exit before the first capture?)", dir)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}
