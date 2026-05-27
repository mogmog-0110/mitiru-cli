package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Semver は単純な X.Y.Z 三つ組。MitiruEngine の tag は常にこの形 (pre-release /
// build metadata は使わない) なので、依存を増やさず自前で扱う。
type Semver struct {
	Major, Minor, Patch int
}

// ParseSemver は "v0.7.0" / "0.7.0" を Semver に変換する。'v' 接頭辞は許容。
// X.Y.Z 以外 (pre-release suffix 等) は ok=false。
func ParseSemver(s string) (Semver, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Semver{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Semver{}, false
		}
		nums[i] = n
	}
	return Semver{Major: nums[0], Minor: nums[1], Patch: nums[2]}, true
}

// Compare は a<b なら -1、a==b なら 0、a>b なら +1 を返す。
func (a Semver) Compare(b Semver) int {
	switch {
	case a.Major != b.Major:
		return cmpInt(a.Major, b.Major)
	case a.Minor != b.Minor:
		return cmpInt(a.Minor, b.Minor)
	default:
		return cmpInt(a.Patch, b.Patch)
	}
}

// String は "X.Y.Z" (v 接頭辞なし、toml に書く形) を返す。
func (a Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", a.Major, a.Minor, a.Patch)
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

type ghTag struct {
	Name string `json:"name"`
}

// LatestVersion は public repo の tag を列挙し、semver 最大を返す。
//
// GitHub *Releases* ではなく *tags* を見る理由: release snapshot pipeline は tag を
// push するが Release を作るとは限らず、`releases/latest` API は最新 tag を取りこぼす
// ことがある (ADR 0010 の失敗モード #1)。X.Y.Z 形でない tag は無視する。
func LatestVersion(progress io.Writer) (Semver, error) {
	if progress == nil {
		progress = io.Discard
	}
	url := "https://api.github.com/repos/" + publicRepo + "/tags?per_page=100"
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Semver{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mitiru-cli")

	resp, err := client.Do(req)
	if err != nil {
		return Semver{}, fmt.Errorf("list tags on %s: %w", publicRepo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return Semver{}, fmt.Errorf("github tags API returned %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var tags []ghTag
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return Semver{}, fmt.Errorf("decode tags response: %w", err)
	}

	best, found := Semver{}, false
	for _, t := range tags {
		v, ok := ParseSemver(t.Name)
		if !ok {
			continue
		}
		if !found || v.Compare(best) > 0 {
			best, found = v, true
		}
	}
	if !found {
		return Semver{}, errors.New("no semver tags found on " + publicRepo)
	}
	return best, nil
}
