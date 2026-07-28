package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

type gatewayTestSearcher struct {
	queries  []string
	requests []websearch.SearchRequest
	results  websearch.SearchResponse
	err      error
	panic    bool
}

func (s *gatewayTestSearcher) Search(_ context.Context, request websearch.SearchRequest) (websearch.SearchResponse, error) {
	s.queries = append(s.queries, request.Query)
	s.requests = append(s.requests, request)
	if s.panic {
		panic("provider failure")
	}
	return s.results, s.err
}

func newSequencedUpstream(t *testing.T, responses ...string) (*httptest.Server, func() int) {
	t.Helper()
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(requestCount.Add(1)) - 1
		if index >= len(responses) {
			t.Errorf("unexpected upstream request %d", index+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responses[index])
	}))
	t.Cleanup(upstream.Close)
	return upstream, func() int { return int(requestCount.Load()) }
}

func TestGatewayMalformedRequestIsTransparent(t *testing.T) {
	gateway := newGateway(config.Config{}, nil, &gatewayTestSearcher{}, nil)
	_, handled, err := gateway.handle(context.Background(), []byte("{"), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want transparent fallback", handled, err)
	}
}

func TestGatewayWithoutProviderIsTransparent(t *testing.T) {
	gateway := newGateway(config.Config{}, nil, nil, nil)
	_, handled, err := gateway.handle(context.Background(), []byte("{\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want transparent fallback", handled, err)
	}
}

func TestGatewayLiteralWebSearchTypeIsTransparent(t *testing.T) {
	gateway := newGateway(config.Config{}, nil, &gatewayTestSearcher{}, nil)
	_, handled, err := gateway.handle(context.Background(), []byte(`{"tools":[{"type":"web_search","name":"web_search"}]}`), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want undocumented literal type to pass through", handled, err)
	}
}

func TestGatewayPauseContinuationRequiresCurrentSearchTool(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"search"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfcGF1c2U","name":"web_search","input":{"query":"query"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_oma_dG9vbHVfcGF1c2U","content":[]}]}]}`)
	_, handled, err := gateway.handle(context.Background(), body, "", nil)
	var requestErr *gatewayRequestError
	if !handled || !errors.As(err, &requestErr) || !strings.Contains(err.Error(), "same web_search tool") {
		t.Fatalf("handled = %v, err = %v; want invalid pause continuation error", handled, err)
	}
	if requestCount.Load() != 0 || len(searcher.requests) != 0 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 0 and 0", requestCount.Load(), len(searcher.requests))
	}
}

func TestGatewayServerToolIterationLimitReturnsPauseTurn(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t, `{"id":"msg_paused","type":"message","content":[{"type":"tool_use","id":"toolu_loop","name":"web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`)
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	response, handled, err := gateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v; want pause_turn response", response, handled, err)
	}
	if requestCount() != 1 {
		t.Fatalf("upstream requests = %d, want exactly 1", requestCount())
	}
	if len(searcher.requests) != 1 {
		t.Fatalf("search requests = %d, want completed search before pause", len(searcher.requests))
	}
	var decoded struct {
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode pause response: %v", err)
	}
	if decoded.StopReason != "pause_turn" || len(decoded.Content) != 2 ||
		!strings.Contains(string(decoded.Content[0]), `"type":"server_tool_use"`) ||
		!strings.Contains(string(decoded.Content[1]), `"type":"web_search_tool_result"`) {
		t.Fatalf("pause response = %s", response.body)
	}
}

func TestGatewayPauseTurnContinuationReplaysCompletedSearch(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_paused","type":"message","content":[{"type":"tool_use","id":"toolu_pause","name":"web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := json.RawMessage(`[{"type":"web_search_20250305","name":"web_search","max_uses":1}]`)
	firstBody, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "search"}},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("encode first request: %v", err)
	}
	first, handled, err := gateway.handle(context.Background(), firstBody, "", nil)
	if err != nil || !handled || first.statusCode != http.StatusOK {
		t.Fatalf("first response = %#v, handled = %v, err = %v", first, handled, err)
	}
	var paused struct {
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(first.body, &paused); err != nil || paused.StopReason != "pause_turn" {
		t.Fatalf("decode paused response: body=%s err=%v", first.body, err)
	}
	secondBody, err := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "search"},
			map[string]any{"role": "assistant", "content": paused.Content},
		},
		"tools": tools,
	})
	if err != nil {
		t.Fatalf("encode continuation request: %v", err)
	}
	second, handled, err := gateway.handle(context.Background(), secondBody, "", nil)
	if err != nil || !handled || second.statusCode != http.StatusOK || !strings.Contains(string(second.body), `"text":"done"`) {
		t.Fatalf("continuation response = %#v, handled = %v, err = %v", second, handled, err)
	}
	if requestCount() != 2 || len(searcher.requests) != 1 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 2 and 1", requestCount(), len(searcher.requests))
	}
}

