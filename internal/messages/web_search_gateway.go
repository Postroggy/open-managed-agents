package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

const (
	maxWebSearchResponseBytes = 8 << 20
	// searchToolName 是 Anthropic server tool 的协议名，只出现在面向调用方的
	// server_tool_use / web_search_tool_result block 中。
	searchToolName = "web_search"
	// upstreamSearchToolName 是 gateway 投影给 BYOK 的 ordinary tool 名。使用 OMA
	// 独占前缀是因为调用方可以合法地声明自己的 web_search 工具，复用该名字会让同一
	// 请求出现两个同名 tool，且无法再按名字判断 tool_use 归属。
	upstreamSearchToolName      = "oma_web_search"
	defaultServerToolIterations = 10
	searchErrorUnavailable      = "unavailable"
	searchErrorMaxUses          = "max_uses_exceeded"
)

type webSearchGateway struct {
	upstreamBaseURL         string
	upstreamAPIKey          string
	maxServerToolIterations int
	client                  *http.Client
	searcher                websearch.Provider
	logger                  *slog.Logger
}

type webSearchGatewayResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

type webSearchGatewayRequestError struct {
	cause error
}

func (e *webSearchGatewayRequestError) Error() string {
	return e.cause.Error()
}

func (e *webSearchGatewayRequestError) Unwrap() error {
	return e.cause
}

type webSearchGatewayRequest struct {
	fields   map[string]json.RawMessage
	messages []json.RawMessage
	stream   bool
}

type webSearchPreparedRequest struct {
	request          webSearchGatewayRequest
	upstreamFields   map[string]json.RawMessage
	transcript       []json.RawMessage
	projectedContent []json.RawMessage
	searchUses       int
	searchEnabled    bool
	searchPolicy     webSearchPolicy
}

type webSearchToolCall struct {
	id         string
	externalID string
	name       string
	input      json.RawMessage
	search     *webSearchInput
}

type webSearchExecution struct {
	call      webSearchToolCall
	results   websearch.SearchResponse
	err       error
	errorCode string
}

// serverToolUseID 返回该次执行面向调用方的 server tool ID。续传路径已带上调用方原始
// ID，首轮执行则从 BYOK 的 ordinary ID 铸造。
func (e webSearchExecution) serverToolUseID() (string, error) {
	if e.call.externalID != "" {
		return e.call.externalID, nil
	}
	return serverWebSearchToolUseID(e.call.id)
}

type webSearchInput struct {
	Query string `json:"query"`
}

type webSearchPolicy struct {
	MaxUses        int
	AllowedDomains []string
	BlockedDomains []string
}

type webSearchUserLocation struct {
	Type     string `json:"type"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Country  string `json:"country,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type webSearchContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	Text  string          `json:"text,omitempty"`
}

type webSearchToolResultBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content"`
}

type webSearchResultBlock struct {
	Type    string `json:"type,omitempty"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content,omitempty"`
	// EncryptedContent is opaque provider data. OMA-managed search leaves it empty.
	EncryptedContent string `json:"encrypted_content,omitempty"`
	PublishedDate    string `json:"published_date,omitempty"`
	PageAge          string `json:"page_age,omitempty"`
}

func newWebSearchGateway(cfg config.Config, client *http.Client, searcher websearch.Provider, logger *slog.Logger) *webSearchGateway {
	if client == nil {
		client = &http.Client{Transport: newProxyTransport()}
	}
	logger = logging.LoggerOrDefault(logger)
	return &webSearchGateway{
		upstreamBaseURL:         cfg.AnthropicUpstream.BaseURL,
		upstreamAPIKey:          cfg.AnthropicUpstream.APIKey,
		maxServerToolIterations: cfg.WebSearch.MaxServerToolIterations,
		client:                  client,
		searcher:                searcher,
		logger:                  logger,
	}
}

