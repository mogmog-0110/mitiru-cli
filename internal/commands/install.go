//go:build windows

package commands

import (
	"github.com/mogmog-0110/mitiru-cli/internal/install"
	"github.com/spf13/cobra"
)

// install command wraps the standalone MitiruEngine_Installer.exe logic so
// users who already have mitiru on PATH can run `mitiru install` (or
// `mitiru install --force` to repair) without locating the installer .exe in
// the release zip. See docs/INSTALLER.md.
func newInstallCommand() *cobra.Command {
	var opts install.Options

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Bootstrap a fresh machine (MSVC + mitiru.exe + PATH + pre-cache)",
		Long: `'mitiru install' runs the same bootstrap as the standalone
MitiruEngine_Installer.exe ships in the release zip: install MSVC Build
Tools 2022 via winget, deploy mitiru.exe, append PATH, pre-cache the
engine source, and (best-effort) enable Windows LongPaths.

Detected components are skipped by default. Use --force to override the
"already installed" heuristics (useful for repair installs).

Examples:
  mitiru install                            # full bootstrap, skipping anything detected
  mitiru install --dry-run                  # show the plan, change nothing
  mitiru install --force                    # repair install (re-runs everything)
  mitiru install --skip-deploy --skip-pathenv  # only set up MSVC + pre-cache

Spec: https://github.com/mogmog-0110/MitiruEngine/blob/main/docs/INSTALLER.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return install.Run(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false,
		"print the plan, change nothing")
	cmd.Flags().StringVar(&opts.TargetDir, "target-dir", "",
		"where to install mitiru.exe (default: %LOCALAPPDATA%\\Programs\\MitiruEngine\\bin)")
	cmd.Flags().BoolVar(&opts.Force, "force", false,
		"override 'already installed' skip detection (repair install)")
	cmd.Flags().BoolVar(&opts.SkipWinget, "skip-winget", false,
		"don't invoke winget (MSVC install)")
	cmd.Flags().BoolVar(&opts.SkipDeploy, "skip-deploy", false,
		"don't copy mitiru.exe into target-dir")
	cmd.Flags().BoolVar(&opts.SkipPathEnv, "skip-pathenv", false,
		"don't append target-dir to HKCU\\Environment\\Path")
	cmd.Flags().BoolVar(&opts.SkipPrecache, "skip-precache", false,
		"don't pre-download the engine source tarball")
	cmd.Flags().BoolVar(&opts.SkipLongPaths, "skip-longpaths", false,
		"don't touch HKLM\\...\\LongPathsEnabled")
	cmd.Flags().BoolVar(&opts.AssumeYes, "yes", false,
		"skip the consent prompt (CI / automation)")
	cmd.Flags().BoolVarP(&opts.AssumeYes, "y", "y", false,
		"alias for --yes")

	return cmd
}
