package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

var cleanAll bool

func newCleanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete build artefacts",
		Long: `Delete the per-project build/ directory.

With --all, also delete the global engine cache at ~/.mitiru/cache/. Run this
when you want to force a re-download of the engine source on the next build.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean()
		},
	}
	cmd.Flags().BoolVar(&cleanAll, "all", false,
		"also delete the global engine cache (~/.mitiru/cache/)")
	return cmd
}

func runClean() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	// project ローカルの build/ — project 内にいる場合のみ必要。
	_, projectRoot, manifestErr := config.FindManifest(cwd)
	if manifestErr == nil {
		buildDir := filepath.Join(projectRoot, "build")
		if err := removeIfPresent(buildDir, "build directory"); err != nil {
			return err
		}
	} else if !cleanAll {
		// project も --all も無い → 何もすることがない。manifest error を返す。
		return manifestErr
	} else {
		// project 無しの --all は OK — cache 削除へ進む。
		fmt.Println("Note: not inside a mitiru project; skipping local build/ cleanup.")
	}

	if cleanAll {
		cacheRoot, err := engine.CacheRoot()
		if err != nil {
			return err
		}
		if err := removeIfPresent(cacheRoot, "engine cache"); err != nil {
			return err
		}
	}

	return nil
}

func removeIfPresent(path, label string) error {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Skipping %s (%s does not exist).\n", label, path)
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory: %s", label, path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	fmt.Printf("Deleted %s: %s\n", label, path)
	return nil
}