func (g *webSearchGateway) handle(ctx context.Context, body []byte, rawQuery string, headers http.Header) (webSearchGatewayResponse, bool, error) {
	prepared, handled, err := g.prepareRequest(ctx, body)
	if err != nil || !handled {
		return webSearchGatewayResponse{}, handled, err
	}
	request := prepared.request
	upstreamFields := prepared.upstreamFields
	transcript := prepared.transcript
	projectedContent := prepared.projectedContent
	searchUses := prepared.searchUses
	searchEnabled := prepared.searchEnabled
	searchPolicy := prepared.searchPolicy
	iterationLimit := g.serverToolIterationLimit()
	usage := &webSearchUsageAccumulator{}
	// iterationLimit 恒为正数，循环由下面的 pause_turn 分支退出，因此不需要循环条件。
	for iteration := 0; ; iteration++ {
		encodedMessages, err := json.Marshal(transcript)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode messages transcript: %w", err)
		}
		upstreamFields["messages"] = encodedMessages
		upstreamFields["stream"] = json.RawMessage("false")
		payload, err := json.Marshal(upstreamFields)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode upstream messages request: %w", err)
		}
		response, err := g.send(ctx, payload, rawQuery, headers)
		if err != nil {
			return webSearchGatewayResponse{}, true, err
		}
		if response.statusCode < http.StatusOK || response.statusCode >= http.StatusMultipleChoices {
			return response, true, nil
		}
		contentType := strings.ToLower(response.header.Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "application/json") {
			return webSearchGatewayResponse{}, true, errors.New("messages upstream returned a non-JSON response")
		}
		calls, err := extractWebSearchToolCalls(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("decode upstream messages response: %w", err)
		}
		content, err := webSearchResponseContent(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("decode upstream messages content: %w", err)
		}
		if err := usage.add(response.body); err != nil {
			return webSearchGatewayResponse{}, true, err
		}
		searchCalls := webSearchCalls(calls)
		if !searchEnabled && len(searchCalls) > 0 {
			return webSearchGatewayResponse{}, true, errors.New("messages upstream requested web search without a current tool definition")
		}
		if len(searchCalls) == 0 {
			projectedContent = append(projectedContent, content...)
			response, err = finalizeWebSearchResponse(response, projectedContent, request.stream, "", usage)
			return response, true, err
		}
		if len(searchCalls) != len(calls) {
			mixedContent, projectErr := projectPendingWebSearchContent(content)
			if projectErr != nil {
				return webSearchGatewayResponse{}, true, fmt.Errorf("project mixed tool response: %w", projectErr)
			}
			projectedContent = append(projectedContent, mixedContent...)
			response, err = finalizeWebSearchResponse(response, projectedContent, request.stream, "", usage)
			return response, true, err
		}
		assistantMessage, err := webSearchAssistantMessage(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode assistant messages transcript: %w", err)
		}
		transcript = append(transcript, assistantMessage)
		results, executions, nextSearchUses, err := g.executeSearchCalls(ctx, searchCalls, searchPolicy, searchUses)
		if err != nil {
			return webSearchGatewayResponse{}, true, err
		}
		completedContent, err := projectCompletedWebSearchContent(content, executions)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("project web search response: %w", err)
		}
		projectedContent = append(projectedContent, completedContent...)
		searchUses = nextSearchUses
		if iteration+1 >= iterationLimit {
			response, err = finalizeWebSearchResponse(response, projectedContent, request.stream, "pause_turn", usage)
			return response, true, err
		}
		userMessage, err := webSearchUserMessage(results)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode user messages transcript: %w", err)
		}
		transcript = append(transcript, userMessage)
	}
}

