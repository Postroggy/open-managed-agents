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
	"runtime/debug"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

const (
	maxGatewayResponseBytes     = 8 << 20
	searchToolName              = "web_search"
	defaultServerToolIterations = 10
	searchErrorUnavailable      = "unavailable"
	searchErrorMaxUses          = "max_uses_exceeded"
)

type gateway struct {
	upstreamBaseURL         string
	upstreamAPIKey          string
	maxServerToolIterations int
	client                  *http.Client
	searcher                websearch.Provider
	logger                  *slog.Logger
}

type gatewayResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

type gatewayRequestError struct {
	cause error
}

func (e *gatewayRequestError) Error() string {
	return e.cause.Error()
}

func (e *gatewayRequestError) Unwrap() error {
	return e.cause
}

type gatewayRequest struct {
	fields   map[string]json.RawMessage
	messages []json.RawMessage
	stream   bool
}

type gatewayPreparedRequest struct {
	request          gatewayRequest
	upstreamFields   map[string]json.RawMessage
	transcript       []json.RawMessage
	projectedContent []json.RawMessage
	searchUses       int
	searchEnabled    bool
	searchPolicy     gatewaySearchPolicy
}

type gatewayToolCall struct {
	id         string
	externalID string
	name       string
	input      json.RawMessage
	search     *gatewaySearchInput
}

type gatewayExecution struct {
	call      gatewayToolCall
	results   websearch.SearchResponse
	err       error
	errorCode string
}

type gatewaySearchInput struct {
	Query string `json:"query"`
}

type gatewaySearchPolicy struct {
	MaxUses        int
	AllowedDomains []string
	BlockedDomains []string
}

type gatewayUserLocation struct {
	Type     string `json:"type"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Country  string `json:"country,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type gatewayContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	Text  string          `json:"text,omitempty"`
}

type gatewayToolResultBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content"`
}

type gatewaySearchResultBlock struct {
	Type    string `json:"type,omitempty"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content,omitempty"`
	// EncryptedContent is opaque provider data. OMA-managed search leaves it empty.
	EncryptedContent string `json:"encrypted_content,omitempty"`
	PublishedDate    string `json:"published_date,omitempty"`
	PageAge          string `json:"page_age,omitempty"`
}

func newGateway(cfg config.Config, client *http.Client, searcher websearch.Provider, logger *slog.Logger) *gateway {
	if client == nil {
		client = &http.Client{Transport: newProxyTransport()}
	}
	logger = logging.LoggerOrDefault(logger)
	return &gateway{
		upstreamBaseURL:         cfg.AnthropicUpstream.BaseURL,
		upstreamAPIKey:          cfg.AnthropicUpstream.APIKey,
		maxServerToolIterations: cfg.WebSearch.MaxServerToolIterations,
		client:                  client,
		searcher:                searcher,
		logger:                  logger,
	}
}

func (g *gateway) handle(ctx context.Context, body []byte, rawQuery string, headers http.Header) (gatewayResponse, bool, error) {
	prepared, handled, err := g.prepareRequest(ctx, body)
	if err != nil || !handled {
		return gatewayResponse{}, handled, err
	}
	request := prepared.request
	upstreamFields := prepared.upstreamFields
	transcript := prepared.transcript
	projectedContent := prepared.projectedContent
	searchUses := prepared.searchUses
	searchEnabled := prepared.searchEnabled
	searchPolicy := prepared.searchPolicy
	iterationLimit := gatewayServerToolIterationLimit(g)
	for iteration := 0; iteration < iterationLimit; iteration++ {
		encodedMessages, err := json.Marshal(transcript)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode messages transcript: %w", err)
		}
		upstreamFields["messages"] = encodedMessages
		upstreamFields["stream"] = json.RawMessage("false")
		payload, err := json.Marshal(upstreamFields)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode upstream messages request: %w", err)
		}
		response, err := g.send(ctx, payload, rawQuery, headers)
		if err != nil {
			return gatewayResponse{}, true, err
		}
		if response.statusCode < http.StatusOK || response.statusCode >= http.StatusMultipleChoices {
			return response, true, nil
		}
		contentType := strings.ToLower(response.header.Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "application/json") {
			return gatewayResponse{}, true, errors.New("messages upstream returned a non-JSON response")
		}
		calls, err := extractGatewayToolCalls(response.body)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("decode upstream messages response: %w", err)
		}
		content, err := gatewayResponseContent(response.body)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("decode upstream messages content: %w", err)
		}
		searchCalls := gatewaySearchCalls(calls)
		if !searchEnabled && len(searchCalls) > 0 {
			return gatewayResponse{}, true, errors.New("messages upstream requested web search without a current tool definition")
		}
		if len(searchCalls) == 0 {
			projectedContent = append(projectedContent, content...)
			response, err = finalizeGatewayResponse(response, projectedContent, request.stream, "")
			return response, true, err
		}
		if len(searchCalls) != len(calls) {
			mixedContent, projectErr := projectPendingGatewayContent(content)
			if projectErr != nil {
				return gatewayResponse{}, true, fmt.Errorf("project mixed tool response: %w", projectErr)
			}
			projectedContent = append(projectedContent, mixedContent...)
			response, err = finalizeGatewayResponse(response, projectedContent, request.stream, "")
			return response, true, err
		}
		assistantMessage, err := assistantGatewayMessage(response.body)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode assistant messages transcript: %w", err)
		}
		transcript = append(transcript, assistantMessage)
		results, executions, nextSearchUses, err := g.executeSearchCalls(ctx, searchCalls, searchPolicy, searchUses)
		if err != nil {
			return gatewayResponse{}, true, err
		}
		completedContent, err := projectCompletedGatewayContent(content, executions)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("project web search response: %w", err)
		}
		projectedContent = append(projectedContent, completedContent...)
		searchUses = nextSearchUses
		if iteration+1 >= iterationLimit {
			response, err = finalizeGatewayResponse(response, projectedContent, request.stream, "pause_turn")
			return response, true, err
		}
		userMessage, err := userGatewayMessage(results)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode user messages transcript: %w", err)
		}
		transcript = append(transcript, userMessage)
	}
	return gatewayResponse{}, true, errors.New("web search tool loop exceeded maximum iterations")
}

