package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var mcpPort int

func newMCPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) サーバーを stdio で起動する",
		Long: `stdio で JSON-RPC 2.0 (MCP protocol v2024-11-05) を話すサーバーを起動する。
AI ツール (Claude Code など) からゲームの状態取得・操作を行える。

エンジン側は MITIRU_AI=1 で起動している必要がある。
ポートは --port フラグ、または環境変数 MITIRU_AI_PORT で指定する (既定: 8090)。

提供するツール:
  game_state       GET /api/ai/state       — GameMemory の構造化 JSON
  state_diff       GET /api/ai/diff        — 直前フレームとの差分 JSON
  screenshot       GET /api/screenshot     — PNG を base64 で返す
  simulate_input   POST /api/input/simulate — 入力イベントを送る
  pause            POST /api/runtime/pause  — ポーズ切り替え
  step             POST /api/runtime/step   — 1フレーム進める
  timescale        POST /api/runtime/timescale — 時間スケール変更
  scene_tree       GET /api/scene/tree      — シーンツリー JSON
  verify           ローカルで verify を実行し結果を返す

Usage (Claude Code の .mcp.json に追加):
  {"mcpServers":{"mitiru":{"command":"mitiru","args":["mcp"]}}}`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP()
		},
	}
	cmd.Flags().IntVar(&mcpPort, "port", 0,
		"エンジン HTTP API のポート番号 (既定: $MITIRU_AI_PORT, なければ 8090)")
	return cmd
}

