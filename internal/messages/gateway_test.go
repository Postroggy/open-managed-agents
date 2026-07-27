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

func TestGatewayToolLoopLimit(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t, `{"type":"message","content":[{"type":"tool_use","id":"toolu_loop","name":"web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`)
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxToolLoops: 1}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{}, nil)
	response, handled, err := gateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if !handled || err == nil || response.body != nil {
		t.Fatalf("response = %#v, handled = %v, err = %v; want bounded loop error", response, handled, err)
	}
	if requestCount() != 1 {
		t.Fatalf("upstream requests = %d, want exactly 1", requestCount())
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

func TestGatewayMixedToolUseReturnsUnsupportedToolResult(t *testing.T) {
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"tool_use","id":"toolu_search","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_other","name":"bash","input":{"command":"pwd"}}],"stop_reason":"tool_use"}`)
			return
		}
		var request struct {
			Messages []struct {
				Role    string            `json:"role"`
				Content []json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode continuation: %v", err)
		}
		last := request.Messages[len(request.Messages)-1]
		if len(last.Content) != 2 {
			t.Fatalf("continuation results = %d, want 2", len(last.Content))
		}
		if !strings.Contains(string(last.Content[0]), `"tool_use_id":"toolu_search"`) ||
			!strings.Contains(string(last.Content[1]), `"tool_use_id":"toolu_other"`) {
			t.Fatalf("continuation results = %s", last.Content)
		}
		if !strings.Contains(string(last.Content[1]), `"is_error":true`) {
			t.Fatalf("unsupported tool result = %s, want is_error=true", last.Content[1])
		}
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}}]}`)
	response, handled, err := gateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK || requestCount != 2 {
		t.Fatalf("response = %#v, handled = %v, err = %v, requests = %d", response, handled, err, requestCount)
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
	if !strings.Contains(string(response.body), "web_search_tool_result_error") {
		t.Fatalf("max_uses response = %s, want an error result for the second call", response.body)
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
			_, _ = io.WriteString(w, "{\"id\":\"msg_tool\",\"type\":\"message\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"web_search\",\"input\":{\"query\":\"golang release\"}}],\"stop_reason\":\"tool_use\"}")
			return
		}
		_, _ = io.WriteString(w, "{\"id\":\"msg_final\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}")
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Snippet: "release"}}}}
	cfg := config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"},
		WebSearch: config.WebSearchConfig{
			Provider:     "tavily",
			MaxToolLoops: 2,
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
	if content[0].(map[string]any)["type"] != "server_tool_use" || content[1].(map[string]any)["type"] != "web_search_tool_result" {
		t.Fatalf("projected content = %#v", content)
	}
}

func TestGatewaySSEResponse(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_sse","name":"web_search","input":{"query":"golang release"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{}}`,
	)
	searcher := &gatewayTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Snippet: "release"}}}}
	gateway := newGateway(config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}, WebSearch: config.WebSearchConfig{MaxToolLoops: 2}}, &http.Client{Timeout: time.Second}, searcher, nil)
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
}