func TestGatewayServerResultUsesOfficialShapeAndRestoresBYOKContent(t *testing.T) {
	execution := gatewayExecution{
		call: gatewayToolCall{id: "toolu_search"},
		results: websearch.SearchResponse{Results: []websearch.Result{{
			Title: "Result", URL: "https://example.com", Snippet: "search snippet", PageAge: "July 28, 2026",
		}}},
	}
	result, err := gatewayWebSearchResultBlock(execution)
	if err != nil {
		t.Fatalf("build server result: %v", err)
	}
	if !strings.Contains(string(result), `"encrypted_content":"`) ||
		strings.Contains(string(result), `"content":"search snippet"`) {
		t.Fatalf("server result does not use the official opaque content shape: %s", result)
	}

	var block gatewayProtocolBlock
	if err := json.Unmarshal(result, &block); err != nil {
		t.Fatalf("decode server result: %v", err)
	}
	projected, err := projectServerResultToClient(block)
	if err != nil {
		t.Fatalf("project server result: %v", err)
	}
	if !strings.Contains(string(projected), `"content":"search snippet"`) {
		t.Fatalf("BYOK result lost restored search content: %s", projected)
	}
}

func TestGatewayServerResultRejectsMissingOrModifiedOpaqueContent(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "missing", content: `[{"type":"web_search_result","title":"Result","url":"https://example.com"}]`},
		{name: "modified", content: `[{"type":"web_search_result","title":"Result","url":"https://example.com","encrypted_content":"oma_search_v1_invalid"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := projectServerResultToClient(gatewayProtocolBlock{
				Type: "web_search_tool_result", ToolUseID: serverGatewayToolUseID("toolu_search"), Content: json.RawMessage(test.content),
			})
			if err == nil || !strings.Contains(err.Error(), "encrypted_content") {
				t.Fatalf("project result error = %v, want encrypted_content validation error", err)
			}
		})
	}
}

func TestGatewayPauseTurnContinuationExecutesPendingSearch(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"search"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfcGF1c2U","name":"web_search","input":{"query":"query"}}]}],"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":1}]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 1 || len(searcher.requests) != 1 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 1 and 1", requestCount(), len(searcher.requests))
	}
	if !strings.Contains(string(response.body), `"tool_use_id":"srvtoolu_oma_dG9vbHVfcGF1c2U"`) ||
		!strings.Contains(string(response.body), `"text":"done"`) {
		t.Fatalf("pending pause continuation response = %s", response.body)
	}
}

func TestGatewayPauseTurnCanRepeat(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_pause_1","type":"message","content":[{"type":"tool_use","id":"toolu_pause_1","name":"web_search","input":{"query":"first"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_pause_2","type":"message","content":[{"type":"tool_use","id":"toolu_pause_2","name":"web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := json.RawMessage(`[{"type":"web_search_20250305","name":"web_search","max_uses":2}]`)
	firstBody, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "search"}},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("encode first request: %v", err)
	}
	first, handled, err := gateway.handle(context.Background(), firstBody, "", nil)
	if err != nil || !handled || first.statusCode != http.StatusOK {
		t.Fatalf("first response = %#v, handled = %v, err = %v", first, handled, err)
	}
	var firstPause struct {
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(first.body, &firstPause); err != nil || firstPause.StopReason != "pause_turn" {
		t.Fatalf("decode first pause: body=%s err=%v", first.body, err)
	}
	secondBody, err := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "search"},
			map[string]any{"role": "assistant", "content": firstPause.Content},
		},
		"tools": tools,
	})
	if err != nil {
		t.Fatalf("encode second request: %v", err)
	}
	second, handled, err := gateway.handle(context.Background(), secondBody, "", nil)
	if err != nil || !handled || second.statusCode != http.StatusOK {
		t.Fatalf("second response = %#v, handled = %v, err = %v", second, handled, err)
	}
	var secondPause struct {
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(second.body, &secondPause); err != nil || secondPause.StopReason != "pause_turn" {
		t.Fatalf("decode second pause: body=%s err=%v", second.body, err)
	}
	if requestCount() != 2 || len(searcher.requests) != 2 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 2 and 2", requestCount(), len(searcher.requests))
	}
}

func TestGatewayServerToolIterationLimitStreamsPauseTurn(t *testing.T) {
	upstream, _ := newSequencedUpstream(t, `{"id":"msg_paused","type":"message","content":[{"type":"tool_use","id":"toolu_stream_pause","name":"web_search","input":{"query":"query"}}],"stop_reason":"tool_use","usage":{}}`)
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	response, handled, err := gateway.handle(context.Background(), []byte(`{"stream":true,"messages":[],"tools":[{"type":"web_search_20250305"}]}`), "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	for _, want := range []string{
		`"type":"server_tool_use"`,
		`"type":"web_search_tool_result"`,
		`"stop_reason":"pause_turn"`,
		"event: message_stop",
	} {
		if !strings.Contains(string(response.body), want) {
			t.Fatalf("pause stream missing %q: %s", want, response.body)
		}
	}
}

func TestGatewayUpstreamFailureIsPassedThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "{\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"try later\"}}")
	}))
	defer upstream.Close()
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{}, nil)
	response, handled, err := gateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	wantBody := "{\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"try later\"}}"
	if err != nil || !handled || response.statusCode != http.StatusTooManyRequests || string(response.body) != wantBody {
		t.Fatalf("response = %#v, handled = %v, err = %v; want original upstream error", response, handled, err)
	}
}

func TestGatewayProviderFailureBecomesToolError(t *testing.T) {
	upstream, _ := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &gatewayTestSearcher{err: errors.New("provider unavailable")}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := gateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	content := decoded["content"].([]any)
	result := content[1].(map[string]any)
	if result["type"] != "web_search_tool_result" || !strings.Contains(string(response.body), "unavailable") {
		t.Fatalf("provider error response = %s", response.body)
	}
}

func TestGatewayProviderPanicBecomesToolError(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &gatewayTestSearcher{panic: true}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, logger)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	ctx := httpapi.WithRequestID(context.Background(), "req_gateway_panic")
	response, handled, err := gateway.handle(ctx, body, "", http.Header{})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 2 {
		t.Fatalf("upstream requests = %d, want 2", requestCount())
	}
	if !strings.Contains(string(response.body), "web_search_tool_result_error") {
		t.Fatalf("panic response = %s", response.body)
	}
	if !strings.Contains(logs.String(), `"request_id":"req_gateway_panic"`) || !strings.Contains(logs.String(), `"stack"`) {
		t.Fatalf("panic log = %s", logs.String())
	}
}

func TestExtractGatewayToolCallsRejectsDuplicateIDs(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_duplicate","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_duplicate","name":"bash","input":{"command":"pwd"}}]}`)

	_, err := extractGatewayToolCalls(body)
	if err == nil || !strings.Contains(err.Error(), `duplicate tool use id "toolu_duplicate"`) {
		t.Fatalf("extract tool calls error = %v, want duplicate id error", err)
	}
}

