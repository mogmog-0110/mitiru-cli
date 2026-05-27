package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mogmog-0110/mitiru-cli/internal/engine"
)

// 更新通知 (受動 footer)。build / run の最後に、新しい engine release や CLI binary が
// あれば一行だけ知らせる。pulled-UI 哲学 (ADR 0010) を守るため:
//   - ユーザーが既に叩いたコマンドの文脈でのみ出す (background daemon ではない)
//   - 自動 DL しない。導線 (`mitiru update` / `mitiru self-update`) を示すだけ
//   - 24h キャッシュ + 短 timeout + オフライン無言で build を遅くしない
//   - MITIRU_NO_UPDATE_CHECK / 非 TTY (CI) では完全に黙る

const (
	updateCheckTTL     = 24 * time.Hour
	updateCheckTimeout = 2500 * time.Millisecond
)

type updateCache struct {
	CheckedAt    time.Time `json:"checked_at"`
	LatestEngine string    `json:"latest_engine"` // "X.Y.Z" — 取得失敗時は空
	LatestCLI    string    `json:"latest_cli"`    // "X.Y.Z" — 取得失敗時は空
}

func updateCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mitiru", "update-check.json"), nil
}

// maybeNotifyUpdates は enginePin (project の mitiru.toml engine) と cliVersion を、
// キャッシュ済み (または今 fetch した) 最新版と比べ、新しければ footer を出す。
// 失敗・抑制時は何もしない (build/run を妨げない)。
func maybeNotifyUpdates(enginePin string, w io.Writer) {
	if !updateChecksEnabled() {
		return
	}
	c := loadOrRefreshUpdateCache()
	lines := updateNotices(enginePin, c.LatestEngine, cliVersion, c.LatestCLI)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// updateNotices は現在版と最新版を比べ、表示すべき footer 行を返す (純関数)。
// 最新版が空 / 解析不能 / 現在版以下なら、その対象は黙る。
func updateNotices(enginePin, latestEngine, cliCur, latestCLI string) []string {
	var lines []string
	if cur, ok := engine.ParseSemver(enginePin); ok {
		if latest, ok := engine.ParseSemver(latestEngine); ok && latest.Compare(cur) > 0 {
			lines = append(lines,
				fmt.Sprintf("  ▸ engine %s available (pinned %s)", latest, cur),
				"    run 'mitiru update' to upgrade")
		}
	}
	if cur, ok := engine.ParseSemver(cliCur); ok {
		if latest, ok := engine.ParseSemver(latestCLI); ok && latest.Compare(cur) > 0 {
			lines = append(lines,
				fmt.Sprintf("  ▸ mitiru CLI %s available (running %s)", latest, cur),
				"    run 'mitiru self-update' to upgrade")
		}
	}
	return lines
}

// updateChecksEnabled は通知を出してよい状況か判定する (抑制 env / 非対話を除外)。
func updateChecksEnabled() bool {
	if strings.TrimSpace(os.Getenv("MITIRU_NO_UPDATE_CHECK")) != "" {
		return false
	}
	// 非 TTY (CI / パイプ) では出さない。
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// loadOrRefreshUpdateCache は 24h 以内のキャッシュをそのまま返し、古ければ
// 短 timeout で再取得する。取得結果 (成功でも失敗でも) で CheckedAt を更新し、
// オフライン時に毎回 timeout を払わないようにする。
func loadOrRefreshUpdateCache() updateCache {
	path, err := updateCachePath()
	if err != nil {
		return updateCache{}
	}
	if data, err := os.ReadFile(path); err == nil {
		var c updateCache
		if json.Unmarshal(data, &c) == nil && time.Since(c.CheckedAt) < updateCheckTTL {
			return c
		}
	}

	c := updateCache{CheckedAt: time.Now()}
	if v, err := engine.LatestVersionTimeout(updateCheckTimeout); err == nil {
		c.LatestEngine = v.String()
	}
	if v, err := fetchLatestCLITag(updateCheckTimeout); err == nil {
		c.LatestCLI = v.String()
	}

	if data, err := json.Marshal(c); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, data, 0o644)
	}
	return c
}

// fetchLatestCLITag は mitiru-cli の releases/latest から tag を semver で返す。
// CLI binary は Release が配布形態なので releases/latest が権威 (self-update と同根拠)。
func fetchLatestCLITag(timeout time.Duration) (engine.Semver, error) {
	url := "https://api.github.com/repos/" + cliRepo + "/releases/latest"
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return engine.Semver{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mitiru-cli")
	resp, err := client.Do(req)
	if err != nil {
		return engine.Semver{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return engine.Semver{}, fmt.Errorf("releases API %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return engine.Semver{}, err
	}
	v, ok := engine.ParseSemver(rel.TagName)
	if !ok {
		return engine.Semver{}, fmt.Errorf("tag %q not semver", rel.TagName)
	}
	return v, nil
}
