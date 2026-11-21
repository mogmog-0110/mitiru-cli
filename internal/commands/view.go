package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

// viewDefaultFrames は --capture-dir だけを指したときに回すフレーム数 (host の既定と同じ)。
const viewDefaultFrames = 120

type viewOptions struct {
	Models     []string // 絶対パス
	Clip       string
	Toon       bool
	Check      bool
	CaptureDir string // 絶対パス。空ならウィンドウで見る
	Frames     int
}

func newViewCommand() *cobra.Command {
	var opts viewOptions
	var captureDir string

	cmd := &cobra.Command{
		Use:   "view <model.glb>...",
		Short: "Look at skinned glb models and play a clip, without writing a game DLL",
		Long: `Put one or more .glb / .gltf models side by side, play a clip, and optionally
capture it. mitiru_host prints a table first (size, meshes, vertices, bone
count, clip names and lengths) and warns about things that touch the skin
limits: more than 256 bones, JOINTS_1 and up, vertices whose weights do not
sum to 1, bone indices out of range, no clips, and glbs whose heights differ
by 10x or more.

This runs the engine's own mitiru_host and apps/mitiru_view (engine 0.54.0 or
newer), not the project's game host, so it works inside or outside a project.
Inside a project it uses the engine pinned in mitiru.toml; outside one, the
latest release (or --engine-root). The first run builds mitiru_host from the
engine source, which takes several minutes.

  mitiru view fox.glb                       open a window and loop the first clip
  mitiru view a.glb b.glb --clip Walk       play Walk on both
  mitiru view fox.glb --toon                toon shading
  mitiru view fox.glb --check               print the table and warnings, no window, no GPU
  mitiru view fox.glb --capture-dir shots   headless DX12 capture of 120 frames into shots/

--check exits 0 when nothing is wrong, 1 when there are warnings, 2 when a
model cannot be read or --clip names a clip that does not exist.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			models, err := resolveViewModels(args)
			if err != nil {
				return err
			}
			opts.Models = models
			if captureDir != "" {
				abs, err := filepath.Abs(captureDir)
				if err != nil {
					return fmt.Errorf("view: --capture-dir %q の場所が分かりません: %w", captureDir, err)
				}
				opts.CaptureDir = abs
			}
			if err := validateViewOptions(opts); err != nil {
				return err
			}
			return runView(opts)
		},
	}
	cmd.Flags().StringVar(&opts.Clip, "clip", "", "clip name to play (default: the first clip of each model)")
	cmd.Flags().BoolVar(&opts.Toon, "toon", false, "toon shading")
	cmd.Flags().BoolVar(&opts.Check, "check", false, "print the table and warnings, then stop (no window, no GPU)")
	cmd.Flags().StringVar(&captureDir, "capture-dir", "", "capture PNGs here, headless on DX12 (no window)")
	cmd.Flags().IntVar(&opts.Frames, "frames", viewDefaultFrames, "frames to run with --capture-dir")
	return cmd
}

// resolveViewModels は引数の glb / gltf を絶対パスにして、あるかを確かめる。
// host は起動時に cwd を exe のフォルダへ変えるので、相対パスは渡せない。
func resolveViewModels(args []string) ([]string, error) {
	models := make([]string, 0, len(args))
	for _, a := range args {
		ext := strings.ToLower(filepath.Ext(a))
		if ext != ".glb" && ext != ".gltf" {
			return nil, fmt.Errorf("view: %s は glb でも gltf でもありません。.glb か .gltf を渡してください", a)
		}
		abs, err := filepath.Abs(a)
		if err != nil {
			return nil, fmt.Errorf("view: %s の場所が分かりません: %w", a, err)
		}
		if !fileExists(abs) {
			return nil, fmt.Errorf("view: %s がありません", abs)
		}
		models = append(models, abs)
	}
	return models, nil
}

func validateViewOptions(o viewOptions) error {
	if o.Check && o.CaptureDir != "" {
		return fmt.Errorf("view: --check は表だけを出して止まるので、--capture-dir とは一緒に使えません")
	}
	if o.Frames < 1 {
		return fmt.Errorf("view: --frames は 1 以上にしてください")
	}
	return nil
}

// viewHostArgs は mitiru_host へ渡す引数。glb を位置引数で渡すと host が mitiru_view の DLL を選ぶ。
func viewHostArgs(o viewOptions) []string {
	args := append([]string{}, o.Models...)
	if o.Clip != "" {
		args = append(args, "--clip", o.Clip)
	}
	if o.Toon {
		args = append(args, "--toon")
	}
	if o.Check {
		args = append(args, "--view-check")
	}
	if o.CaptureDir != "" {
		frames := o.Frames
		if frames < 1 {
			frames = viewDefaultFrames
		}
		args = append(args, "--headless-3d", "--backend", "dx12",
			"--capture-dir", o.CaptureDir, "--max-frames", strconv.Itoa(frames))
	}
	return args
}

// interpretViewExit は host の終了コードを CLI の結果にする。
// 0 = 成功。--check の 1 (警告あり) と 2 (読めない) は host が表と理由を出し終えているので、
// 終了コードだけを伝える。それ以外は host が落ちたとみなす。
func interpretViewExit(code int, check bool, exe string) error {
	switch {
	case code == 0:
		return nil
	case check && (code == 1 || code == 2):
		return &ExitCodeError{Code: code}
	case code == 2:
		return &ExitCodeError{Code: 2}
	default:
		return fmt.Errorf("%s", hostExitMessage(filepath.Base(exe), code))
	}
}

func runView(opts viewOptions) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru view は今のところ Windows でだけ動きます (今の OS は %s です)。", runtime.GOOS)
	}
	engineRoot, err := viewEngineRoot(os.Stderr)
	if err != nil {
		return err
	}
	hostExe, err := findViewHost(engineRoot)
	if err != nil {
		return err
	}
	if opts.CaptureDir != "" {
		if err := os.MkdirAll(opts.CaptureDir, 0o755); err != nil {
			return fmt.Errorf("view: %s を作れません: %w", opts.CaptureDir, err)
		}
	}

	args := viewHostArgs(opts)
	console.Verbosef("Running %s %s\n", filepath.Base(hostExe), strings.Join(args, " "))
	cmd := exec.Command(hostExe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = filepath.Dir(hostExe)
	cmd.Env = build.HostEnv()
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return interpretViewExit(exitErr.ExitCode(), opts.Check, hostExe)
		}
		return fmt.Errorf("%s を起動できません (%w)。", hostExe, err)
	}
	if opts.CaptureDir != "" {
		shots, _ := filepath.Glob(filepath.Join(opts.CaptureDir, "frame_*.png"))
		fmt.Printf("%d 枚を %s に書きました。\n", len(shots), opts.CaptureDir)
	}
	return nil
}

// viewEngineRoot は view が使うエンジンの場所。プロジェクトの中なら mitiru.toml の pin、外なら最新。
// --engine-root と MITIRU_ENGINE_ROOT は EnsureSource がどちらの場合でも先に見る。
func viewEngineRoot(progress io.Writer) (string, error) {
	version := "latest"
	inProject := false
	if cwd, err := os.Getwd(); err == nil {
		if manifestPath, _, ferr := config.FindManifest(cwd); ferr == nil {
			cfg, lerr := config.Load(manifestPath)
			if lerr != nil {
				return "", lerr
			}
			inProject = true
			if tag := cfg.EngineTag(); tag != "" {
				version = tag
			}
		}
	}
	// プロジェクトの外には比べる pin が無い。engine を上書きしているときの「pin と違う」知らせは出さない
	if _, _, overridden := engine.OverrideRoot(); overridden && !inProject {
		progress = io.Discard
	}
	root, err := engine.EnsureSource(version, progress)
	if err != nil {
		return "", fmt.Errorf("エンジンのソースを用意できません。%w", err)
	}
	return root, nil
}

// viewHostCandidates は view に使える host の置き場所。mitiru_view の DLL が host の隣に
// 無い物は、view を含めずに組んだ古い host なので数えない。
func viewHostCandidates(engineRoot string) []string {
	var found []string
	for _, c := range engineExeCandidates(engineRoot, "mitiru_host", "mitiru_host.exe") {
		if !fileExists(c) {
			continue
		}
		if fileExists(filepath.Join(filepath.Dir(c), "mitiru_view", "mitiru_view.dll")) {
			found = append(found, c)
		}
	}
	return found
}

// findViewHost はエンジン自身の build tree から mitiru_host (と、host が依存する mitiru_view) を探し、
// 無ければ組む。ゲームの host は使わない: mitiru-cli が生成する host の CMake は mitiru_view を組まない。
func findViewHost(engineRoot string) (string, error) {
	if !dirExists(filepath.Join(engineRoot, "apps", "mitiru_view")) {
		return "", fmt.Errorf("このエンジン (%s) には apps/mitiru_view がありません。mitiru view は 0.54.0 以降のエンジンが要ります。"+
			"mitiru.toml の engine を上げる (mitiru update) か、--engine-root で新しいエンジンの場所を指してください。", engineRoot)
	}
	if found := viewHostCandidates(engineRoot); len(found) > 0 {
		return found[0], nil
	}

	fmt.Fprintf(os.Stderr, "mitiru_host と mitiru_view をエンジンからビルドしています。初回だけ数分かかります。\n")
	if err := ensureEngineConfigured(engineRoot); err != nil {
		return "", err
	}
	if err := buildEngineTarget(filepath.Join(engineRoot, "build"), "mitiru_host"); err != nil {
		return "", err
	}
	if found := viewHostCandidates(engineRoot); len(found) > 0 {
		return found[0], nil
	}
	return "", fmt.Errorf("mitiru_host をビルドしましたが、その隣に mitiru_view/mitiru_view.dll が見つかりません (%s)。",
		filepath.Join(engineRoot, "build", "apps", "mitiru_host"))
}
