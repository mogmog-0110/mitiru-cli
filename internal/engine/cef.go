package engine

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	cefVersion   = "128.4.12+g1d7a1f9+chromium-128.0.6613.138"
	cefPlatform  = "windows64"
	cefDistType  = "minimal"
	cefCDNBase   = "https://cef-builds.spotifycdn.com"
	cefTimeout   = httpTimeout // reuse 5-minute cap from cache.go
)

// cefArchiveName returns the .tar.bz2 filename for the pinned CEF build.
func cefArchiveName() string {
	return fmt.Sprintf("cef_binary_%s_%s_%s.tar.bz2", cefVersion, cefPlatform, cefDistType)
}

// cefDirName is the top-level directory extracted from the archive.
// fetch_cef.py keeps this directory inside external/cef/, so cmake's
// FindCEF resolves to external/cef/<dirName>/{Release,Resources,include,...}.
func cefDirName() string {
	return fmt.Sprintf("cef_binary_%s_%s", cefVersion, cefPlatform)
}

// cefDownloadURL returns the percent-encoded URL for the CEF archive.
// The version string contains '+' which must be encoded as %2B.
func cefDownloadURL() string {
	archiveName := cefArchiveName()
	encoded := strings.ReplaceAll(archiveName, "+", "%2B")
	return cefCDNBase + "/" + encoded
}

// EnsureCEF guarantees that <engineRoot>/external/cef/<cefDirName> exists and
// is populated. If it is already present, it returns nil immediately (cached).
// Otherwise it downloads and extracts the minimal CEF binary archive.
func EnsureCEF(engineRoot string, progress io.Writer) error {
	if progress == nil {
		progress = io.Discard
	}

	externalCef := filepath.Join(engineRoot, "external", "cef")
	targetDir := filepath.Join(externalCef, cefDirName())

	if dirNonEmpty(targetDir) {
		// Cache hit — CEF is already extracted, so report nothing. The
		// download branch below narrates its (multi-minute) work itself.
		return nil
	}

	if err := os.MkdirAll(externalCef, 0o755); err != nil {
		return fmt.Errorf("create external/cef dir: %w", err)
	}

	url := cefDownloadURL()
	fmt.Fprintf(progress, "Downloading CEF %s (this may take a few minutes)...\n", cefVersion)
	fmt.Fprintf(progress, "  URL: %s\n", url)

	client := &http.Client{Timeout: cefTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build CEF download request: %w", err)
	}
	req.Header.Set("User-Agent", "mitiru-cli")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"CEF download returned %s\n"+
				"  URL: %s\n"+
				"  Check https://cef-builds.spotifycdn.com/index.html for available versions.",
			resp.Status, url)
	}

	fmt.Fprintln(progress, "  Extracting CEF archive...")
	if err := extractTarBz2(resp.Body, externalCef, progress); err != nil {
		// Best-effort cleanup — leave no half-extracted tree.
		_ = os.RemoveAll(targetDir)
		return fmt.Errorf("extract CEF archive: %w", err)
	}

	// fetch_cef.py renames the _minimal-suffixed dir to the canonical name if
	// needed. Mirror that: if the exact targetDir is absent but a prefixed
	// sibling exists, rename it.
	if !dirNonEmpty(targetDir) {
		if err := fixupCEFDirName(externalCef, targetDir); err != nil {
			return err
		}
	}

	if !dirNonEmpty(targetDir) {
		return fmt.Errorf(
			"CEF extraction completed but expected directory not found: %s\n"+
				"  The archive layout may have changed — check cef-builds.spotifycdn.com.",
			targetDir)
	}

	fmt.Fprintf(progress, "CEF ready at %s\n", targetDir)
	return nil
}

// extractTarBz2 extracts a bzip2-compressed tar stream into destDir,
// preserving the archive's top-level directory (matching fetch_cef.py behaviour).
func extractTarBz2(r io.Reader, destDir string, progress io.Writer) error {
	bzr := bzip2.NewReader(r)
	tr := tar.NewReader(bzr)

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

		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") ||
			strings.Contains(cleanName, string(filepath.Separator)+"..") {
			return fmt.Errorf("tar entry escapes archive root: %q", hdr.Name)
		}
		outPath := filepath.Join(destDir, cleanName)

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
				return fmt.Errorf("mkdir parent %s: %w", outPath, err)
			}
			f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("create %s: %w", outPath, err)
			}
			n, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return fmt.Errorf("write %s: %w", outPath, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close %s: %w", outPath, closeErr)
			}
			fileCount++
			totalBytes += n
			if fileCount%500 == 0 {
				fmt.Fprintf(progress, "  extracted %d files (%.1f MB)...\n",
					fileCount, float64(totalBytes)/(1024*1024))
			}
		default:
			// Skip symlinks and special entries (Windows doesn't need them).
			continue
		}
	}

	fmt.Fprintf(progress, "  Extracted %d files (%.1f MB total).\n",
		fileCount, float64(totalBytes)/(1024*1024))
	return nil
}

// fixupCEFDirName renames the first directory under parent whose name begins
// with cefDirName() to the exact canonical name. This mirrors fetch_cef.py's
// handling of the "_minimal" suffix variant.
func fixupCEFDirName(parent, want string) error {
	prefix := cefDirName()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return fmt.Errorf("read %s: %w", parent, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) && e.Name() != filepath.Base(want) {
			src := filepath.Join(parent, e.Name())
			if renErr := os.Rename(src, want); renErr != nil {
				return fmt.Errorf("rename %s -> %s: %w", src, want, renErr)
			}
			return nil
		}
	}
	return nil
}

// dirNonEmpty reports whether path exists as a directory and contains at least
// one entry.
func dirNonEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}