func (g *gateway) prepareRequest(ctx context.Context, body []byte) (gatewayPreparedRequest, bool, error) {
	if g == nil {
		return gatewayPreparedRequest{}, true, errors.New("messages web search gateway is not configured")
	}
	if int64(len(body)) > maxRequestBodyBytes {
		return gatewayPreparedRequest{}, true, errors.New("request body exceeds maximum size")
	}
	if g.searcher == nil {
		return gatewayPreparedRequest{}, false, nil
	}
	if g.client == nil {
		return gatewayPreparedRequest{}, true, errors.New("messages upstream client is not configured")
	}
	request, err := parseGatewayRequest(body)
	if err != nil {
		return gatewayPreparedRequest{}, false, nil
	}
	_, hasTools := request.fields["tools"]
	searchEnabled := hasTools && hasWebSearchTool(request.fields["tools"])
	if !searchEnabled && !hasGatewayWebSearchHistory(request.messages) {
		return gatewayPreparedRequest{}, false, nil
	}
	if !searchEnabled {
		if isGatewayPauseContinuation(request.messages) {
			return gatewayPreparedRequest{}, true, &gatewayRequestError{
				cause: errors.New("pause_turn continuation requires the same web_search tool"),
			}
		}
		pending, pendingErr := findPendingGatewayTurn(request.messages)
		if pendingErr != nil {
			return gatewayPreparedRequest{}, true, &gatewayRequestError{cause: pendingErr}
		}
		if pending != nil {
			return gatewayPreparedRequest{}, true, &gatewayRequestError{
				cause: errors.New("pending web search continuation requires the same web_search tool"),
			}
		}
	}
	if strings.TrimSpace(g.upstreamAPIKey) == "" {
		return gatewayPreparedRequest{}, true, errors.New("messages upstream key is required")
	}
	upstreamFields, searchPolicy, err := projectGatewayFields(request.fields)
	if err != nil {
		return gatewayPreparedRequest{}, true, &gatewayRequestError{cause: fmt.Errorf("project messages request: %w", err)}
	}
	transcript, projectedContent, searchUses, err := g.prepareGatewayTranscript(ctx, request.messages, searchPolicy)
	if err != nil {
		return gatewayPreparedRequest{}, true, &gatewayRequestError{cause: fmt.Errorf("project messages transcript: %w", err)}
	}
	return gatewayPreparedRequest{
		request:          request,
		upstreamFields:   upstreamFields,
		transcript:       transcript,
		projectedContent: projectedContent,
		searchUses:       searchUses,
		searchEnabled:    searchEnabled,
		searchPolicy:     searchPolicy,
	}, true, nil
}