func TestGatewayMixedContinuationRequiresCurrentSearchTool(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"search and inspect"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfc2VhcmNo","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}]}],"tools":[{"name":"bash","input_schema":{"type":"object"}}]}`)
	_, handled, err := gateway.handle(context.Background(), body, "", nil)
	var requestErr *gatewayRequestError
	if !handled || !errors.As(err, &requestErr) || !strings.Contains(err.Error(), "same web_search tool") {
		t.Fatalf("handled = %v, err = %v; want invalid continuation error", handled, err)
	}
	if requestCount.Load() != 0 || len(searcher.requests) != 0 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 0 and 0", requestCount.Load(), len(searcher.requests))
	}
}

func TestGatewayMixedContinuationRejectsDuplicateClientResults(t *testing.T) {
	messages := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"search and inspect"}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfc2VhcmNo","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]}`),
		json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"},{"type":"tool_result","tool_use_id":"toolu_bash","content":"duplicate"}]}`),
	}

	_, err := findPendingGatewayTurn(messages)
	if err == nil || !strings.Contains(err.Error(), `duplicate client tool result "toolu_bash"`) {
		t.Fatalf("find pending turn error = %v, want duplicate client result error", err)
	}
}

func TestGatewayMixedContinuationRejectsNonToolResultContent(t *testing.T) {
	messages := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"search and inspect"}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfc2VhcmNo","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]}`),
		json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"},{"type":"text","text":"continue"}]}`),
	}

	_, err := findPendingGatewayTurn(messages)
	if err == nil || !strings.Contains(err.Error(), "only tool_result blocks") {
		t.Fatalf("find pending turn error = %v, want non-tool-result content error", err)
	}
}