func (g *webSearchGateway) prepareRequest(ctx context.Context, body []byte) (webSearchPreparedRequest, bool, error) {
	if g == nil {
		return webSearchPreparedRequest{}, true, errors.New("messages web search gateway is not configured")
	}
	if int64(len(body)) > maxRequestBodyBytes {
		return webSearchPreparedRequest{}, true, errors.New("request body exceeds maximum size")
	}
	if g.searcher == nil {
		return webSearchPreparedRequest{}, false, nil
	}
	if g.client == nil {
		return webSearchPreparedRequest{}, true, errors.New("messages upstream client is not configured")
	}
	request, err := parseWebSearchRequest(body)
	if err != nil {
		return webSearchPreparedRequest{}, false, nil
	}
	_, hasTools := request.fields["tools"]
	searchEnabled := hasTools && hasWebSearchTool(request.fields["tools"])
	if !searchEnabled && !hasWebSearchHistory(request.messages) {
		return webSearchPreparedRequest{}, false, nil
	}
	if !searchEnabled {
		if isWebSearchPauseContinuation(request.messages) {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{
				cause: errors.New("pause_turn continuation requires the same web_search tool"),
			}
		}
		pending, pendingErr := findPendingWebSearchTurn(request.messages)
		if pendingErr != nil {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: pendingErr}
		}
		if pending != nil {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{
				cause: errors.New("pending web search continuation requires the same web_search tool"),
			}
		}
	}
	if strings.TrimSpace(g.upstreamAPIKey) == "" {
		return webSearchPreparedRequest{}, true, errors.New("messages upstream key is required")
	}
	upstreamFields, searchPolicy, err := projectWebSearchFields(request.fields)
	if err != nil {
		return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: fmt.Errorf("project messages request: %w", err)}
	}
	transcript, projectedContent, searchUses, err := g.prepareWebSearchTranscript(ctx, request.messages, searchPolicy)
	if err != nil {
		return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: fmt.Errorf("project messages transcript: %w", err)}
	}
	return webSearchPreparedRequest{
		request:          request,
		upstreamFields:   upstreamFields,
		transcript:       transcript,
		projectedContent: projectedContent,
		searchUses:       searchUses,
		searchEnabled:    searchEnabled,
		searchPolicy:     searchPolicy,
	}, true, nil
}

func (g *webSearchGateway) executeSearchCalls(ctx context.Context, calls []webSearchToolCall, policy webSearchPolicy, searchUses int) ([]json.RawMessage, []webSearchExecution, int, error) {
	results := make([]json.RawMessage, 0, len(calls))
	executions := make([]webSearchExecution, 0, len(calls))
	for _, call := range calls {
		if call.search == nil {
			return nil, nil, searchUses, fmt.Errorf("tool %q is not owned by the web search gateway", call.name)
		}
		if call.externalID == "" {
			externalID, err := serverWebSearchToolUseID(call.id)
			if err != nil {
				return nil, nil, searchUses, err
			}
			call.externalID = externalID
		}
		var searchErr error
		var errorCode string
		var result websearch.SearchResponse
		if policy.MaxUses > 0 && searchUses >= policy.MaxUses {
			searchErr = errors.New("web search max uses exceeded")
			errorCode = searchErrorMaxUses
		} else {
			searchUses++
			result, searchErr = g.search(ctx, call.search.Query, policy)
			if searchErr != nil {
				errorCode = searchErrorUnavailable
			}
		}
		if searchErr != nil {
			// 搜索失败会降级为 web_search_tool_result_error 继续本回合，调用方只看到
			// error_code；这里记录唯一一次可诊断的原因，不记录 query 等请求内容。
			g.logger.WarnContext(ctx, "web search execution degraded",
				"request_id", httpapi.RequestID(ctx),
				"error_code", errorCode,
				"error", searchErr,
			)
		}
		execution := webSearchExecution{call: call, results: result, err: searchErr, errorCode: errorCode}
		executions = append(executions, execution)
		toolResult, err := webSearchToolResult(execution)
		if err != nil {
			return nil, nil, searchUses, fmt.Errorf("encode web search tool result: %w", err)
		}
		results = append(results, toolResult)
	}
	return results, executions, searchUses, nil
}

