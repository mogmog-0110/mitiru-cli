// Package engine は MitiruEngine の source archive を取得し、
// ~/.mitiru/cache/engine-<version>/ 配下に cache する処理を担う。
package engine

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// publicRepo は public release repository。
	publicRepo = "mogmog-0110/MitiruEngine"

	// httpTimeout は単一の network request の上限。tarball は ~10 MB なので、
	// 遅い corporate proxy でも足りるよう default は余裕を持たせる。
	httpTimeout = 5 * time.Minute
)

// CacheRoot は on-disk engine cache root の absolute path を返す。
// default は ~/.mitiru/cache だが、test 用に MITIRU_CACHE_DIR で上書き可能。
func CacheRoot() (string, error) {
	if v := os.Getenv("MITIRU_CACHE_DIR"); v != "" {
		return filepath.Abs(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(home, ".mitiru", "cache"), nil
}

// EnsureSource は `version` の engine source が disk 上に展開済みであることを
// 保証し、source root (engine の top-level CMakeLists.txt を含む directory) の
// absolute path を返す。
//
// version は以下のいずれか:
//   - "v0.1.0" や "0.1.0" のような literal tag (後者は "v0.1.0" に正規化される)
//   - "latest"。最新の GitHub release tag に解決される
//
// test や power-user は MITIRU_ENGINE_ROOT を展開済みの engine source tree に
// 向けることで download を short-circuit できる。
func EnsureSource(version string, progress io.Writer) (string, error) {
	if progress == nil {
		progress = io.Discard
	}

	if override := strings.TrimSpace(os.Getenv("MITIRU_ENGINE_ROOT")); override != "" {
		abs, absErr := filepath.Abs(override)
		if absErr != nil {
			return "", fmt.Errorf("resolve MITIRU_ENGINE_ROOT: %w", absErr)
		}
		if _, statErr := os.Stat(filepath.Join(abs, "CMakeLists.txt")); statErr != nil {
			return "", fmt.Errorf("MITIRU_ENGINE_ROOT=%s does not contain CMakeLists.txt: %w",
				abs, statErr)
		}
		fmt.Fprintf(progress, "Using MITIRU_ENGINE_ROOT override: %s\n", abs)
		return abs, nil
	}

	tag, err := resolveTag(version, progress)
	if err != nil {
		return "", err
	}

	cacheRoot, err := CacheRoot()
	if err != nil {
		return "", err
	}
	versionDir := filepath.Join(cacheRoot, "engine-"+tag)
	markerFile := filepath.Join(versionDir, ".mitiru-cache-ok")

	if _, err := os.Stat(markerFile); err == nil {
		root, rerr := findSourceRoot(versionDir)
		if rerr == nil {
			// Cache hit — 何も fetch していないので何も出さない。下の download
			// path は自分で進捗を語る。no-op が build/run のたびに 1 行
			// (しかも長い absolute path) を増やすべきではない。
			return root, nil
		}
		// marker はあるが source layout が壊れている — fall through して
		// 再 fetch する。
		_ = os.RemoveAll(versionDir)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", markerFile, err)
	}

	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	fmt.Fprintf(progress, "Downloading MitiruEngine %s...\n", tag)
	if err := downloadAndExtract(tag, versionDir, progress); err != nil {
		// Best-effort: 中途半端な cache を残さない。
		_ = os.RemoveAll(versionDir)
		return "", err
	}

	if err := os.WriteFile(markerFile, []byte(tag+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write cache marker: %w", err)
	}

	root, err := findSourceRoot(versionDir)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(progress, "MitiruEngine %s ready at %s\n", tag, root)
	return root, nil
}

// resolveTag は user 向けの version を具体的な git tag 文字列に変換する。
func resolveTag(version string, progress io.Writer) (string, error) {
	v := strings.TrimSpace(version)
	if v == "" {
		return "", errors.New("engine version is empty")
	}
	if v == "latest" {
		fmt.Fprintln(progress, "Resolving latest MitiruEngine release...")
		tag, err := fetchLatestTag()
		if err != nil {
			return "", fmt.Errorf("resolve 'latest' tag: %w", err)
		}
		return tag, nil
	}
	if v[0] != 'v' && v[0] != 'V' {
		v = "v" + v
	}
	return v, nil
}

type ghRelease struct {
	TagName string `json:"tag_name"`
}

func fetchLatestTag() (string, error) {
	url := "https://api.github.com/repos/" + publicRepo + "/releases/latest"
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mitiru-cli")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return "", fmt.Errorf("github releases API returned %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("decode releases response: %w", err)
	}
	if rel.TagName == "" {
		return "", errors.New("github releases API returned no tag_name")
	}
	return rel.TagName, nil
}

func downloadAndExtract(tag, destDir string, progress io.Writer) error {
	url := fmt.Sprintf(
		"https://github.com/%s/archive/refs/tags/%s.tar.gz",
		publicRepo, tag)

	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mitiru-cli")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s (does tag %q exist on %s?)",
			url, resp.Status, tag, publicRepo)
	}

	return extractTarGz(resp.Body, destDir, progress)
}

