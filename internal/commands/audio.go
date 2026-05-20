package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var audioEngineTag = "latest"

func newAudioCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audio [file]",
		Short: "Launch the audio subsystem in isolation (axis 3)",
		Long: `Builds and runs the engine's standalone audio playground:
miniaudio initialised, no CEF, no ECS, no scene manager. Boots in under a
second. Optionally pass a .wav / .mp3 / .flac path to load on startup;
SPACE plays it, +/- adjusts master volume, ESC quits.

This is the axis 3 (per-system isolation) showcase tool — same pattern
as 'mitiru renderer', for the audio subsystem.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ""
			if len(args) == 1 {
				abs, err := filepath.Abs(args[0])
				if err != nil {
					return fmt.Errorf("audio: resolve %q: %w", args[0], err)
				}
				if _, err := os.Stat(abs); err != nil {
					return fmt.Errorf("audio: %s: %w", abs, err)
				}
				file = abs
			}
			return runAudio(file)
		},
	}
	cmd.Flags().StringVar(&audioEngineTag, "engine", "latest",
		"engine version to build against (default 'latest'). Overridable via MITIRU_ENGINE_ROOT.")
	return cmd
}

func runAudio(filePath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("mitiru audio is currently Windows-only (running on %s)",
			runtime.GOOS)
	}

	engineRoot, err := engine.EnsureSource(audioEngineTag, os.Stdout)
	if err != nil {
		return fmt.Errorf("audio: fetch engine source: %w", err)
	}

	candidates := []string{
		filepath.Join(engineRoot, "build", "examples", "mitiru_audio", "mitiru_audio.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_audio", "Debug", "mitiru_audio.exe"),
		filepath.Join(engineRoot, "build", "examples", "mitiru_audio", "Release", "mitiru_audio.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			fmt.Printf("Running %s\n", c)
			return runAudioExe(c, filepath.Dir(c), filePath)
		}
	}

	// Slow path: build via engine's own cmake tree
	engineBuildDir := filepath.Join(engineRoot, "build")
	if _, err := os.Stat(filepath.Join(engineBuildDir, "CMakeCache.txt")); err != nil {
		return fmt.Errorf("audio: engine has not been configured yet; expected %s — run `cmake --preset default` from the engine root first",
			engineBuildDir)
	}
	if err := buildEngineTarget(engineBuildDir, "mitiru_audio"); err != nil {
		return err
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			fmt.Printf("Running %s\n", c)
			return runAudioExe(c, filepath.Dir(c), filePath)
		}
	}
	return fmt.Errorf("audio: build succeeded but mitiru_audio.exe was not found under %s", engineBuildDir)
}

func runAudioExe(exePath, workDir, fileArg string) error {
	args := []string{}
	if fileArg != "" {
		args = append(args, fileArg)
	}
	cmd := exec.Command(exePath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = workDir

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with status %d", exePath, exitErr.ExitCode())
		}
		return fmt.Errorf("audio: %w", err)
	}
	return nil
}