// mcpBaseURL は接続先の base URL を解決する。
func mcpBaseURL() string {
	port := mcpPort
	if port == 0 {
		if env := os.Getenv("MITIRU_AI_PORT"); env != "" {
			if p, err := strconv.Atoi(env); err == nil && p > 0 {
				port = p
			}
		}
	}
	if port == 0 {
		port = 8090
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// --- JSON-RPC 2.0 の最小型定義 ---

type rpcRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP プロトコル固定応答 ---

const mcpProtocolVersion = "2024-11-05"

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpInitResult struct {
	ProtocolVersion string        `json:"protocolVersion"`
	ServerInfo      mcpServerInfo `json:"serverInfo"`
	Capabilities    struct {
		Tools struct{} `json:"tools"`
	} `json:"capabilities"`
}

// mcpTool は tools/list が返す 1 ツールのスキーマ。
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

var mcpTools = []mcpTool{
	{
		Name:        "game_state",
		Description: "GameMemory の構造化 JSON を返す (GET /api/ai/state)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "state_diff",
		Description: "直前フレームとの差分 JSON を返す (GET /api/ai/diff)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "screenshot",
		Description: "現在のゲーム画面を base64 PNG で返す (GET /api/screenshot)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"width":{"type":"integer"},"height":{"type":"integer"}}}`),
	},
	{
		Name:        "simulate_input",
		Description: "入力イベントを送る (POST /api/input/simulate)。body は JSON",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"body":{"type":"string"}},"required":["body"]}`),
	},
	{
		Name:        "pause",
		Description: "ポーズ切り替え (POST /api/runtime/pause)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "step",
		Description: "1フレーム進める (POST /api/runtime/step)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "timescale",
		Description: "時間スケールを変更する (POST /api/runtime/timescale)。scale は float",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"scale":{"type":"number"}},"required":["scale"]}`),
	},
	{
		Name:        "scene_tree",
		Description: "シーンツリー JSON を返す (GET /api/scene/tree)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "verify",
		Description: "ローカルプロジェクトをビルド・起動して verify を実行し、JSON 結果を返す",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"frames":{"type":"integer"},"golden":{"type":"string"}}}`),
	},
}

// --- MCP tools/call ディスパッチ ---

// mcpCallParams は tools/call の params フィールド。
type mcpCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// mcpTextContent は MCP content type=text の応答。
type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// mcpImageContent は MCP content type=image の応答。
type mcpImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

// dispatchTool は tools/call のディスパッチ。result は interface{} として返す。
func dispatchTool(name string, argsRaw json.RawMessage, baseURL string) (interface{}, error) {
	switch name {
	case "game_state":
		return simpleGetTool(baseURL, apiState)
	case "state_diff":
		return simpleGetTool(baseURL, apiDiff)
	case "screenshot":
		return screenshotTool(baseURL, argsRaw)
	case "simulate_input":
		return simulateInputTool(baseURL, argsRaw)
	case "pause":
		return simplePostTool(baseURL, apiPause)
	case "step":
		return simplePostTool(baseURL, apiStep)
	case "timescale":
		return timescaleTool(baseURL, argsRaw)
	case "scene_tree":
		return simpleGetTool(baseURL, apiScene)
	case "verify":
		return verifyTool(argsRaw)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func simpleGetTool(baseURL, path string) (interface{}, error) {
	body, status, err := apiGet(baseURL, path)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d from %s", status, path)
	}
	return []interface{}{mcpTextContent{Type: "text", Text: string(body)}}, nil
}

func simplePostTool(baseURL, path string) (interface{}, error) {
	body, status, err := apiPost(baseURL, path, nil, "")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d from POST %s", status, path)
	}
	text := string(body)
	if strings.TrimSpace(text) == "" {
		text = "ok"
	}
	return []interface{}{mcpTextContent{Type: "text", Text: text}}, nil
}

func screenshotTool(baseURL string, argsRaw json.RawMessage) (interface{}, error) {
	var args struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if len(argsRaw) > 0 {
		_ = json.Unmarshal(argsRaw, &args)
	}
	// エンジン側は width/height 片方指定でもアスペクト維持リサイズする。
	path := apiShot
	switch {
	case args.Width > 0 && args.Height > 0:
		path = fmt.Sprintf("%s?width=%d&height=%d", apiShot, args.Width, args.Height)
	case args.Width > 0:
		path = fmt.Sprintf("%s?width=%d", apiShot, args.Width)
	case args.Height > 0:
		path = fmt.Sprintf("%s?height=%d", apiShot, args.Height)
	}
	body, status, err := apiGet(baseURL, path)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d from %s", status, path)
	}
	// PNG をそのまま base64 エンコードして image content として返す。
	import64 := encodeBase64(body)
	return []interface{}{mcpImageContent{
		Type:     "image",
		Data:     import64,
		MIMEType: "image/png",
	}}, nil
}

func simulateInputTool(baseURL string, argsRaw json.RawMessage) (interface{}, error) {
	var args struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return nil, fmt.Errorf("simulate_input: parse args: %w", err)
	}
	if args.Body == "" {
		return nil, fmt.Errorf("simulate_input: body is required")
	}
	body, status, err := apiPost(baseURL, apiInput, strings.NewReader(args.Body), "application/json")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d from POST %s", status, apiInput)
	}
	return []interface{}{mcpTextContent{Type: "text", Text: string(body)}}, nil
}

func timescaleTool(baseURL string, argsRaw json.RawMessage) (interface{}, error) {
	var args struct {
		Scale float64 `json:"scale"`
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return nil, fmt.Errorf("timescale: parse args: %w", err)
	}
	payload := fmt.Sprintf(`{"scale":%g}`, args.Scale)
	body, status, err := apiPost(baseURL, apiTimescale, strings.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d from POST %s", status, apiTimescale)
	}
	return []interface{}{mcpTextContent{Type: "text", Text: string(body)}}, nil
}

// verifyTool は現在のプロジェクトを verify し、JSON 文字列を content として返す。
// CLI の stdout/stderr は MCP レスポンスに織り込めないので、
// 一時的に stdout を capture してから返す。
func verifyTool(argsRaw json.RawMessage) (interface{}, error) {
	var args struct {
		Frames int    `json:"frames"`
		Golden string `json:"golden"`
	}
	if len(argsRaw) > 0 {
		_ = json.Unmarshal(argsRaw, &args)
	}

	// フラグをリセットして verify を実行する。
	old := captureVerifyFlags(args.Frames, args.Golden)
	defer restoreVerifyFlags(old)

	// stdout を一時 buffer に向け、verify の JSON 出力を取得する。
	origOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("verify tool: pipe: %w", err)
	}
	os.Stdout = w

	runErr := runVerify()

	w.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()

	text := buf.String()
	if runErr != nil && text == "" {
		text = fmt.Sprintf(`{"verdict":"fail","buildErr":%q}`, runErr.Error())
	}
	return []interface{}{mcpTextContent{Type: "text", Text: text}}, nil
}

type savedVerifyFlags struct {
	frames          int
	golden          string
	goldenThreshold float64
	json            bool
}

func captureVerifyFlags(frames int, golden string) savedVerifyFlags {
	saved := savedVerifyFlags{
		frames:          verifyFrames,
		golden:          verifyGolden,
		goldenThreshold: verifyGoldenThreshold,
		json:            verifyJSON,
	}
	if frames > 0 {
		verifyFrames = frames
	} else {
		verifyFrames = 300
	}
	verifyGolden = golden
	verifyGoldenThreshold = 0.95
	verifyJSON = true
	return saved
}

func restoreVerifyFlags(s savedVerifyFlags) {
	verifyFrames = s.frames
	verifyGolden = s.golden
	verifyGoldenThreshold = s.goldenThreshold
	verifyJSON = s.json
}

// encodeBase64 は stdlib の encoding/base64 を使わずに済むよう import を抑える。
// encoding/base64 は既に import されていないので直接 fmt.Sprintf でラップする。
func encodeBase64(data []byte) string {
	// encoding/base64 を使う。import は mcp.go のみで完結させる。
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var b strings.Builder
	for i := 0; i < len(data); i += 3 {
		var block [3]byte
		n := copy(block[:], data[i:])
		b.WriteByte(chars[block[0]>>2])
		b.WriteByte(chars[(block[0]&0x3)<<4|block[1]>>4])
		if n > 1 {
			b.WriteByte(chars[(block[1]&0xf)<<2|block[2]>>6])
		} else {
			b.WriteByte('=')
		}
		if n > 2 {
			b.WriteByte(chars[block[2]&0x3f])
		} else {
			b.WriteByte('=')
		}
	}
	return b.String()
}

// --- メインの stdio ループ ---

func runMCP() error {
	baseURL := mcpBaseURL()
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// パース不能なリクエストは parse error で返す。
			_ = enc.Encode(rpcResponse{
				Jsonrpc: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}

		var resp rpcResponse
		resp.Jsonrpc = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = mcpInitResult{
				ProtocolVersion: mcpProtocolVersion,
				ServerInfo:      mcpServerInfo{Name: "mitiru", Version: cliVersion},
			}

		case "initialized":
			// 通知 (id なし)。応答不要。
			continue

		case "tools/list":
			resp.Result = map[string]interface{}{"tools": mcpTools}

		case "tools/call":
			var params mcpCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				resp.Error = &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
				break
			}
			result, err := dispatchTool(params.Name, params.Arguments, baseURL)
			if err != nil {
				resp.Error = &rpcError{Code: -32603, Message: err.Error()}
			} else {
				resp.Result = map[string]interface{}{"content": result}
			}

		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		}

		_ = enc.Encode(resp)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("mcp: stdin: %w", err)
	}
	return nil
}
