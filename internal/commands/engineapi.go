package commands

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// エンジン HTTP API のエンドポイント定数。
// エンドポイント名が変わった場合はこのファイルの定数のみ直す。
const (
	apiState    = "/api/ai/state"
	apiDiff     = "/api/ai/diff"
	apiBranch   = "/api/ai/branch"
	apiShot     = "/api/screenshot"
	apiInput    = "/api/input/simulate"
	apiPause    = "/api/runtime/pause"
	apiStep     = "/api/runtime/step"
	apiTimescale = "/api/runtime/timescale"
	apiQuit     = "/api/runtime/quit"
	apiScene    = "/api/scene/tree"
	apiFrame    = "/api/ai/frame"
	apiAudio    = "/api/ai/audio"
)

// apiGet は baseURL に対して GET リクエストを送り、レスポンスボディを返す。
// baseURL は "http://127.0.0.1:<port>" 形式。
func apiGet(baseURL, path string) ([]byte, int, error) {
	resp, err := http.Get(baseURL + path)
	if err != nil {
		return nil, 0, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read %s: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

// apiPost は baseURL に対して POST リクエストを送る。ボディは不要な場合 nil でよい。
func apiPost(baseURL, path string, body io.Reader, contentType string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodPost, baseURL+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("POST %s: build request: %w", path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read POST %s: %w", path, err)
	}
	return respBody, resp.StatusCode, nil
}

// pickFreePort は OS にポートを割り当ててもらい、その番号を返す。
// Listen → すぐ Close することで「空きポート」を確保する (衝突リスクは低い)。
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("pick free port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// waitReady は baseURL の GET /api/ai/state が 200 を返すまで poll する。
// タイムアウトは timeout (推奨 30s)。0.5 秒間隔で retry する。
func waitReady(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + apiState)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("engine at %s did not become ready within %s", baseURL, timeout)
}
