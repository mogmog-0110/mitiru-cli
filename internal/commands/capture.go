package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

func newCaptureCommand() *cobra.Command {
	var frames, every int
	var outDir, script string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Run the game headless for some frames and save PNGs, without opening a window",
		Long: `Build the project, run this project's own mitiru_host (from build/out)
headless on DX12, and save a PNG every --every frames. With --script the game
plays an input script (engine docs/INPUT_SCRIPT.md), so the frames show what a
player would see.

  mitiru capture                          120 frames, a PNG every 30 → build/capture/
  mitiru capture --frames 600 --every 60  run longer
  mitiru capture --script play.txt        play scripted input while capturing
  mitiru capture --out shots              choose the output folder

For a single frame of the RML UI, ` + "`mitiru ui`" + ` is shorter.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapture(frames, every, outDir, script)
		},
	}
	cmd.Flags().IntVar(&frames, "frames", 120, "frames to run")
	cmd.Flags().IntVar(&every, "every", 30, "save a PNG every N frames")
	cmd.Flags().StringVar(&outDir, "out", "", "output folder (default build/capture)")
	cmd.Flags().StringVar(&script, "script", "", "input script to play while capturing")
	return cmd
}

func runCapture(frames, every int, outDir, script string) error {
	if frames < 1 || every < 1 {
		return fmt.Errorf("capture: --frames と --every は 1 以上にしてください")
	}
	scriptAbs, err := absOrEmpty(script)
	if err != nil {
		return fmt.Errorf("capture: --script %q の場所が分かりません: %w", script, err)
	}
	if scriptAbs != "" && !fileExists(scriptAbs) {
		return fmt.Errorf("capture: 入力の台本 %s がありません", scriptAbs)
	}

	result, err := runBuild()
	if err != nil {
		return err
	}
	if outDir == "" {
		outDir = filepath.Join(result.ProjectRoot, "build", "capture")
	}
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("capture: --out %q の場所が分かりません: %w", outDir, err)
	}
	if err := clearCaptures(outAbs); err != nil {
		return err
	}

	// host が途中で落ちても撮れた絵は残し、落ちた理由と一緒に見られるようにする
	runErr := runHostCapture(result.Artifacts, outAbs, every, frames+2, scriptAbs)
	shots, _ := filepath.Glob(filepath.Join(outAbs, "frame_*.png"))
	if len(shots) == 0 {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("capture: host が %s に PNG を 1 枚も書きませんでした", outAbs)
	}
	last, _ := lastCapture(outAbs)
	fmt.Printf("%d 枚を %s に書きました。最後の 1 枚は %s です。\n", len(shots), outAbs, filepath.Base(last))
	return runErr
}

// clearCaptures は出力先の frame_*.png だけを消す。残っていると、別の実行の絵が番号順に混ざる。
func clearCaptures(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("capture: %s を作れません: %w", dir, err)
	}
	old, err := filepath.Glob(filepath.Join(dir, "frame_*.png"))
	if err != nil {
		return err
	}
	for _, p := range old {
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("capture: %s を消せません: %w", p, err)
		}
	}
	return nil
}

// captureHostArgs は host を窓なしの DX12 で回して captureDir に撮らせる引数。RmlUi は DX12 の
// 描画先にしか重ならないので backend を固定する。host は 2 フレーム目に 1 枚目を撮り、以後 every ごとに撮る。
func captureHostArgs(dllRel, captureDir string, every, maxFrames int, inputScript string) []string {
	args := []string{dllRel, "--headless-3d", "--backend", "dx12",
		"--capture-dir", captureDir, "--capture-every", strconv.Itoa(every),
		"--max-frames", strconv.Itoa(maxFrames)}
	if inputScript != "" {
		args = append(args, "--input-script", inputScript)
	}
	return append(args, tomlHostArgs()...)
}