func (g *gateway) executeSearchCalls(ctx context.Context, calls []gatewayToolCall, policy gatewaySearchPolicy, searchUses int) ([]json.RawMessage, []gatewayExecution, int, error) {
	results := make([]json.RawMessage, 0, len(calls))
	executions := make([]gatewayExecution, 0, len(calls))
	for _, call := range calls {
		if call.search == nil {
			return nil, nil, searchUses, fmt.Errorf("tool %q is not owned by the web search gateway", call.name)
		}
		if call.externalID == "" {
			call.externalID = serverGatewayToolUseID(call.id)
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
		execution := gatewayExecution{call: call, results: result, err: searchErr, errorCode: errorCode}
		executions = append(executions, execution)
		toolResult, err := gatewayToolResult(execution)
		if err != nil {
			return nil, nil, searchUses, fmt.Errorf("encode web search tool result: %w", err)
		}
		results = append(results, toolResult)
	}
	return results, executions, searchUses, nil
}

func parseGatewayRequest(body []byte) (gatewayRequest, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return gatewayRequest{}, fmt.Errorf("invalid JSON request body: %w", err)
	}
	var messages []json.RawMessage
	if raw := fields["messages"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return gatewayRequest{}, fmt.Errorf("messages must be an array: %w", err)
		}
	}
	var stream bool
	if raw := fields["stream"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return gatewayRequest{}, fmt.Errorf("stream must be a boolean: %w", err)
		}
	}
	return gatewayRequest{fields: fields, messages: messages, stream: stream}, nil
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

func projectGatewayFields(fields map[string]json.RawMessage) (map[string]json.RawMessage, gatewaySearchPolicy, error) {
	projected := cloneRawMap(fields)
	rawTools, ok := fields["tools"]
	if !ok {
		return projected, gatewaySearchPolicy{}, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil, gatewaySearchPolicy{}, fmt.Errorf("tools must be an array: %w", err)
	}
	projectedTools := make([]json.RawMessage, 0, len(tools))
	var searchPolicy gatewaySearchPolicy
	foundSearchTool := false
	for _, rawTool := range tools {
		var tool gatewaySearchTool
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			return nil, gatewaySearchPolicy{}, fmt.Errorf("decode tool: %w", err)
		}
		if isServerWebSearchToolType(tool.Type) {
			if foundSearchTool {
				return nil, gatewaySearchPolicy{}, errors.New("multiple web search tools are unsupported")
			}
			var err error
			searchPolicy, err = tool.searchPolicy()
			if err != nil {
				return nil, gatewaySearchPolicy{}, err
			}
			foundSearchTool = true
			projectedTools = append(projectedTools, searchToolDefinition())
			continue
		}
		projectedTools = append(projectedTools, rawTool)
	}
	encodedTools, err := json.Marshal(projectedTools)
	if err != nil {
		return nil, gatewaySearchPolicy{}, fmt.Errorf("encode tools: %w", err)
	}
	projected["tools"] = encodedTools
	return projected, searchPolicy, nil
}

type gatewaySearchTool struct {
	Type              string               `json:"type"`
	MaxUses           *int                 `json:"max_uses,omitempty"`
	AllowedDomains    []string             `json:"allowed_domains,omitempty"`
	BlockedDomains    []string             `json:"blocked_domains,omitempty"`
	AllowedCallers    []string             `json:"allowed_callers,omitempty"`
	ResponseInclusion string               `json:"response_inclusion,omitempty"`
	UserLocation      *gatewayUserLocation `json:"user_location,omitempty"`
}

func (t gatewaySearchTool) searchPolicy() (gatewaySearchPolicy, error) {
	if err := t.validateDirectCaller(); err != nil {
		return gatewaySearchPolicy{}, err
	}
	if err := t.validateResponseInclusion(); err != nil {
		return gatewaySearchPolicy{}, err
	}
	if t.MaxUses != nil && *t.MaxUses <= 0 {
		return gatewaySearchPolicy{}, errors.New("web search max_uses must be positive")
	}
	if len(t.AllowedDomains) > 0 && len(t.BlockedDomains) > 0 {
		return gatewaySearchPolicy{}, errors.New("web search cannot include both allowed_domains and blocked_domains")
	}
	if t.UserLocation != nil {
		return gatewaySearchPolicy{}, errors.New("web search user_location is unsupported by the configured provider")
	}
	policy := gatewaySearchPolicy{
		AllowedDomains: append([]string(nil), t.AllowedDomains...),
		BlockedDomains: append([]string(nil), t.BlockedDomains...),
	}
	if t.MaxUses != nil {
		policy.MaxUses = *t.MaxUses
	}
	return policy, nil
}

func (t gatewaySearchTool) validateDirectCaller() error {
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

func (t gatewaySearchTool) validateResponseInclusion() error {
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
	return json.RawMessage(`{"name":"web_search","description":"Search the public web and return relevant results.","input_schema":{"type":"object","properties":{"query":{"type":"string","description":"The web search query"}},"required":["query"]}}`)
}

func (g *gateway) send(ctx context.Context, body []byte, rawQuery string, headers http.Header) (gatewayResponse, error) {
	target, err := messagesEndpoint(g.upstreamBaseURL, rawQuery)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build messages upstream endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build messages upstream request: %w", err)
	}
	request.Header = sanitizedRequestHeaders(headers)
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-Api-Key", strings.TrimSpace(g.upstreamAPIKey))
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := g.client.Do(request)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("send messages upstream request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponseBytes+1))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("read messages upstream response: %w", err)
	}
	if len(responseBody) > maxGatewayResponseBytes {
		return gatewayResponse{}, errors.New("messages upstream response exceeds maximum size")
	}
	return gatewayResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: responseBody}, nil
}

