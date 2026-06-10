package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// モック HTTP サーバーで verify の HTTP ヘルパーが正しく動くことを確認する。
// 実際のビルドや host 起動はしない (subprocess を起動しない)。

func TestWaitReady_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == apiState {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if err := waitReady(srv.URL, 5*time.Second); err != nil {
		t.Fatalf("waitReady should succeed: %v", err)
	}
}

func TestWaitReady_Timeout(t *testing.T) {
	// 常に 503 を返すサーバー → タイムアウトすること。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := waitReady(srv.URL, 200*time.Millisecond); err == nil {
		t.Fatal("waitReady should time out")
	}
}

func TestApiGet_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == apiState {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hp":100}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	body, status, err := apiGet(srv.URL, apiState)
	if err != nil {
		t.Fatalf("apiGet error: %v", err)
	}
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(string(body), "hp") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestApiPost_OK(t *testing.T) {
	received := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == apiQuit {
			received = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, status, err := apiPost(srv.URL, apiQuit, nil, "")
	if err != nil {
		t.Fatalf("apiPost error: %v", err)
	}
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if !received {
		t.Error("POST was not received by server")
	}
}

func TestPickFreePort(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("invalid port %d", port)
	}
}

func TestComparePNG_Identical(t *testing.T) {
	dir := t.TempDir()
	data := []byte("PNG\x00\x01\x02\x03")
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, data, 0o644); err != nil {
		t.Fatal(err)
	}
	pct, err := comparePNG(a, b)
	if err != nil {
		t.Fatalf("comparePNG: %v", err)
	}
	if pct != 1.0 {
		t.Errorf("identical files: pct = %f, want 1.0", pct)
	}
}

func TestComparePNG_Different(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, []byte("AAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("BBBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	pct, err := comparePNG(a, b)
	if err != nil {
		t.Fatalf("comparePNG: %v", err)
	}
	if pct >= 1.0 {
		t.Errorf("different files: pct = %f, want < 1.0", pct)
	}
}

func TestComparePNG_DifferentSize(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, []byte("AAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("BBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	pct, err := comparePNG(a, b)
	if err != nil {
		t.Fatalf("comparePNG: %v", err)
	}
	if pct != 0.0 {
		t.Errorf("different-size files: pct = %f, want 0.0", pct)
	}
}

func TestExitCodeForVerdict(t *testing.T) {
	cases := []struct {
		verdict  string
		wantCode int
	}{
		{"pass", 0},
		{"fail", 1},
		{"build_error", 2},
		{"unknown", 1},
	}
	for _, c := range cases {
		got := exitCodeForVerdict(c.verdict)
		if got != c.wantCode {
			t.Errorf("exitCodeForVerdict(%q) = %d, want %d", c.verdict, got, c.wantCode)
		}
	}
}

func TestVerifyResultJSON_Schema(t *testing.T) {
	// verifyResult が期待するフィールドを持つ JSON に変換できることを確認する。
	r := &verifyResult{
		Build:   "ok",
		Verdict: "pass",
		Screenshot: &verifyShotResult{
			Path:          "/tmp/shot.png",
			GoldenFile:    "ref.png",
			GoldenDiffPct: 0.97,
		},
		Replay: &verifyReplayResult{
			BitExact: true,
			ExitCode: 0,
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"build"`, `"verdict"`, `"screenshot"`, `"replay"`, `"bitExact"`, `"goldenDiffPct"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("JSON missing key %s: %s", key, b)
		}
	}
}
