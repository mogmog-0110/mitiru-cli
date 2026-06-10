package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- mcpBaseURL 解決 ---

func TestMCPBaseURL_DefaultPort(t *testing.T) {
	// ポート指定なし、環境変数なし → 8090。
	old := mcpPort
	mcpPort = 0
	t.Setenv("MITIRU_AI_PORT", "")
	defer func() { mcpPort = old }()

	url := mcpBaseURL()
	if url != "http://127.0.0.1:8090" {
		t.Errorf("mcpBaseURL() = %q, want %q", url, "http://127.0.0.1:8090")
	}
}

func TestMCPBaseURL_FlagOverride(t *testing.T) {
	old := mcpPort
	mcpPort = 9999
	defer func() { mcpPort = old }()

	url := mcpBaseURL()
	if url != "http://127.0.0.1:9999" {
		t.Errorf("mcpBaseURL() = %q, want %q", url, "http://127.0.0.1:9999")
	}
}

func TestMCPBaseURL_EnvOverride(t *testing.T) {
	old := mcpPort
	mcpPort = 0
	defer func() { mcpPort = old }()
	t.Setenv("MITIRU_AI_PORT", "8123")

	url := mcpBaseURL()
	if url != "http://127.0.0.1:8123" {
		t.Errorf("mcpBaseURL() = %q, want %q", url, "http://127.0.0.1:8123")
	}
}

// --- tools 一覧 ---

func TestMCPTools_AllPresent(t *testing.T) {
	want := []string{
		"game_state", "state_diff", "screenshot", "simulate_input",
		"pause", "step", "timescale", "scene_tree", "verify",
	}
	names := map[string]bool{}
	for _, tool := range mcpTools {
		names[tool.Name] = true
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("mcpTools missing %q", w)
		}
	}
}

func TestMCPTools_InputSchemaValidJSON(t *testing.T) {
	for _, tool := range mcpTools {
		var v interface{}
		if err := json.Unmarshal(tool.InputSchema, &v); err != nil {
			t.Errorf("tool %q: InputSchema is not valid JSON: %v", tool.Name, err)
		}
	}
}

// --- dispatchTool (モックサーバー経由) ---

func TestDispatch_GameState(t *testing.T) {
	srv := mockEngineServer(t, map[string]string{
		apiState: `{"hp":100}`,
	})
	defer srv.Close()

	res, err := dispatchTool("game_state", nil, srv.URL)
	if err != nil {
		t.Fatalf("dispatch game_state: %v", err)
	}
	text := extractText(t, res)
	if !strings.Contains(text, "hp") {
		t.Errorf("game_state: unexpected text: %s", text)
	}
}

func TestDispatch_StateDiff(t *testing.T) {
	srv := mockEngineServer(t, map[string]string{
		apiDiff: `{"delta":{"hp":-10}}`,
	})
	defer srv.Close()

	res, err := dispatchTool("state_diff", nil, srv.URL)
	if err != nil {
		t.Fatalf("dispatch state_diff: %v", err)
	}
	text := extractText(t, res)
	if !strings.Contains(text, "delta") {
		t.Errorf("state_diff: unexpected text: %s", text)
	}
}

func TestDispatch_Pause(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == apiPause {
			called = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := dispatchTool("pause", nil, srv.URL)
	if err != nil {
		t.Fatalf("dispatch pause: %v", err)
	}
	if !called {
		t.Error("pause endpoint not called")
	}
}

func TestDispatch_Step(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == apiStep {
			called = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := dispatchTool("step", nil, srv.URL)
	if err != nil {
		t.Fatalf("dispatch step: %v", err)
	}
	if !called {
		t.Error("step endpoint not called")
	}
}

func TestDispatch_Timescale(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == apiTimescale {
			b, _ := io.ReadAll(r.Body)
			receivedBody = string(b)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	args := json.RawMessage(`{"scale":0.5}`)
	_, err := dispatchTool("timescale", args, srv.URL)
	if err != nil {
		t.Fatalf("dispatch timescale: %v", err)
	}
	if !strings.Contains(receivedBody, "0.5") {
		t.Errorf("timescale body = %q, want scale=0.5", receivedBody)
	}
}

func TestDispatch_SimulateInput(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == apiInput {
			b, _ := io.ReadAll(r.Body)
			receivedBody = string(b)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	args := json.RawMessage(`{"body":"{\"key\":\"A\"}"}`)
	_, err := dispatchTool("simulate_input", args, srv.URL)
	if err != nil {
		t.Fatalf("dispatch simulate_input: %v", err)
	}
	if !strings.Contains(receivedBody, "key") {
		t.Errorf("simulate_input body = %q, want 'key'", receivedBody)
	}
}

func TestDispatch_SceneTree(t *testing.T) {
	srv := mockEngineServer(t, map[string]string{
		apiScene: `{"root":{"name":"scene"}}`,
	})
	defer srv.Close()

	res, err := dispatchTool("scene_tree", nil, srv.URL)
	if err != nil {
		t.Fatalf("dispatch scene_tree: %v", err)
	}
	text := extractText(t, res)
	if !strings.Contains(text, "root") {
		t.Errorf("scene_tree: unexpected text: %s", text)
	}
}

func TestDispatch_UnknownTool(t *testing.T) {
	_, err := dispatchTool("nonexistent", nil, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- encodeBase64 ---

func TestEncodeBase64_Empty(t *testing.T) {
	got := encodeBase64(nil)
	if got != "" {
		t.Errorf("encodeBase64(nil) = %q, want empty", got)
	}
}

func TestEncodeBase64_Simple(t *testing.T) {
	// "Man" → "TWFu" (RFC 4648 の例)
	got := encodeBase64([]byte("Man"))
	if got != "TWFu" {
		t.Errorf("encodeBase64('Man') = %q, want 'TWFu'", got)
	}
}

func TestEncodeBase64_Padding(t *testing.T) {
	// "Ma" → "TWE=" (パディング 1)
	got := encodeBase64([]byte("Ma"))
	if got != "TWE=" {
		t.Errorf("encodeBase64('Ma') = %q, want 'TWE='", got)
	}
	// "M" → "TQ==" (パディング 2)
	got = encodeBase64([]byte("M"))
	if got != "TQ==" {
		t.Errorf("encodeBase64('M') = %q, want 'TQ=='", got)
	}
}

// --- MCP JSON-RPC メッセージ処理 ---

func TestRPCRequest_Parse(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	var req rpcRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Method != "tools/list" {
		t.Errorf("method = %q, want tools/list", req.Method)
	}
}

// --- ヘルパー ---

// mockEngineServer は指定パスに固定レスポンス JSON を返す GET サーバー。
func mockEngineServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := routes[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// extractText は dispatchTool の返り値 ([]interface{}) から最初の text content を取り出す。
func extractText(t *testing.T, res interface{}) string {
	t.Helper()
	items, ok := res.([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected []interface{}, got %T", res)
	}
	tc, ok := items[0].(mcpTextContent)
	if !ok {
		t.Fatalf("expected mcpTextContent, got %T", items[0])
	}
	return tc.Text
}
