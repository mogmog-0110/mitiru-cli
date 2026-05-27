package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
	"github.com/spf13/cobra"
)

// self-update は mitiru.exe 自体を最新 release に置き換える。プロジェクトの engine
// pin を上げる 'update' とは別関心事 (ADR 0010)。CLI の binary は GitHub Release に
// attach される (goreleaser) ため、engine と違い releases/latest API が権威。

const cliRepo = "mogmog-0110/mitiru-cli"

// pickAsset は release assets から現在の OS/arch 向けの mitiru binary を選ぶ。
// goreleaser の name_template の細部 (amd64 vs x86_64、区切り文字) に依存しないよう、
// 名前に OS と arch 別名が両方含まれるものを許容マッチする (ADR 0010 — CI 出力名の
// ブレに対する堅牢性)。複数該当時は最初を返す。該当なしは ""。
func pickAsset(assets []ghAsset) string {
	archAliases := []string{runtime.GOARCH}
	switch runtime.GOARCH {
	case "amd64":
		archAliases = append(archAliases, "x86_64", "x64")
	case "arm64":
		archAliases = append(archAliases, "aarch64")
	}
	wantExe := runtime.GOOS == "windows"
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if !strings.HasPrefix(name, "mitiru") {
			continue
		}
		if wantExe && !strings.HasSuffix(name, ".exe") {
			continue
		}
		if !strings.Contains(name, runtime.GOOS) {
			continue
		}
		archOK := false
		for _, al := range archAliases {
			if strings.Contains(name, al) {
				archOK = true
				break
			}
		}
		if archOK {
			return a.URL
		}
	}
	return ""
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type ghReleaseFull struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

func newSelfUpdateCommand() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update the mitiru CLI binary itself to the latest release",
		Long: `Downloads the latest mitiru.exe from the CLI's GitHub releases and
replaces the running binary in place.

  mitiru self-update          # download latest and swap the binary
  mitiru self-update --check  # report the available version only

To update a project's engine version instead, use 'mitiru update'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSelfUpdate(checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report the latest version only; change nothing")
	return cmd
}

func runSelfUpdate(checkOnly bool) error {
	cur, ok := engine.ParseSemver(cliVersion)
	if !ok {
		return fmt.Errorf("current CLI version %q is not parseable", cliVersion)
	}

	fmt.Println("Checking for the latest mitiru release...")
	rel, err := fetchLatestCLIRelease()
	if err != nil {
		return err
	}
	latest, ok := engine.ParseSemver(rel.TagName)
	if !ok {
		return fmt.Errorf("latest release tag %q is not a X.Y.Z version", rel.TagName)
	}

	switch latest.Compare(cur) {
	case 0:
		fmt.Printf("Already up to date: mitiru %s is the latest release.\n", cur)
		return nil
	case -1:
		fmt.Printf("Running mitiru %s is newer than the latest release %s; leaving it.\n", cur, latest)
		return nil
	}

	fmt.Printf("\n  update available: mitiru %s -> %s\n", cur, latest)
	if checkOnly {
		fmt.Println("\n  (--check) no changes made. Run 'mitiru self-update' to apply.")
		return nil
	}

	dlURL := pickAsset(rel.Assets)
	if dlURL == "" {
		return fmt.Errorf("release %s has no mitiru asset for %s/%s (platform unsupported by this release?)",
			latest, runtime.GOOS, runtime.GOARCH)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	fmt.Printf("Downloading mitiru %s for %s/%s...\n", latest, runtime.GOOS, runtime.GOARCH)
	if err := downloadBinarySwap(dlURL, exe); err != nil {
		return err
	}
	fmt.Printf("Updated: %s is now mitiru %s.\n", exe, latest)
	return nil
}

func fetchLatestCLIRelease() (*ghReleaseFull, error) {
	url := "https://api.github.com/repos/" + cliRepo + "/releases/latest"
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mitiru-cli")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return nil, fmt.Errorf("github releases API returned %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}
	var rel ghReleaseFull
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("latest release has no tag_name")
	}
	return &rel, nil
}

// downloadBinarySwap は url の中身を newExe の隣に書き、rename-swap で置き換える。
// 実行中の binary は上書きできない (Windows: ERROR_SHARING_VIOLATION) が rename は
// 可能なので、現 exe を .old に退避 → 新 exe を配置する。.old は次回起動時に
// cleanupStaleSelfUpdate() が best-effort で削除する (ADR 0010 #8)。
func downloadBinarySwap(url, exe string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "mitiru-cli")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned %s", url, resp.Status)
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".mitiru-update-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod download: %w", err)
	}

	oldPath := exe + ".old"
	_ = os.Remove(oldPath) // 前回の残骸があれば消す
	if err := os.Rename(exe, oldPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("move running binary aside: %w", err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		// 失敗したら現 binary を復旧する。
		_ = os.Rename(oldPath, exe)
		_ = os.Remove(tmpPath)
		return fmt.Errorf("install new binary: %w", err)
	}
	// .old は実行中のため削除できないことがある — 黙って残し、次回起動時に消す。
	_ = os.Remove(oldPath)
	return nil
}

// cleanupStaleSelfUpdate は前回 self-update が残した <exe>.old を best-effort で
// 削除する。CLI 起動ごとに 1 回だけ呼ぶ (失敗は無視)。
func cleanupStaleSelfUpdate() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}