func TestGatewayClientToolUsePassesToClaudeCode(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}}]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 1 || len(searcher.requests) != 0 {
		t.Fatalf("upstream requests = %d, searches = %d; want 1 and 0", requestCount(), len(searcher.requests))
	}
	if !strings.Contains(string(response.body), `"id":"toolu_bash"`) || strings.Contains(string(response.body), "unsupported tool use") {
		t.Fatalf("client tool response = %s", response.body)
	}
}
func TestGatewayMixedToolUseDefersSearchUntilClientResults(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"toolu_search_1","name":"web_search","input":{"query":"first query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}},{"type":"tool_use","id":"toolu_search_2","name":"web_search","input":{"query":"second query"}},{"type":"tool_use","id":"toolu_read","name":"read_file","input":{"path":"README.md"}}],"stop_reason":"tool_use"}`)
			return
		}
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode continuation: %v", err)
			return
		}
		if len(request.Messages) != 3 {
			t.Errorf("continuation messages = %d, want 3", len(request.Messages))
			return
		}
		assistant := request.Messages[1]
		var assistantContent []json.RawMessage
		if err := json.Unmarshal(assistant.Content, &assistantContent); err != nil {
			t.Errorf("decode assistant content: %v", err)
			return
		}
		if len(assistantContent) != 5 ||
			!strings.Contains(string(assistantContent[1]), `"id":"toolu_search_1"`) ||
			!strings.Contains(string(assistantContent[2]), `"id":"toolu_bash"`) ||
			!strings.Contains(string(assistantContent[3]), `"id":"toolu_search_2"`) ||
			!strings.Contains(string(assistantContent[4]), `"id":"toolu_read"`) ||
			strings.Contains(string(assistant.Content), "server_tool_use") {
			t.Errorf("projected assistant message = %s", assistant.Content)
			return
		}
		last := request.Messages[2]
		var lastContent []json.RawMessage
		if err := json.Unmarshal(last.Content, &lastContent); err != nil {
			t.Errorf("decode continuation results: %v", err)
			return
		}
		if len(lastContent) != 4 {
			t.Errorf("continuation results = %d, want 4", len(lastContent))
			return
		}
		if !strings.Contains(string(lastContent[0]), `"tool_use_id":"toolu_search_1"`) ||
			!strings.Contains(string(lastContent[1]), `"tool_use_id":"toolu_bash"`) ||
			!strings.Contains(string(lastContent[2]), `"tool_use_id":"toolu_search_2"`) ||
			!strings.Contains(string(lastContent[3]), `"tool_use_id":"toolu_read"`) {
			t.Errorf("continuation results = %s", lastContent)
			return
		}
		if strings.Contains(string(lastContent[1]), `"is_error":true`) || strings.Contains(string(lastContent[3]), `"is_error":true`) {
			t.Errorf("client tool results were replaced: %s", lastContent)
			return
		}
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := json.RawMessage(`[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}},{"name":"read_file","input_schema":{"type":"object"}}]`)
	body := []byte(`{"messages":[{"role":"user","content":"search and inspect"}],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}},{"name":"read_file","input_schema":{"type":"object"}}]}`)
	first, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || first.statusCode != http.StatusOK || requestCount.Load() != 1 {
		t.Fatalf("first response = %#v, handled = %v, err = %v, requests = %d", first, handled, err, requestCount.Load())
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("searches before client result = %d, want 0", len(searcher.requests))
	}
	var firstMessage struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
	}
	if err := json.Unmarshal(first.body, &firstMessage); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstMessage.StopReason != "tool_use" || len(firstMessage.Content) != 5 {
		t.Fatalf("first response = %s", first.body)
	}
	serverCalls := make([]gatewayContentBlock, 0, 2)
	for _, index := range []int{1, 3} {
		var serverCall gatewayContentBlock
		if err := json.Unmarshal(firstMessage.Content[index], &serverCall); err != nil {
			t.Fatalf("decode server tool use %d: %v", index, err)
		}
		if serverCall.Type != "server_tool_use" || !strings.HasPrefix(serverCall.ID, "srvtoolu_") {
			t.Fatalf("server tool use %d = %s", index, firstMessage.Content[index])
		}
		serverCalls = append(serverCalls, serverCall)
	}
	if !strings.Contains(string(firstMessage.Content[2]), `"id":"toolu_bash"`) ||
		!strings.Contains(string(firstMessage.Content[4]), `"id":"toolu_read"`) ||
		strings.Contains(string(first.body), "web_search_tool_result") {
		t.Fatalf("mixed response = %s", first.body)
	}
	assistant, err := json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: "assistant", Content: firstMessage.Content})
	if err != nil {
		t.Fatalf("encode assistant continuation: %v", err)
	}
	clientResult := json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"readme"},{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}]}`)
	followUp, err := json.Marshal(map[string]any{
		"messages": []json.RawMessage{json.RawMessage(`{"role":"user","content":"search and inspect"}`), assistant, clientResult},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("encode follow-up request: %v", err)
	}
	second, handled, err := gateway.handle(context.Background(), followUp, "", nil)
	if err != nil || !handled || second.statusCode != http.StatusOK || requestCount.Load() != 2 {
		t.Fatalf("second response = %#v, handled = %v, err = %v, requests = %d", second, handled, err, requestCount.Load())
	}
	if len(searcher.requests) != 2 || searcher.requests[0].Query != "first query" || searcher.requests[1].Query != "second query" {
		t.Fatalf("deferred search requests = %#v", searcher.requests)
	}
	var secondMessage struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(second.body, &secondMessage); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if len(secondMessage.Content) != 3 || !strings.Contains(string(secondMessage.Content[0]), `"type":"web_search_tool_result"`) ||
		!strings.Contains(string(secondMessage.Content[0]), `"tool_use_id":"`+serverCalls[0].ID+`"`) ||
		!strings.Contains(string(secondMessage.Content[1]), `"tool_use_id":"`+serverCalls[1].ID+`"`) ||
		strings.Contains(string(second.body), "server_tool_use") {
		t.Fatalf("resumed mixed response = %s", second.body)
	}
}

func TestGatewayProjectsCompletedSearchHistoryBackToBYOK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(request.Messages) != 5 {
			t.Errorf("projected history messages = %d, want 5", len(request.Messages))
			return
		}
		if request.Messages[1].Role != "assistant" || !strings.Contains(string(request.Messages[1].Content), `"type":"tool_use"`) ||
			!strings.Contains(string(request.Messages[1].Content), `"id":"toolu_history"`) {
			t.Errorf("projected search call = %s", request.Messages[1].Content)
			return
		}
		if request.Messages[2].Role != "user" || !strings.Contains(string(request.Messages[2].Content), `"type":"tool_result"`) ||
			!strings.Contains(string(request.Messages[2].Content), `"tool_use_id":"toolu_history"`) {
			t.Errorf("projected search result = %s", request.Messages[2].Content)
			return
		}
		if request.Messages[3].Role != "assistant" || !strings.Contains(string(request.Messages[3].Content), `"text":"old answer"`) {
			t.Errorf("projected final content = %s", request.Messages[3].Content)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"new answer"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	encryptedContent := gatewayTestEncryptedContent(t, "old result")
	body := []byte(`{"messages":[{"role":"user","content":"old search"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfaGlzdG9yeQ","name":"web_search","input":{"query":"old query"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_oma_dG9vbHVfaGlzdG9yeQ","content":[{"type":"web_search_result","title":"Old","url":"https://example.com","encrypted_content":"` + encryptedContent + `"}]},{"type":"text","text":"old answer"}]},{"role":"user","content":"new question"}],"tools":[]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("history replay searches = %d, want 0", len(searcher.requests))
	}
}