func parseWebSearchRequest(body []byte) (webSearchGatewayRequest, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return webSearchGatewayRequest{}, fmt.Errorf("invalid JSON request body: %w", err)
	}
	var messages []json.RawMessage
	if raw := fields["messages"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return webSearchGatewayRequest{}, fmt.Errorf("messages must be an array: %w", err)
		}
	}
	var stream bool
	if raw := fields["stream"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return webSearchGatewayRequest{}, fmt.Errorf("stream must be a boolean: %w", err)
		}
	}
	return webSearchGatewayRequest{fields: fields, messages: messages, stream: stream}, nil
}

func hasWebSearchTool(raw json.RawMessage) bool {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return false
	}
	for _, rawTool := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			continue
		}
		if isServerWebSearchToolType(tool.Type) {
			return true
		}
	}
	return false
}

func projectWebSearchFields(fields map[string]json.RawMessage) (map[string]json.RawMessage, webSearchPolicy, error) {
	projected := cloneRawMap(fields)
	rawTools, ok := fields["tools"]
	if !ok {
		return projected, webSearchPolicy{}, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil, webSearchPolicy{}, fmt.Errorf("tools must be an array: %w", err)
	}
	projectedTools := make([]json.RawMessage, 0, len(tools))
	var searchPolicy webSearchPolicy
	foundSearchTool := false
	for _, rawTool := range tools {
		var tool webSearchTool
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			return nil, webSearchPolicy{}, fmt.Errorf("decode tool: %w", err)
		}
		if isServerWebSearchToolType(tool.Type) {
			if foundSearchTool {
				return nil, webSearchPolicy{}, errors.New("multiple web search tools are unsupported")
			}
			var err error
			searchPolicy, err = tool.searchPolicy()
			if err != nil {
				return nil, webSearchPolicy{}, err
			}
			foundSearchTool = true
			projectedTools = append(projectedTools, searchToolDefinition())
			continue
		}
		projectedTools = append(projectedTools, rawTool)
	}
	encodedTools, err := json.Marshal(projectedTools)
	if err != nil {
		return nil, webSearchPolicy{}, fmt.Errorf("encode tools: %w", err)
	}
	projected["tools"] = encodedTools
	if foundSearchTool {
		if err := projectWebSearchToolChoice(projected); err != nil {
			return nil, webSearchPolicy{}, err
		}
	}
	return projected, searchPolicy, nil
}

// projectWebSearchToolChoice keeps a forced server Web Search call aligned with
// the gateway-owned name sent to BYOK. Other tool_choice variants remain opaque.
func projectWebSearchToolChoice(fields map[string]json.RawMessage) error {
	rawChoice, ok := fields["tool_choice"]
	if !ok {
		return nil
	}
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(rawChoice, &choice); err != nil {
		return nil
	}
	var choiceType, name string
	if json.Unmarshal(choice["type"], &choiceType) != nil || json.Unmarshal(choice["name"], &name) != nil {
		return nil
	}
	if choiceType != "tool" || name != searchToolName {
		return nil
	}
	encodedName, err := json.Marshal(upstreamSearchToolName)
	if err != nil {
		return fmt.Errorf("encode projected tool choice name: %w", err)
	}
	choice["name"] = encodedName
	encodedChoice, err := json.Marshal(choice)
	if err != nil {
		return fmt.Errorf("encode projected tool choice: %w", err)
	}
	fields["tool_choice"] = encodedChoice
	return nil
}

type webSearchTool struct {
	Type              string                 `json:"type"`
	MaxUses           *int                   `json:"max_uses,omitempty"`
	AllowedDomains    []string               `json:"allowed_domains,omitempty"`
	BlockedDomains    []string               `json:"blocked_domains,omitempty"`
	AllowedCallers    []string               `json:"allowed_callers,omitempty"`
	ResponseInclusion string                 `json:"response_inclusion,omitempty"`
	UserLocation      *webSearchUserLocation `json:"user_location,omitempty"`
}