func gatewayServerToolIterationLimit(g *gateway) int {
	if g.maxServerToolIterations > 0 {
		return g.maxServerToolIterations
	}
	return defaultServerToolIterations
}

func (g *gateway) search(ctx context.Context, query string, policy gatewaySearchPolicy) (results websearch.SearchResponse, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			g.logger.ErrorContext(ctx, "web search provider panic",
				"request_id", httpapi.RequestID(ctx),
				"panic_type", fmt.Sprintf("%T", recovered),
				"stack", string(debug.Stack()),
			)
			results = websearch.SearchResponse{}
			err = errors.New("web search provider panicked")
		}
	}()
	return g.searcher.Search(ctx, websearch.SearchRequest{
		Query: query,
		Options: websearch.SearchOptions{
			MaxResults:     5,
			IncludeDomains: append([]string(nil), policy.AllowedDomains...),
			ExcludeDomains: append([]string(nil), policy.BlockedDomains...),
		},
	})
}

func extractGatewayToolCalls(body []byte) ([]gatewayToolCall, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Content == nil {
		return nil, errors.New("messages response content must be an array")
	}
	calls := []gatewayToolCall{}
	seenIDs := make(map[string]struct{})
	for _, rawBlock := range response.Content {
		var block gatewayContentBlock
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
		call := gatewayToolCall{id: block.ID, name: block.Name, input: append(json.RawMessage(nil), block.Input...)}
		if block.Name != searchToolName {
			calls = append(calls, call)
			continue
		}
		var input gatewaySearchInput
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

func assistantGatewayMessage(body []byte) (json.RawMessage, error) {
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

func userGatewayMessage(results []json.RawMessage) (json.RawMessage, error) {
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

func gatewayToolResult(execution gatewayExecution) (json.RawMessage, error) {
	if execution.err != nil {
		message := `"web search unavailable"`
		if execution.errorCode == searchErrorMaxUses {
			message = `"web search max uses exceeded"`
		}
		return marshalGatewayToolResult(gatewayToolResultBlock{
			Type: "tool_result", ToolUseID: execution.call.id, IsError: true,
			Content: json.RawMessage(message),
		})
	}
	content := make([]gatewaySearchResultBlock, 0, len(execution.results.Results))
	for _, result := range execution.results.Results {
		content = append(content, resultToContentItem(result, ""))
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal web search results: %w", err)
	}
	return marshalGatewayToolResult(gatewayToolResultBlock{Type: "tool_result", ToolUseID: execution.call.id, Content: encoded})
}

func marshalGatewayToolResult(result gatewayToolResultBlock) (json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result: %w", err)
	}
	return encoded, nil
}

func resultToContentItem(result websearch.Result, blockType string) gatewaySearchResultBlock {
	content := result.Snippet
	if result.Text != "" {
		content = result.Text
	}
	return gatewaySearchResultBlock{
		Type:          blockType,
		Title:         result.Title,
		URL:           result.URL,
		Content:       content,
		PublishedDate: result.PublishedDate,
		PageAge:       result.PageAge,
	}
}

func resultToServerContentItem(result websearch.Result) gatewaySearchResultBlock {
	return gatewaySearchResultBlock{
		Type:    "web_search_result",
		Title:   result.Title,
		URL:     result.URL,
		PageAge: result.PageAge,
	}
}

func encodeGatewaySSE(body []byte) ([]byte, error) {
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
	if err := writeGatewaySSE(&output, "message_start", struct {
		Type    string                     `json:"type"`
		Message map[string]json.RawMessage `json:"message"`
	}{Type: "message_start", Message: messageStart}); err != nil {
		return nil, err
	}
	for index, rawBlock := range content {
		var block gatewayContentBlock
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
		if err := writeGatewaySSE(&output, "content_block_start", struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
		}{Type: "content_block_start", Index: index, ContentBlock: startBlock}); err != nil {
			return nil, err
		}
		if block.Text != "" {
			if err := writeGatewaySSE(&output, "content_block_delta", struct {
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
			if err := writeGatewaySSE(&output, "content_block_delta", struct {
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
		if err := writeGatewaySSE(&output, "content_block_stop", struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: "content_block_stop", Index: index}); err != nil {
			return nil, err
		}
	}
	if err := writeGatewaySSE(&output, "message_delta", struct {
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
	if err := writeGatewaySSE(&output, "message_stop", struct {
		Type string `json:"type"`
	}{Type: "message_stop"}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeGatewaySSE(output *bytes.Buffer, event string, value any) error {
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