func TestGatewayProjectsCompletedMixedHistoryBackToBYOK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(request.Messages) != 5 {
			t.Errorf("projected mixed history messages = %d, want 5", len(request.Messages))
			return
		}
		var calls []json.RawMessage
		if err := json.Unmarshal(request.Messages[1].Content, &calls); err != nil || len(calls) != 2 {
			t.Errorf("projected mixed calls = %s, err = %v", request.Messages[1].Content, err)
			return
		}
		if !strings.Contains(string(calls[0]), `"id":"toolu_history_search"`) ||
			!strings.Contains(string(calls[1]), `"id":"toolu_history_bash"`) {
			t.Errorf("projected mixed calls = %s", calls)
			return
		}
		var results []json.RawMessage
		if err := json.Unmarshal(request.Messages[2].Content, &results); err != nil || len(results) != 2 {
			t.Errorf("projected mixed results = %s, err = %v", request.Messages[2].Content, err)
			return
		}
		if !strings.Contains(string(results[0]), `"tool_use_id":"toolu_history_search"`) ||
			!strings.Contains(string(results[1]), `"tool_use_id":"toolu_history_bash"`) {
			t.Errorf("projected mixed result order = %s", results)
			return
		}
		if request.Messages[3].Role != "assistant" || !strings.Contains(string(request.Messages[3].Content), `"text":"old answer"`) {
			t.Errorf("projected mixed answer = %s", request.Messages[3].Content)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"new answer"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	encryptedContent := gatewayTestEncryptedContent(t, "old result")
	body := []byte(`{"messages":[{"role":"user","content":"old mixed turn"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_dG9vbHVfaGlzdG9yeV9zZWFyY2g","name":"web_search","input":{"query":"old query"}},{"type":"tool_use","id":"toolu_history_bash","name":"bash","input":{"command":"pwd"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_history_bash","content":"/workspace"}]},{"role":"assistant","content":[{"type":"web_search_tool_result","tool_use_id":"srvtoolu_oma_dG9vbHVfaGlzdG9yeV9zZWFyY2g","content":[{"type":"web_search_result","title":"Old","url":"https://example.com","encrypted_content":"` + encryptedContent + `"}]},{"type":"text","text":"old answer"}]},{"role":"user","content":"new question"}],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}}]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("completed mixed history searches = %d, want 0", len(searcher.requests))
	}
}

func gatewayTestEncryptedContent(t *testing.T, content string) string {
	t.Helper()
	encryptedContent, err := encodeGatewaySearchContent(content, "")
	if err != nil {
		t.Fatalf("encode test search content: %v", err)
	}
	return encryptedContent
}

func TestProjectCompletedGatewayContentUsesResolvedServerID(t *testing.T) {
	content := []json.RawMessage{
		json.RawMessage(`{"type":"tool_use","id":"toolu_fallback","name":"web_search","input":{"query":"query"}}`),
	}
	executions := []gatewayExecution{{
		call: gatewayToolCall{id: "toolu_fallback", name: searchToolName},
		results: websearch.SearchResponse{Results: []websearch.Result{{
			Title: "Result", URL: "https://example.com", Snippet: "snippet",
		}}},
	}}

	projected, err := projectCompletedGatewayContent(content, executions)
	if err != nil {
		t.Fatalf("project completed content: %v", err)
	}
	if len(projected) != 2 {
		t.Fatalf("projected blocks = %d, want 2", len(projected))
	}
	var serverUse, searchResult gatewayProtocolBlock
	if err := json.Unmarshal(projected[0], &serverUse); err != nil {
		t.Fatalf("decode server tool use: %v", err)
	}
	if err := json.Unmarshal(projected[1], &searchResult); err != nil {
		t.Fatalf("decode search result: %v", err)
	}
	expectedID := serverGatewayToolUseID("toolu_fallback")
	if serverUse.ID != expectedID || searchResult.ToolUseID != expectedID {
		t.Fatalf("server use ID = %q, result ID = %q; want %q", serverUse.ID, searchResult.ToolUseID, expectedID)
	}
}

func TestGatewayEnforcesCallerMaxUses(t *testing.T) {
	upstream, _ := newSequencedUpstream(t,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"web_search","input":{"query":"first"}},{"type":"tool_use","id":"toolu_2","name":"web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
		`{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305","max_uses":1}]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Query != "first" {
		t.Fatalf("provider requests = %#v, want only the first search", searcher.requests)
	}
	var decoded struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode max_uses response: %v", err)
	}
	if len(decoded.Content) != 5 {
		t.Fatalf("max_uses content = %s", response.body)
	}
	for _, index := range []int{0, 2} {
		var call gatewayContentBlock
		if err := json.Unmarshal(decoded.Content[index], &call); err != nil {
			t.Fatalf("decode server call %d: %v", index, err)
		}
		if call.Type != "server_tool_use" || !strings.HasPrefix(call.ID, "srvtoolu_") {
			t.Fatalf("server call %d = %s", index, decoded.Content[index])
		}
		var result gatewayProtocolBlock
		if err := json.Unmarshal(decoded.Content[index+1], &result); err != nil {
			t.Fatalf("decode server result %d: %v", index+1, err)
		}
		if result.ToolUseID != call.ID {
			t.Fatalf("server call/result ids = %q/%q", call.ID, result.ToolUseID)
		}
	}
	if !strings.Contains(string(decoded.Content[3]), `"error_code":"max_uses_exceeded"`) {
		t.Fatalf("max_uses error = %s", decoded.Content[3])
	}
}