func (t webSearchTool) searchPolicy() (webSearchPolicy, error) {
	if err := t.validateDirectCaller(); err != nil {
		return webSearchPolicy{}, err
	}
	if err := t.validateResponseInclusion(); err != nil {
		return webSearchPolicy{}, err
	}
	if t.MaxUses != nil && *t.MaxUses <= 0 {
		return webSearchPolicy{}, errors.New("web search max_uses must be positive")
	}
	if len(t.AllowedDomains) > 0 && len(t.BlockedDomains) > 0 {
		return webSearchPolicy{}, errors.New("web search cannot include both allowed_domains and blocked_domains")
	}
	if t.UserLocation != nil {
		return webSearchPolicy{}, errors.New("web search user_location is unsupported by the configured provider")
	}
	policy := webSearchPolicy{
		AllowedDomains: append([]string(nil), t.AllowedDomains...),
		BlockedDomains: append([]string(nil), t.BlockedDomains...),
	}
	if t.MaxUses != nil {
		policy.MaxUses = *t.MaxUses
	}
	return policy, nil
}

func (t webSearchTool) validateDirectCaller() error {
	if t.AllowedCallers == nil {
		if t.Type == "web_search_20250305" {
			return nil
		}
		return errors.New(`web search allowed_callers must include "direct" for the configured BYOK model`)
	}
	direct := false
	for _, caller := range t.AllowedCallers {
		switch caller {
		case "direct":
			direct = true
		case "code_execution_20260120":
		default:
			return fmt.Errorf("unsupported web search allowed caller %q", caller)
		}
	}
	if !direct {
		return errors.New(`web search allowed_callers must include "direct" for the configured BYOK model`)
	}
	return nil
}

func (t webSearchTool) validateResponseInclusion() error {
	if t.ResponseInclusion == "" {
		return nil
	}
	if t.Type != "web_search_20260318" {
		return errors.New("web search response_inclusion requires web_search_20260318")
	}
	if t.ResponseInclusion != "full" && t.ResponseInclusion != "excluded" {
		return fmt.Errorf("unsupported web search response_inclusion %q", t.ResponseInclusion)
	}
	return nil
}

func isServerWebSearchToolType(value string) bool {
	switch value {
	case "web_search_20250305", "web_search_20260209", "web_search_20260318":
		return true
	default:
		return false
	}
}

func searchToolDefinition() json.RawMessage {
	return json.RawMessage(`{"name":"` + upstreamSearchToolName + `","description":"Search the public web and return relevant results.","input_schema":{"type":"object","properties":{"query":{"type":"string","description":"The web search query"}},"required":["query"]}}`)
}

func (g *webSearchGateway) send(ctx context.Context, body []byte, rawQuery string, headers http.Header) (webSearchGatewayResponse, error) {
	target, err := messagesEndpoint(g.upstreamBaseURL, rawQuery)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("build messages upstream endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("build messages upstream request: %w", err)
	}
	request.Header = sanitizedRequestHeaders(headers)
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-Api-Key", strings.TrimSpace(g.upstreamAPIKey))
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	// 透传调用方的 Accept-Encoding 会关闭 Transport 的透明解压，使 gateway 拿到未解压的
	// gzip 字节并在解析上游 JSON 时失败；这里交回 Transport 自行协商并解压。
	request.Header.Del("Accept-Encoding")
	response, err := g.client.Do(request)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("send messages upstream request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxWebSearchResponseBytes+1))
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("read messages upstream response: %w", err)
	}
	if len(responseBody) > maxWebSearchResponseBytes {
		return webSearchGatewayResponse{}, errors.New("messages upstream response exceeds maximum size")
	}
	return webSearchGatewayResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: responseBody}, nil
}

// serverToolIterationLimit 返回每条入站请求允许的 BYOK 采样次数，未配置时回落到与
// Anthropic 对齐的默认值。返回值恒为正数。
func (g *webSearchGateway) serverToolIterationLimit() int {
	if g.maxServerToolIterations > 0 {
		return g.maxServerToolIterations
	}
	return defaultServerToolIterations
}

