package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"github.com/spf13/cobra"
)

var bakeAll bool

func newBakeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bake",
		Short: "Bake placement JSON (assets/*.json) into POD bytes",
		Long: `Bake the project's Spawner-format placement JSON (assets/*.json, i.e. arrays
of objects with a "type" field, or {"objects":[...]}) into .baked POD files
next to them. At runtime the engine prefers a same-named .baked file over
re-parsing the JSON, so startup skips the JSON→float conversion pass and
matches the same initial state bit-for-bit every run (★4-1).

Building the project first (same as 'mitiru build') because baking needs the
game DLL's own reflect/spawner schema to lay bytes out exactly like it will
at runtime.

  mitiru bake        # bake every assets/*.json that looks like a Spawner file
  mitiru bake --all  # same (kept for symmetry with other --all flags)

Set [build] bake = true in mitiru.toml to run this automatically after
'mitiru build'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runAnyBuild()
			if err != nil {
				return err
			}
			if res.Config.Standalone() {
				return fmt.Errorf("standalone のプロジェクトでは mitiru bake を使えません。" +
					"焼くには game DLL の配置の定義が要ります。")
			}
			return runBakeAll(res.Artifacts, os.Stdout)
		},
	}
	cmd.Flags().BoolVar(&bakeAll, "all", true, "bake every assets/*.json (kept for symmetry, always on)")
	return cmd
}

// runBakeAll は DeployDir 配下の <TargetName>/assets/*.json を全て
// `mitiru_host <dll> --bake <in> <out>` で焼く。Spawner 形 (配列 or
// {"objects":[...]}) でないファイル (i18n / balance table 等の素 JSON) は
// host 側の bakeAssetsFileGeneric が失敗として返すので、1 行注記して skip する。
// assets/*.json には焼けない JSON が混ざっているのが普通なので、全体は止めない。
func runBakeAll(art *build.Artifacts, stdout io.Writer) error {
	assetsDir := filepath.Join(filepath.Dir(art.DllPath), "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stdout, "%s が無いので、焼くものはありません。\n", assetsDir)
			return nil
		}
		return fmt.Errorf("%s を読めません (%w)。", assetsDir, err)
	}

	baked, skipped := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		in := filepath.Join(assetsDir, e.Name())
		out := strings.TrimSuffix(in, filepath.Ext(in)) + ".baked"

		cmd := exec.Command(art.HostExePath, art.DllRel, "--bake", in, out)
		cmd.Dir = art.DeployDir
		cmd.Env = build.HostEnv()
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			// Spawner 形でない JSON は「焼けない」が正常系 (i18n など)。stderr 1 行だけ見せて続行する。
			console.Fverbosef(stdout, "  skip %s (%s)\n", e.Name(), strings.TrimSpace(string(output)))
			skipped++
			continue
		}
		console.Fverbosef(stdout, "  baked %s -> %s\n", e.Name(), filepath.Base(out))
		baked++
	}
	fmt.Fprintf(stdout, "%s の JSON を %d 個焼きました。", assetsDir, baked)
	if skipped > 0 {
		fmt.Fprintf(stdout, "配置の形でない %d 個は飛ばしました。", skipped)
	}
	fmt.Fprintln(stdout)
	return nil
}