func TestGatewayMaxUsesSpansInternalContinuations(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"web_search","input":{"query":"first"}}],"stop_reason":"tool_use"}`,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_2","name":"web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
		`{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &gatewayTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305","max_uses":1}]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 3 {
		t.Fatalf("BYOK requests = %d, want 3", requestCount())
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Query != "first" {
		t.Fatalf("provider requests = %#v, want only the first search", searcher.requests)
	}
	var decoded struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode max_uses continuation response: %v", err)
	}
	if len(decoded.Content) != 5 || !strings.Contains(string(decoded.Content[3]), `"error_code":"max_uses_exceeded"`) {
		t.Fatalf("max_uses continuation response = %s", response.body)
	}
}

func TestGatewayRejectsUnsupportedCallerLocation(t *testing.T) {
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	defer upstream.Close()
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{}, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305","user_location":{"type":"approximate","country":"US"}}]}`)
	_, handled, err := gateway.handle(context.Background(), body, "", nil)
	if !handled || err == nil || !strings.Contains(err.Error(), "user_location is unsupported") {
		t.Fatalf("handled = %v, err = %v, want explicit unsupported user_location error", handled, err)
	}
	if requestCount != 0 {
		t.Fatalf("upstream requests = %d, want 0", requestCount)
	}
}