func (g *webSearchGateway) search(ctx context.Context, query string, policy webSearchPolicy) (websearch.SearchResponse, error) {
	return g.searcher.Search(ctx, websearch.SearchRequest{
		Query: query,
		Options: websearch.SearchOptions{
			MaxResults:     5,
			IncludeDomains: append([]string(nil), policy.AllowedDomains...),
			ExcludeDomains: append([]string(nil), policy.BlockedDomains...),
		},
	})
}

func extractWebSearchToolCalls(body []byte) ([]webSearchToolCall, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Content == nil {
		return nil, errors.New("messages response content must be an array")
	}
	calls := []webSearchToolCall{}
	seenIDs := make(map[string]struct{})
	for _, rawBlock := range response.Content {
		var block webSearchContentBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode messages content block: %w", err)
		}
		if block.Type != "tool_use" {
			continue
		}
		if strings.TrimSpace(block.ID) == "" {
			return nil, errors.New("tool use id is required")
		}
		if _, duplicate := seenIDs[block.ID]; duplicate {
			return nil, fmt.Errorf("duplicate tool use id %q", block.ID)
		}
		seenIDs[block.ID] = struct{}{}
		call := webSearchToolCall{id: block.ID, name: block.Name, input: append(json.RawMessage(nil), block.Input...)}
		if block.Name != upstreamSearchToolName {
			calls = append(calls, call)
			continue
		}
		var input webSearchInput
		if len(block.Input) == 0 || json.Unmarshal(block.Input, &input) != nil {
			return nil, errors.New("web search tool input must be an object")
		}
		input.Query = strings.TrimSpace(input.Query)
		if input.Query == "" {
			return nil, errors.New("web search tool input query is required")
		}
		call.search = &input
		calls = append(calls, call)
	}
	return calls, nil
}

func webSearchAssistantMessage(body []byte) (json.RawMessage, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode assistant message: %w", err)
	}
	message := struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: "assistant", Content: response.Content}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal assistant message: %w", err)
	}
	return encoded, nil
}

func webSearchUserMessage(results []json.RawMessage) (json.RawMessage, error) {
	message := struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: "user", Content: results}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal user message: %w", err)
	}
	return encoded, nil
}

func webSearchToolResult(execution webSearchExecution) (json.RawMessage, error) {
	if execution.err != nil {
		message := `"web search unavailable"`
		if execution.errorCode == searchErrorMaxUses {
			message = `"web search max uses exceeded"`
		}
		return marshalWebSearchToolResult(webSearchToolResultBlock{
			Type: "tool_result", ToolUseID: execution.call.id, IsError: true,
			Content: json.RawMessage(message),
		})
	}
	content := make([]webSearchResultBlock, 0, len(execution.results.Results))
	for _, result := range execution.results.Results {
		content = append(content, resultToUpstreamContentItem(result))
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal web search results: %w", err)
	}
	return marshalWebSearchToolResult(webSearchToolResultBlock{Type: "tool_result", ToolUseID: execution.call.id, Content: encoded})
}

func marshalWebSearchToolResult(result webSearchToolResultBlock) (json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result: %w", err)
	}
	return encoded, nil
}

// resultToUpstreamContentItem 生成发给 BYOK 的 tool_result 条目。BYOK 侧是 ordinary
// tool，条目不带 Anthropic 的 web_search_result 类型标记，但保留正文供模型引用。
func resultToUpstreamContentItem(result websearch.Result) webSearchResultBlock {
	content := result.Snippet
	if result.Text != "" {
		content = result.Text
	}
	return webSearchResultBlock{
		Title:         result.Title,
		URL:           result.URL,
		Content:       content,
		PublishedDate: result.PublishedDate,
		PageAge:       result.PageAge,
	}
}

// resultToServerContentItem 生成面向调用方的 web_search_result 条目，只暴露标题、URL
// 与页面时间。
func resultToServerContentItem(result websearch.Result) webSearchResultBlock {
	return webSearchResultBlock{
		Type:    "web_search_result",
		Title:   result.Title,
		URL:     result.URL,
		PageAge: result.PageAge,
	}
}