// extractTarGz は gzip 圧縮された tar stream を destDir に展開する。
// archive の単一 top-level directory は保持される (例: MitiruEngine-0.1.0/)。
func extractTarGz(r io.Reader, destDir string, progress io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	fileCount := 0
	var totalBytes int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		// entry 名を sanitise する。path traversal の試みは拒否する。
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") ||
			strings.Contains(cleanName, string(filepath.Separator)+"..") {
			return fmt.Errorf("tar entry escapes archive root: %q", hdr.Name)
		}
		outPath := filepath.Join(destDir, cleanName)

		// destDir の外を指す absolute path や symlink を防ぐ。
		rel, err := filepath.Rel(destDir, outPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("tar entry escapes destination: %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", outPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return fmt.Errorf("mkdir parent of %s: %w", outPath, err)
			}
			f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("create %s: %w", outPath, err)
			}
			n, err := io.Copy(f, tr)
			closeErr := f.Close()
			if err != nil {
				return fmt.Errorf("write %s: %w", outPath, err)
			}
			if closeErr != nil {
				return fmt.Errorf("close %s: %w", outPath, closeErr)
			}
			fileCount++
			totalBytes += n
			if fileCount%200 == 0 {
				fmt.Fprintf(progress, "  extracted %d files (%.1f MB)...\n",
					fileCount, float64(totalBytes)/(1024*1024))
			}
		case tar.TypeSymlink, tar.TypeLink:
			// symlink は skip — Windows は admin 無しでは default で許可せず、
			// engine archive 内でも load-bearing ではない。
			continue
		default:
			// その他の type (xattr header、sparse file 等) は黙って skip。
			continue
		}
	}

	fmt.Fprintf(progress, "Extracted %d files (%.1f MB total).\n",
		fileCount, float64(totalBytes)/(1024*1024))
	return nil
}

// findSourceRoot は versionDir 内で top-level の engine CMakeLists.txt を含む
// directory の absolute path を返す。GitHub の source archive は repo を
// <Repo>-<sha-or-tag>/ で包むので、1 階層下も見る。
func findSourceRoot(versionDir string) (string, error) {
	// まず: versionDir 自体が直接 CMakeLists.txt を含むかもしれない。
	if _, err := os.Stat(filepath.Join(versionDir, "CMakeLists.txt")); err == nil {
		return versionDir, nil
	}
	entries, err := os.ReadDir(versionDir)
	if err != nil {
		return "", fmt.Errorf("read cache dir %s: %w", versionDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(versionDir, e.Name())
		if _, err := os.Stat(filepath.Join(candidate, "CMakeLists.txt")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no CMakeLists.txt found under %s (corrupt cache?)",
		versionDir)
}