func TestGatewayRejectsUnsupportedSearchCallersBeforeUpstream(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{name: "legacy explicit code execution", tool: `{"type":"web_search_20250305","allowed_callers":["code_execution_20260120"]}`},
		{name: "dynamic filtering default", tool: `{"type":"web_search_20260209"}`},
		{name: "latest dynamic filtering default", tool: `{"type":"web_search_20260318"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requestCount atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestCount.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"unexpected"}],"stop_reason":"end_turn"}`)
			}))
			defer upstream.Close()
			cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
			gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{}, nil)
			body := []byte(`{"messages":[],"tools":[` + test.tool + `]}`)
			_, handled, err := gateway.handle(context.Background(), body, "", nil)
			if !handled || err == nil || !strings.Contains(err.Error(), `allowed_callers must include "direct"`) {
				t.Fatalf("handled = %v, err = %v; want direct-caller validation error", handled, err)
			}
			if requestCount.Load() != 0 {
				t.Fatalf("upstream requests = %d, want 0", requestCount.Load())
			}
		})
	}
}

func TestGatewayRejectsInvalidResponseInclusionBeforeUpstream(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{name: "unsupported version", tool: `{"type":"web_search_20260209","allowed_callers":["direct"],"response_inclusion":"excluded"}`},
		{name: "unsupported value", tool: `{"type":"web_search_20260318","allowed_callers":["direct"],"response_inclusion":"summary"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requestCount atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requestCount.Add(1)
			}))
			defer upstream.Close()
			cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
			gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{}, nil)
			body := []byte(`{"messages":[],"tools":[` + test.tool + `]}`)
			_, handled, err := gateway.handle(context.Background(), body, "", nil)
			if !handled || err == nil || !strings.Contains(err.Error(), "response_inclusion") {
				t.Fatalf("handled = %v, err = %v; want response_inclusion validation error", handled, err)
			}
			if requestCount.Load() != 0 {
				t.Fatalf("upstream requests = %d, want 0", requestCount.Load())
			}
		})
	}
}

func TestGatewayAcceptsDirectCallerForDynamicSearchVersion(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{name: "dynamic version", tool: `{"type":"web_search_20260209","allowed_callers":["direct"]}`},
		{name: "latest direct exclusion is still full", tool: `{"type":"web_search_20260318","allowed_callers":["direct"],"response_inclusion":"excluded"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream, requestCount := newSequencedUpstream(t, `{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
			cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
			gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{}, nil)
			body := []byte(`{"messages":[],"tools":[` + test.tool + `]}`)
			response, handled, err := gateway.handle(context.Background(), body, "", nil)
			if err != nil || !handled || response.statusCode != http.StatusOK || requestCount() != 1 {
				t.Fatalf("response = %#v, handled = %v, err = %v, requests = %d", response, handled, err, requestCount())
			}
		})
	}
}

func TestGatewayToolLoopProjectsTranscript(t *testing.T) {
	var requests []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			_, _ = io.WriteString(w, "{\"id\":\"msg_tool\",\"type\":\"message\",\"content\":[{\"type\":\"text\",\"text\":\"looking\"},{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"web_search\",\"input\":{\"query\":\"golang release\"}}],\"stop_reason\":\"tool_use\"}")
			return
		}
		_, _ = io.WriteString(w, "{\"id\":\"msg_final\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}")
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Snippet: "release"}}}}
	cfg := config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"},
		WebSearch: config.WebSearchConfig{
			Provider:                "tavily",
			MaxServerToolIterations: 2,
			Providers: map[string]config.WebSearchProviderConfig{
				"tavily": {APIKey: "tavily-key"},
			},
		},
	}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[{\"role\":\"user\",\"content\":\"search\"}],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\",\"max_uses\":1,\"allowed_domains\":[\"go.dev\"]}]}")
	response, handled, err := gateway.handle(context.Background(), body, "beta=true", http.Header{"Anthropic-Version": []string{"2023-06-01"}})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(requests) != 2 || len(searcher.queries) != 1 || searcher.queries[0] != "golang release" {
		t.Fatalf("requests = %d, queries = %#v", len(requests), searcher.queries)
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Options.MaxResults != 5 {
		t.Fatalf("search requests = %#v", searcher.requests)
	}
	if len(searcher.requests[0].Options.IncludeDomains) != 1 || searcher.requests[0].Options.IncludeDomains[0] != "go.dev" {
		t.Fatalf("search domain options = %#v", searcher.requests[0].Options)
	}
	encodedFirstRequest, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatalf("marshal first request: %v", err)
	}
	if strings.Contains(string(encodedFirstRequest), "tavily-key") {
		t.Fatal("Tavily API key reached the BYOK request")
	}
	tools, ok := requests[0]["tools"].([]any)
	if !ok || len(tools) != 1 || tools[0].(map[string]any)["name"] != searchToolName {
		t.Fatalf("projected tools = %#v", requests[0]["tools"])
	}
	if strings.Contains(string(encodedFirstRequest), "allowed_domains") || strings.Contains(string(encodedFirstRequest), "max_uses") {
		t.Fatalf("caller search policy leaked into model-controlled tool input: %s", encodedFirstRequest)
	}
	messages, ok := requests[1]["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("continuation messages = %#v", requests[1]["messages"])
	}
	var final map[string]any
	if err := json.Unmarshal(response.body, &final); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	content := final["content"].([]any)
	if len(content) != 4 || content[0].(map[string]any)["text"] != "looking" ||
		content[1].(map[string]any)["type"] != "server_tool_use" ||
		content[2].(map[string]any)["type"] != "web_search_tool_result" ||
		content[3].(map[string]any)["text"] != "answer" {
		t.Fatalf("projected content = %#v", content)
	}
}

func TestGatewaySSEResponse(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_sse","name":"web_search","input":{"query":"golang release"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{}}`,
	)
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Snippet: "release"}}}}
	gateway := newGateway(config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 2}}, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"stream\":true,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := gateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled || !strings.Contains(string(response.body), "event: message_start") || !strings.Contains(string(response.body), "event: message_stop") {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if got := response.header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := response.header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := response.header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	if requestCount() != 2 || len(searcher.requests) != 1 {
		t.Fatalf("continuation requests = %d, searches = %d; want 2 and 1", requestCount(), len(searcher.requests))
	}
	for _, want := range []string{"server_tool_use", "web_search_tool_result", "web_search_result", "event: content_block_start"} {
		if !strings.Contains(string(response.body), want) {
			t.Fatalf("SSE response = %s, missing %q", response.body, want)
		}
	}
	if !strings.Contains(string(response.body), `"type":"input_json_delta"`) || !strings.Contains(string(response.body), `"partial_json":"{\"query\":\"golang release\"}"`) {
		t.Fatalf("SSE response missing server tool input delta: %s", response.body)
	}
}
