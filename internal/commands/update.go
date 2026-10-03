package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

// update はこのプロジェクトの engine pin (mitiru.toml の engine) を最新 release に
// 揃える。CLI binary 自体の更新は self-update (別関心事、ADR 0010)。

func newUpdateCommand() *cobra.Command {
	var checkOnly bool
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update this project's pinned engine version to the latest release",
		Long: `Resolves the latest MitiruEngine release (highest semver tag on the
public repo) and updates the 'engine' pin in mitiru.toml.

A minor/major bump in 0.x may include ABI-breaking changes (a full rebuild is
required); 'update' warns and asks before applying one. Comments in mitiru.toml
are preserved — only the engine line is rewritten.

  mitiru update            # check, show the delta, confirm, then bump + prefetch
  mitiru update --check    # report the available version only; change nothing
  mitiru update --yes      # apply without the confirmation prompt (automation)

If MITIRU_ENGINE_ROOT is set, builds use that local checkout and the pin is
cosmetic — 'update' still rewrites the pin but skips the download.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(checkOnly, assumeYes)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report the latest version only; change nothing")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "apply without the confirmation prompt")
	return cmd
}

func runUpdate(checkOnly, assumeYes bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	manifestPath, _, err := config.FindManifest(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(manifestPath)
	if err != nil {
		return err
	}

	cur, ok := engine.ParseSemver(cfg.Project.Engine)
	if !ok {
		return fmt.Errorf("mitiru.toml の engine = %q は X.Y.Z の形ではありません。手で直してください。",
			cfg.Project.Engine)
	}

	console.Verbosef("Resolving latest MitiruEngine release\n")
	latest, err := engine.LatestVersion(os.Stdout)
	if err != nil {
		// offline / API 失敗時は pin を壊さずに抜ける (ADR 0010 #4)。
		return fmt.Errorf("could not resolve latest version (pin left at %s): %w",
			cur, err)
	}

	switch latest.Compare(cur) {
	case 0:
		fmt.Printf("engine %s が最新です。\n", cur)
		return nil
	case -1:
		fmt.Printf("指定している engine %s は、公開されている最新の %s より新しいので、そのままにします。\n",
			cur, latest)
		return nil
	}

	breaking := latest.Major > cur.Major || latest.Minor > cur.Minor
	fmt.Printf("engine %s から %s へ上げられます。\n", cur, latest)
	if breaking {
		fmt.Printf("この更新では ABI が変わることがあります。上げたら mitiru clean で古いビルドを消してから mitiru build してください。\n")
	}

	if checkOnly {
		fmt.Printf("--check なので何も変えていません。上げるには mitiru update を実行してください。\n")
		return nil
	}

	if !assumeYes && !confirm(fmt.Sprintf("mitiru.toml を engine = \"%s\" に書き換えますか?", latest)) {
		fmt.Println("取りやめました。mitiru.toml はそのままです。")
		return nil
	}

	if err := config.SetEngine(manifestPath, latest.String()); err != nil {
		return err
	}
	// MITIRU_ENGINE_ROOT override 中は tarball を引かない (pin は cosmetic)。
	if root := strings.TrimSpace(os.Getenv("MITIRU_ENGINE_ROOT")); root != "" {
		fmt.Printf("%s を engine = \"%s\" にしました。ただし MITIRU_ENGINE_ROOT があるので、ビルドには %s を使います。\n",
			config.ManifestFilename, latest, root)
		return nil
	}

	if _, err := engine.EnsureSource(latest.String(), os.Stdout); err != nil {
		return fmt.Errorf("engine %s を取得できません (%w)。mitiru.toml はもう書き換えたので、mitiru build で取得し直せます。", latest, err)
	}
	fmt.Printf("%s を engine = \"%s\" にしました。mitiru build でこの版を使ってビルドします。\n",
		config.ManifestFilename, latest)
	return nil
}

// confirm は stdin から y/N を読む。デフォルト No。
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return ans == "y" || ans == "yes"
}
