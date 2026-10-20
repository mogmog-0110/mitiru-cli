package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeCommit = "0123456789abcdef0123456789abcdef01234567"

// 公式の archive と同じく、Dawn-<コミット>-windows-latest-Release/ で包んだ tar.gz を作る。
func fakeDawnArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	top := "Dawn-" + fakeCommit + "-windows-latest-Release/"
	for name, body := range map[string]string{
		"include/webgpu/webgpu_cpp.h": "// header",
		"lib/webgpu_dawn.lib":         "lib",
	} {
		if err := tw.WriteHeader(&tar.Header{Name: top + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fakeEngineWithPin(t *testing.T, sha string) string {
	t.Helper()
	root := t.TempDir()
	pin := "set(MITIRU_DAWN_TAG \"v1\")\nset(MITIRU_DAWN_COMMIT \"" + fakeCommit + "\")\n" +
		"set(MITIRU_DAWN_ARCHIVE_SHA256 \"" + sha + "\")\n"
	if err := os.MkdirAll(filepath.Join(root, "cmake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmake", "dawn_version.cmake"), []byte(pin), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func serveDawn(t *testing.T, body []byte, hits *int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if r.URL.Path != "/v1/Dawn-"+fakeCommit+"-windows-latest-Release.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	old := dawnReleaseBase
	dawnReleaseBase = srv.URL
	t.Cleanup(func() { srv.Close(); dawnReleaseBase = old })
}

func sumHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func isolateDawnEnv(t *testing.T) string {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("MITIRU_CACHE_DIR", cache)
	t.Setenv(DawnDirEnv, "")
	return cache
}

func TestEnsureDawn_FetchesVerifiesAndCaches(t *testing.T) {
	cache := isolateDawnEnv(t)
	archive := fakeDawnArchive(t)
	hits := 0
	serveDawn(t, archive, &hits)
	engineRoot := fakeEngineWithPin(t, sumHex(archive))

	var progress bytes.Buffer
	dir, err := EnsureDawn(engineRoot, &progress)
	if err != nil {
		t.Fatalf("EnsureDawn: %v", err)
	}
	if want := filepath.Join(cache, "dawn-"+fakeCommit[:dawnDirCommitLen]); dir != want {
		t.Errorf("dir = %s, want %s", dir, want)
	}
	if !dawnDirUsable(dir) {
		t.Errorf("%s に include と lib が無い", dir)
	}
	if !strings.Contains(progress.String(), "ダウンロード") {
		t.Errorf("取得の知らせが無い: %q", progress.String())
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Errorf("cache に残るのは展開先だけのはず: %v", entries)
	}

	progress.Reset()
	if _, err := EnsureDawn(engineRoot, &progress); err != nil {
		t.Fatalf("2 回目: %v", err)
	}
	if hits != 1 || progress.Len() != 0 {
		t.Errorf("cache があるのに取り直した (hits=%d, 出力=%q)", hits, progress.String())
	}
}

func TestEnsureDawn_RejectsAWrongChecksum(t *testing.T) {
	cache := isolateDawnEnv(t)
	hits := 0
	serveDawn(t, fakeDawnArchive(t), &hits)
	engineRoot := fakeEngineWithPin(t, strings.Repeat("0", 64))

	_, err := EnsureDawn(engineRoot, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("SHA-256 の食い違いで止まるはず: %v", err)
	}
	if !strings.Contains(err.Error(), DawnDirEnv) {
		t.Errorf("手で用意する道が書かれていない: %v", err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("失敗したのに cache に物が残った: %v", entries)
	}
}

func TestEnsureDawn_FailsShortlyOnHTTPError(t *testing.T) {
	isolateDawnEnv(t)
	hits := 0
	serveDawn(t, nil, &hits)
	engineRoot := fakeEngineWithPin(t, strings.Repeat("a", 64))
	old := dawnReleaseBase
	dawnReleaseBase += "/missing"
	defer func() { dawnReleaseBase = old }()

	_, err := EnsureDawn(engineRoot, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404 を伝えるはず: %v", err)
	}
}

func TestEnsureDawn_UsesTheEnvDirFirstAndIgnoresABrokenOne(t *testing.T) {
	isolateDawnEnv(t)
	archive := fakeDawnArchive(t)
	hits := 0
	serveDawn(t, archive, &hits)
	engineRoot := fakeEngineWithPin(t, sumHex(archive))

	good := t.TempDir()
	for _, rel := range []string{"include/webgpu/webgpu_cpp.h", "lib/webgpu_dawn.lib"} {
		p := filepath.Join(good, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	t.Setenv(DawnDirEnv, good)
	if dir, err := EnsureDawn(engineRoot, nil); err != nil || dir != good || hits != 0 {
		t.Fatalf("環境変数の展開先を使うはず: dir=%s err=%v hits=%d", dir, err, hits)
	}

	t.Setenv(DawnDirEnv, filepath.Join(good, "nothing"))
	var progress bytes.Buffer
	dir, err := EnsureDawn(engineRoot, &progress)
	if err != nil || dir == good {
		t.Fatalf("壊れた環境変数は cache に落ちるはず: dir=%s err=%v", dir, err)
	}
	if !strings.Contains(progress.String(), DawnDirEnv) {
		t.Errorf("環境変数を無視したことを知らせるはず: %q", progress.String())
	}
}

func TestEnsureDawn_EngineWithoutPin(t *testing.T) {
	isolateDawnEnv(t)
	if _, err := EnsureDawn(t.TempDir(), nil); err != ErrNoDawnPin {
		t.Fatalf("err = %v, want ErrNoDawnPin", err)
	}
}