func encodeWebSearchSSE(body []byte) ([]byte, error) {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	var content []json.RawMessage
	if err := json.Unmarshal(message["content"], &content); err != nil {
		return nil, errors.New("messages response content must be an array")
	}
	messageStart := cloneRawMap(message)
	messageStart["content"] = json.RawMessage("[]")
	messageStart["stop_reason"] = json.RawMessage("null")
	messageStart["stop_sequence"] = json.RawMessage("null")
	var output bytes.Buffer
	if err := writeWebSearchSSE(&output, "message_start", struct {
		Type    string                     `json:"type"`
		Message map[string]json.RawMessage `json:"message"`
	}{Type: "message_start", Message: messageStart}); err != nil {
		return nil, err
	}
	for index, rawBlock := range content {
		var block webSearchContentBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode messages content block: %w", err)
		}
		startBlock := append(json.RawMessage(nil), rawBlock...)
		var toolInput json.RawMessage
		if block.Type == "text" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rawBlock, &fields); err != nil {
				return nil, fmt.Errorf("decode text content block: %w", err)
			}
			fields["text"] = json.RawMessage(`""`)
			var err error
			startBlock, err = json.Marshal(fields)
			if err != nil {
				return nil, fmt.Errorf("marshal text content block: %w", err)
			}
		} else if block.Type == "tool_use" || block.Type == "server_tool_use" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rawBlock, &fields); err != nil {
				return nil, fmt.Errorf("decode tool content block: %w", err)
			}
			toolInput = append(json.RawMessage(nil), fields["input"]...)
			fields["input"] = json.RawMessage(`{}`)
			var err error
			startBlock, err = json.Marshal(fields)
			if err != nil {
				return nil, fmt.Errorf("marshal tool content block: %w", err)
			}
		}
		if err := writeWebSearchSSE(&output, "content_block_start", struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
		}{Type: "content_block_start", Index: index, ContentBlock: startBlock}); err != nil {
			return nil, err
		}
		if block.Text != "" {
			if err := writeWebSearchSSE(&output, "content_block_delta", struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}{Type: "content_block_delta", Index: index, Delta: struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text_delta", Text: block.Text}}); err != nil {
				return nil, err
			}
		} else if len(toolInput) > 0 {
			if err := writeWebSearchSSE(&output, "content_block_delta", struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}{Type: "content_block_delta", Index: index, Delta: struct {
				Type        string `json:"type"`
				PartialJSON string `json:"partial_json"`
			}{Type: "input_json_delta", PartialJSON: string(toolInput)}}); err != nil {
				return nil, err
			}
		}
		if err := writeWebSearchSSE(&output, "content_block_stop", struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: "content_block_stop", Index: index}); err != nil {
			return nil, err
		}
	}
	if err := writeWebSearchSSE(&output, "message_delta", struct {
		Type  string `json:"type"`
		Delta struct {
			StopReason   json.RawMessage `json:"stop_reason"`
			StopSequence json.RawMessage `json:"stop_sequence"`
		} `json:"delta"`
		Usage json.RawMessage `json:"usage"`
	}{Type: "message_delta", Delta: struct {
		StopReason   json.RawMessage `json:"stop_reason"`
		StopSequence json.RawMessage `json:"stop_sequence"`
	}{StopReason: message["stop_reason"], StopSequence: message["stop_sequence"]}, Usage: message["usage"]}); err != nil {
		return nil, err
	}
	if err := writeWebSearchSSE(&output, "message_stop", struct {
		Type string `json:"type"`
	}{Type: "message_stop"}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeWebSearchSSE(output *bytes.Buffer, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", event, err)
	}
	output.WriteString("event: " + event + "\n")
	output.WriteString("data: ")
	output.Write(data)
	output.WriteString("\n\n")
	return nil
}

func cloneRawMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	clone := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}
