package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	replayRecordFile string
	replayPlayFile   string
	replayTestFile   string
	replayExpectFile string
	replayGame       bool
)

func newReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Record, play back, or regression-test an input replay (deterministic)",
		Long: `Runs the engine's replay subsystem in isolation. Part of
MitiruEngine's per-system isolation and the deterministic-replay axis:
the recorded InputSnapshot stream reproduces a session bit-for-bit.

Provide exactly one of:
  --record <file>   record this session to <file>
  --replay <file>   play back a previously recorded <file>
  --test   <file>   headless regression test (no window, no CEF)
                    prints final-state JSON to stdout and exits 0 on success.
                    Combine with --expect <json> to diff against a known baseline.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplay()
		},
	}
	cmd.Flags().StringVar(&replayRecordFile, "record", "", "record a session to <file>")
	cmd.Flags().StringVar(&replayPlayFile, "replay", "", "play back <file>")
	cmd.Flags().StringVar(&replayTestFile, "test", "", "headless regression test against <file>")
	cmd.Flags().StringVar(&replayExpectFile, "expect", "", "expected final-state JSON for --test comparison")
	cmd.Flags().BoolVar(&replayGame, "game", false,
		"replay THIS project's game (build + headless host) instead of the standalone demo subsystem")
	return cmd
}

func runReplay() error {
	record := replayRecordFile != ""
	play := replayPlayFile != ""
	test := replayTestFile != ""

	modeCount := 0
	if record {
		modeCount++
	}
	if play {
		modeCount++
	}
	if test {
		modeCount++
	}

	if modeCount > 1 {
		return fmt.Errorf("replay: --record, --replay, and --test are mutually exclusive; pass exactly one")
	}
	if modeCount == 0 {
		return fmt.Errorf("replay: pass exactly one of --record <file>, --replay <file>, or --test <file>")
	}

	if replayExpectFile != "" && !test {
		return fmt.Errorf("replay: --expect requires --test")
	}
	if replayGame && !test {
		return fmt.Errorf("replay: --game supports only --test; to record a game session use `mitiru run --record <file>`")
	}

	if record {
		abs, err := filepath.Abs(replayRecordFile)
		if err != nil {
			return fmt.Errorf("replay: resolve %q: %w", replayRecordFile, err)
		}
		return launchSubsystem("replay", "--record", abs)
	}

	if play {
		abs, err := filepath.Abs(replayPlayFile)
		if err != nil {
			return fmt.Errorf("replay: resolve %q: %w", replayPlayFile, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return fmt.Errorf("replay: %s: %w", abs, err)
		}
		return launchSubsystem("replay", "--replay", abs)
	}

	// --test モード: headless、window なし、CEF なし。
	abs, err := filepath.Abs(replayTestFile)
	if err != nil {
		return fmt.Errorf("replay: resolve %q: %w", replayTestFile, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("replay: %s: %w", abs, err)
	}

	// --game: standalone な demo subsystem ではなく、このプロジェクトの DLL を host
	// 経由で replay する (実ゲーム)。プロジェクトを build してから host を headless 実行。
	if replayGame {
		return runReplayGameTest(abs)
	}

	subsysArgs := []string{"--test", abs}
	if replayExpectFile != "" {
		absExpect, err := filepath.Abs(replayExpectFile)
		if err != nil {
			return fmt.Errorf("replay: resolve expect %q: %w", replayExpectFile, err)
		}
		if _, err := os.Stat(absExpect); err != nil {
			return fmt.Errorf("replay: expect %s: %w", absExpect, err)
		}
		subsysArgs = append(subsysArgs, "--expect", absExpect)
	}

	return launchSubsystem("replay", subsysArgs...)
}

// runReplayGameTest は現在のプロジェクトを build し、記録した session をプロジェクトの
// host DLL 経由で headless に replay する (`mitiru_host <dll> --replay-test`)。--expect
// 指定時は最終 push された view.* state を assert する。host の exit code はそのまま
// 伝播するので CI が regression 結果を見られる。
func runReplayGameTest(absFile string) error {
	result, err := runBuild()
	if err != nil {
		return err
	}
	art := result.Artifacts

	hostArgs := []string{art.DllRel, "--replay-test", absFile}
	if replayExpectFile != "" {
		absExpect, err := filepath.Abs(replayExpectFile)
		if err != nil {
			return fmt.Errorf("replay: resolve expect %q: %w", replayExpectFile, err)
		}
		if _, err := os.Stat(absExpect); err != nil {
			return fmt.Errorf("replay: expect %s: %w", absExpect, err)
		}
		hostArgs = append(hostArgs, "--expect", absExpect)
	}

	cmd := exec.Command(art.HostExePath, hostArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = art.DeployDir
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode()) // regression-gate の exit code を忠実に伝える
		}
		return fmt.Errorf("replay --game: %w", err)
	}
	return nil
}
