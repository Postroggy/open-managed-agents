package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// serverToolUseIDPrefix 是 Anthropic server tool 的 ID 前缀，upstreamToolUseIDPrefix
	// 是 ordinary tool 的前缀；gateway 只在两者之间做前缀替换。
	serverToolUseIDPrefix   = "srvtoolu_"
	upstreamToolUseIDPrefix = "toolu_"
)

type webSearchMessageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type webSearchProtocolBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type webSearchProjectedMessage struct {
	role   string
	blocks []json.RawMessage
}

type webSearchPendingTurn struct {
	orderedCalls  []webSearchToolCall
	clientResults map[string]json.RawMessage
}

func hasWebSearchHistory(messages []json.RawMessage) bool {
	for _, rawMessage := range messages {
		message, err := decodeWebSearchMessage(rawMessage)
		if err != nil || message.Role != "assistant" {
			continue
		}
		var blocks []json.RawMessage
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, rawBlock := range blocks {
			var block webSearchProtocolBlock
			if json.Unmarshal(rawBlock, &block) != nil {
				continue
			}
			if block.Type == "web_search_tool_result" ||
				(block.Type == "server_tool_use" && block.Name == searchToolName) {
				return true
			}
		}
	}
	return false
}

func webSearchResponseContent(body []byte) ([]json.RawMessage, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Content == nil {
		return nil, errors.New("messages response content must be an array")
	}
	return response.Content, nil
}

func webSearchCalls(calls []webSearchToolCall) []webSearchToolCall {
	searchCalls := make([]webSearchToolCall, 0, len(calls))
	for _, call := range calls {
		if call.search != nil {
			searchCalls = append(searchCalls, call)
		}
	}
	return searchCalls
}

// webSearchUsageAccumulator 汇总同一条入站请求内所有 BYOK 采样的 token 计量。
// gateway 会为一次外部请求发起多次 BYOK 采样，只保留最后一次响应的 usage 会少报
// 前几次迭代消耗的 token。
//
// Anthropic usage 的顶层数值字段都是可累加的 token 计数，因此按 JSON 数值类型判定
// 累加对象即可覆盖后续新增的计数器；字符串和嵌套对象（service_tier、cache_creation、
// server_tool_use）保留最后一次采样的值。
type webSearchUsageAccumulator struct {
	totals  map[string]int64
	last    map[string]json.RawMessage
	samples int
}

// add 累加一次 BYOK 响应的 usage；响应不携带 usage 时不计入采样。
func (u *webSearchUsageAccumulator) add(body []byte) error {
	var response struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode messages usage: %w", err)
	}
	if len(response.Usage) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Usage, &fields); err != nil {
		return fmt.Errorf("decode messages usage: %w", err)
	}
	if len(fields) == 0 {
		return nil
	}
	if u.totals == nil {
		u.totals = make(map[string]int64, len(fields))
	}
	for name, value := range fields {
		if count, ok := webSearchUsageCount(value); ok {
			u.totals[name] += count
		}
	}
	u.last = fields
	u.samples++
	return nil
}

// merge 生成跨迭代累计后的 token 计量。只采样到一次时返回 false，让调用方保持上游
// usage 原样透传。
func (u *webSearchUsageAccumulator) merge() (map[string]json.RawMessage, bool) {
	if u == nil || u.samples == 0 {
		return nil, false
	}
	merged := cloneRawMap(u.last)
	if u.samples < 2 {
		return merged, false
	}
	for name, total := range u.totals {
		encoded, err := json.Marshal(total)
		if err != nil {
			// int64 序列化不会失败；保留最后一次采样的原值而不是让整个响应失败。
			continue
		}
		merged[name] = encoded
	}
	return merged, true
}

// webSearchUsage 生成最终响应的 usage：先按字段名累加多次 BYOK 采样的 token 计数，再写入
// gateway 自己执行的搜索次数。返回 false 表示无需改写上游 usage。
//
// Anthropic 用 usage.server_tool_use.web_search_requests 上报搜索次数，而 BYOK 只看到
// ordinary tool，不会上报该字段，因此这里由 gateway 补齐。
func (u *webSearchUsageAccumulator) webSearchUsage(searchRequests int) (json.RawMessage, bool, error) {
	merged, changed := u.merge()
	if searchRequests > 0 {
		if merged == nil {
			merged = make(map[string]json.RawMessage, 1)
		}
		serverToolUse, err := webSearchServerToolUsage(merged["server_tool_use"], searchRequests)
		if err != nil {
			return nil, false, err
		}
		merged["server_tool_use"] = serverToolUse
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, false, fmt.Errorf("encode messages usage: %w", err)
	}
	return encoded, true, nil
}

// webSearchServerToolUsage 在保留上游其他 server tool 计量的前提下写入搜索次数。
func webSearchServerToolUsage(existing json.RawMessage, searchRequests int) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &fields); err != nil {
			return nil, fmt.Errorf("decode messages server tool usage: %w", err)
		}
	}
	encodedRequests, err := json.Marshal(searchRequests)
	if err != nil {
		return nil, fmt.Errorf("encode web search request count: %w", err)
	}
	fields["web_search_requests"] = encodedRequests
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode messages server tool usage: %w", err)
	}
	return encoded, nil
}

// countBillableWebSearchRequests 统计本次响应内容里计费的搜索次数。Anthropic 规定每次
// 搜索计一次、失败的搜索不计费，因此只统计带结果的 web_search_tool_result；
// max_uses_exceeded 与 provider 失败都以 web_search_tool_result_error 呈现，不计入。
func countBillableWebSearchRequests(content []json.RawMessage) int {
	requests := 0
	for _, rawBlock := range content {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "web_search_tool_result" {
			continue
		}
		if webSearchResultErrorCode(block.Content) != "" {
			continue
		}
		requests++
	}
	return requests
}

func webSearchUsageCount(value json.RawMessage) (int64, bool) {
	var number json.Number
	if err := json.Unmarshal(value, &number); err != nil {
		return 0, false
	}
	count, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return count, true
}

func finalizeWebSearchResponse(response webSearchGatewayResponse, content []json.RawMessage, stream bool, stopReason string, usage *webSearchUsageAccumulator) (webSearchGatewayResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &fields); err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("decode messages response: %w", err)
	}
	encodedContent, err := json.Marshal(content)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("encode messages response content: %w", err)
	}
	fields["content"] = encodedContent
	if stopReason != "" {
		encodedStopReason, err := json.Marshal(stopReason)
		if err != nil {
			return webSearchGatewayResponse{}, fmt.Errorf("encode messages stop reason: %w", err)
		}
		fields["stop_reason"] = encodedStopReason
	}
	mergedUsage, changed, err := usage.webSearchUsage(countBillableWebSearchRequests(content))
	if err != nil {
		return webSearchGatewayResponse{}, err
	}
	if changed {
		fields["usage"] = mergedUsage
	}
	response.body, err = json.Marshal(fields)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("encode messages response: %w", err)
	}
	if stream {
		response.body, err = encodeWebSearchSSE(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, fmt.Errorf("encode messages stream: %w", err)
		}
		response.header.Set("Content-Type", "text/event-stream")
		prepareResponseHeaders(response.header)
	}
	response.header.Del("Content-Length")
	return response, nil
}

func (g *webSearchGateway) prepareWebSearchTranscript(ctx context.Context, messages []json.RawMessage, policy webSearchPolicy) ([]json.RawMessage, []json.RawMessage, int, error) {
	pending, err := findPendingWebSearchTurn(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	paused, err := findPausedWebSearchTurn(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	transcript, err := projectWebSearchTranscript(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	// max_uses 是 per-request 上限，与 Anthropic 官方语义一致（"limits the number of
	// searches performed" per request，每条请求独立计数）。同一请求内的 BYOK
	// continuation 不重置计数；pause_turn 与 mixed continuation 是新的入站请求，因此
	// 按官方语义重新获得完整额度，gateway 不从历史里累加。
	if pending == nil && paused == nil {
		return transcript, nil, 0, nil
	}
	if paused != nil {
		return g.resumePausedWebSearchTurn(ctx, transcript, paused, policy)
	}
	searchResults, executions, searchUses, err := g.executeSearchCalls(ctx, pending.searchCalls(), policy, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	mergedResults, err := pending.mergeResults(searchResults)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(transcript) == 0 {
		return nil, nil, 0, errors.New("pending web search continuation has no transcript")
	}
	transcript[len(transcript)-1], err = replaceWebSearchMessageContent(transcript[len(transcript)-1], mergedResults)
	if err != nil {
		return nil, nil, 0, err
	}
	prefix, err := webSearchExecutionResultBlocks(executions)
	if err != nil {
		return nil, nil, 0, err
	}
	return transcript, prefix, searchUses, nil
}

func webSearchResultErrorCode(content json.RawMessage) string {
	var resultError struct {
		Type      string `json:"type"`
		ErrorCode string `json:"error_code"`
	}
	if json.Unmarshal(content, &resultError) != nil || resultError.Type != "web_search_tool_result_error" {
		return ""
	}
	return resultError.ErrorCode
}

func (g *webSearchGateway) resumePausedWebSearchTurn(ctx context.Context, transcript []json.RawMessage, pending *webSearchPendingTurn, policy webSearchPolicy) ([]json.RawMessage, []json.RawMessage, int, error) {
	searchResults, executions, searchUses, err := g.executeSearchCalls(ctx, pending.searchCalls(), policy, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	resultMessage, err := webSearchUserMessage(searchResults)
	if err != nil {
		return nil, nil, 0, err
	}
	transcript = append(transcript, resultMessage)
	prefix, err := webSearchExecutionResultBlocks(executions)
	if err != nil {
		return nil, nil, 0, err
	}
	return transcript, prefix, searchUses, nil
}

func isWebSearchPauseContinuation(messages []json.RawMessage) bool {
	if len(messages) == 0 {
		return false
	}
	message, err := decodeWebSearchMessage(messages[len(messages)-1])
	if err != nil || message.Role != "assistant" {
		return false
	}
	var blocks []json.RawMessage
	if json.Unmarshal(message.Content, &blocks) != nil {
		return false
	}
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil &&
			(block.Type == "web_search_tool_result" ||
				(block.Type == "server_tool_use" && block.Name == searchToolName)) {
			return true
		}
	}
	return false
}

func findPausedWebSearchTurn(messages []json.RawMessage) (*webSearchPendingTurn, error) {
	if !isWebSearchPauseContinuation(messages) {
		return nil, nil
	}
	assistant, err := decodeWebSearchMessage(messages[len(messages)-1])
	if err != nil {
		return nil, err
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(assistant.Content, &blocks); err != nil {
		return nil, err
	}
	completedSearches := make(map[string]struct{})
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil && block.Type == "web_search_tool_result" {
			completedSearches[block.ToolUseID] = struct{}{}
		}
	}
	turn := &webSearchPendingTurn{clientResults: make(map[string]json.RawMessage)}
	serverToolUses := make(map[string]struct{})
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode paused assistant content block: %w", err)
		}
		if block.Type == "tool_use" {
			return nil, errors.New("pause_turn continuation cannot contain a pending client tool use")
		}
		if block.Type != "server_tool_use" || block.Name != searchToolName {
			continue
		}
		if _, duplicate := serverToolUses[block.ID]; duplicate {
			return nil, fmt.Errorf("duplicate server web search tool use id %q", block.ID)
		}
		serverToolUses[block.ID] = struct{}{}
		if _, complete := completedSearches[block.ID]; complete {
			continue
		}
		call, err := pendingWebSearchCall(block)
		if err != nil {
			return nil, err
		}
		turn.orderedCalls = append(turn.orderedCalls, call)
	}
	if len(turn.orderedCalls) == 0 {
		return nil, nil
	}
	return turn, nil
}

func findPendingWebSearchTurn(messages []json.RawMessage) (*webSearchPendingTurn, error) {
	if len(messages) < 2 {
		return nil, nil
	}
	assistant, err := decodeWebSearchMessage(messages[len(messages)-2])
	if err != nil {
		return nil, err
	}
	user, err := decodeWebSearchMessage(messages[len(messages)-1])
	if err != nil {
		return nil, err
	}
	if assistant.Role != "assistant" || user.Role != "user" {
		return nil, nil
	}
	var assistantBlocks []json.RawMessage
	if err := json.Unmarshal(assistant.Content, &assistantBlocks); err != nil {
		return nil, nil
	}
	completedSearches := make(map[string]struct{})
	for _, rawBlock := range assistantBlocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil && block.Type == "web_search_tool_result" {
			completedSearches[block.ToolUseID] = struct{}{}
		}
	}
	turn := &webSearchPendingTurn{clientResults: make(map[string]json.RawMessage)}
	clientCalls := make(map[string]struct{})
	serverToolUses := make(map[string]struct{})
	pendingSearches := 0
	for _, rawBlock := range assistantBlocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode assistant content block: %w", err)
		}
		switch {
		case block.Type == "server_tool_use" && block.Name == searchToolName:
			if _, duplicate := serverToolUses[block.ID]; duplicate {
				return nil, fmt.Errorf("duplicate server web search tool use id %q", block.ID)
			}
			serverToolUses[block.ID] = struct{}{}
			if _, complete := completedSearches[block.ID]; complete {
				continue
			}
			call, err := pendingWebSearchCall(block)
			if err != nil {
				return nil, err
			}
			turn.orderedCalls = append(turn.orderedCalls, call)
			pendingSearches++
		case block.Type == "tool_use":
			if strings.TrimSpace(block.ID) == "" {
				return nil, errors.New("client tool use id is required")
			}
			turn.orderedCalls = append(turn.orderedCalls, webSearchToolCall{id: block.ID, name: block.Name, input: block.Input})
			clientCalls[block.ID] = struct{}{}
		}
	}
	if pendingSearches == 0 {
		return nil, nil
	}
	if len(clientCalls) == 0 {
		return nil, errors.New("pending web search continuation is missing a client tool use")
	}
	var userBlocks []json.RawMessage
	if err := json.Unmarshal(user.Content, &userBlocks); err != nil {
		return nil, errors.New("mixed tool continuation must contain tool_result blocks")
	}
	for _, rawBlock := range userBlocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode client tool result: %w", err)
		}
		if block.Type != "tool_result" {
			return nil, errors.New("mixed tool continuation must contain only tool_result blocks")
		}
		if _, ok := clientCalls[block.ToolUseID]; !ok {
			return nil, fmt.Errorf("unexpected client tool result %q", block.ToolUseID)
		}
		if _, duplicate := turn.clientResults[block.ToolUseID]; duplicate {
			return nil, fmt.Errorf("duplicate client tool result %q", block.ToolUseID)
		}
		turn.clientResults[block.ToolUseID] = append(json.RawMessage(nil), rawBlock...)
	}
	for id := range clientCalls {
		if _, ok := turn.clientResults[id]; !ok {
			return nil, fmt.Errorf("client tool result %q is required", id)
		}
	}
	return turn, nil
}

func pendingWebSearchCall(block webSearchProtocolBlock) (webSearchToolCall, error) {
	if strings.TrimSpace(block.ID) == "" {
		return webSearchToolCall{}, errors.New("server web search tool use id is required")
	}
	upstreamID, err := upstreamWebSearchToolUseID(block.ID)
	if err != nil {
		return webSearchToolCall{}, err
	}
	var input webSearchInput
	if len(block.Input) == 0 || json.Unmarshal(block.Input, &input) != nil {
		return webSearchToolCall{}, errors.New("server web search tool input must be an object")
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return webSearchToolCall{}, errors.New("server web search query is required")
	}
	return webSearchToolCall{
		id:         upstreamID,
		externalID: block.ID,
		name:       upstreamSearchToolName,
		input:      append(json.RawMessage(nil), block.Input...),
		search:     &input,
	}, nil
}

func (t *webSearchPendingTurn) searchCalls() []webSearchToolCall {
	calls := make([]webSearchToolCall, 0, len(t.orderedCalls))
	for _, call := range t.orderedCalls {
		if call.search != nil {
			calls = append(calls, call)
		}
	}
	return calls
}

func (t *webSearchPendingTurn) mergeResults(searchResults []json.RawMessage) ([]json.RawMessage, error) {
	byID := make(map[string]json.RawMessage, len(searchResults))
	for _, rawResult := range searchResults {
		var result webSearchProtocolBlock
		if err := json.Unmarshal(rawResult, &result); err != nil {
			return nil, fmt.Errorf("decode web search tool result: %w", err)
		}
		byID[result.ToolUseID] = rawResult
	}
	merged := make([]json.RawMessage, 0, len(t.orderedCalls))
	for _, call := range t.orderedCalls {
		if call.search != nil {
			result, ok := byID[call.id]
			if !ok {
				return nil, fmt.Errorf("web search tool result %q is missing", call.id)
			}
			merged = append(merged, result)
			continue
		}
		result, ok := t.clientResults[call.id]
		if !ok {
			return nil, fmt.Errorf("client tool result %q is missing", call.id)
		}
		merged = append(merged, result)
	}
	return merged, nil
}

func projectWebSearchTranscript(messages []json.RawMessage) ([]json.RawMessage, error) {
	projected := make([]json.RawMessage, 0, len(messages))
	for _, rawMessage := range messages {
		message, err := decodeWebSearchMessage(rawMessage)
		if err != nil {
			return nil, err
		}
		if message.Role != "assistant" {
			projected = append(projected, append(json.RawMessage(nil), rawMessage...))
			continue
		}
		var blocks []json.RawMessage
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			projected = append(projected, append(json.RawMessage(nil), rawMessage...))
			continue
		}
		segments, err := projectAssistantWebSearchMessages(blocks)
		if err != nil {
			return nil, err
		}
		for _, segment := range segments {
			projected, err = appendWebSearchProjectedMessage(projected, segment)
			if err != nil {
				return nil, err
			}
		}
	}
	return projected, nil
}

func projectAssistantWebSearchMessages(blocks []json.RawMessage) ([]webSearchProjectedMessage, error) {
	segments := make([]webSearchProjectedMessage, 0, 3)
	assistantBlocks := make([]json.RawMessage, 0, len(blocks))
	userBlocks := make([]json.RawMessage, 0, len(blocks))
	flush := func(role string, pending *[]json.RawMessage) {
		if len(*pending) == 0 {
			return
		}
		segments = append(segments, webSearchProjectedMessage{role: role, blocks: *pending})
		*pending = nil
	}
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode assistant content block: %w", err)
		}
		if block.Type == "web_search_tool_result" {
			flush("assistant", &assistantBlocks)
			result, err := projectServerResultToClient(block)
			if err != nil {
				return nil, err
			}
			userBlocks = append(userBlocks, result)
			continue
		}
		flush("user", &userBlocks)
		if block.Type == "server_tool_use" && block.Name == searchToolName {
			projectedBlock, err := projectServerToolUseToClient(rawBlock, block)
			if err != nil {
				return nil, err
			}
			assistantBlocks = append(assistantBlocks, projectedBlock)
			continue
		}
		assistantBlocks = append(assistantBlocks, append(json.RawMessage(nil), rawBlock...))
	}
	flush("user", &userBlocks)
	flush("assistant", &assistantBlocks)
	return segments, nil
}

func appendWebSearchProjectedMessage(transcript []json.RawMessage, message webSearchProjectedMessage) ([]json.RawMessage, error) {
	if message.role == "user" && webSearchToolResultsOnly(message.blocks) && len(transcript) > 0 {
		last, err := decodeWebSearchMessage(transcript[len(transcript)-1])
		if err != nil {
			return nil, err
		}
		var lastBlocks []json.RawMessage
		if last.Role == "user" && json.Unmarshal(last.Content, &lastBlocks) == nil && webSearchToolResultsOnly(lastBlocks) {
			merged := append(lastBlocks, message.blocks...)
			if len(transcript) >= 2 {
				merged = orderWebSearchToolResults(transcript[len(transcript)-2], merged)
			}
			transcript[len(transcript)-1], err = replaceWebSearchMessageContent(transcript[len(transcript)-1], merged)
			return transcript, err
		}
	}
	encoded, err := marshalWebSearchMessage(message.role, message.blocks)
	if err != nil {
		return nil, err
	}
	return append(transcript, encoded), nil
}

func webSearchToolResultsOnly(blocks []json.RawMessage) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "tool_result" {
			return false
		}
	}
	return true
}

func orderWebSearchToolResults(rawAssistant json.RawMessage, results []json.RawMessage) []json.RawMessage {
	assistant, err := decodeWebSearchMessage(rawAssistant)
	if err != nil || assistant.Role != "assistant" {
		return results
	}
	var blocks []json.RawMessage
	if json.Unmarshal(assistant.Content, &blocks) != nil {
		return results
	}
	// Anthropic 按 tool_use_id 匹配 result，不依赖 result 在 user message 里的顺序，
	// 因此这里只做与 tool_use 一致的可读性排序。按 ID 排队而不是按 ID 建立唯一索引，
	// 是为了让重复 ID 的 result 也全部保留：丢弃 result 会让对应的 tool_use 失去配对。
	pendingByID := make(map[string][]int, len(results))
	for index, rawResult := range results {
		var result webSearchProtocolBlock
		if json.Unmarshal(rawResult, &result) == nil {
			pendingByID[result.ToolUseID] = append(pendingByID[result.ToolUseID], index)
		}
	}
	ordered := make([]json.RawMessage, 0, len(results))
	emitted := make([]bool, len(results))
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "tool_use" {
			continue
		}
		queue := pendingByID[block.ID]
		if len(queue) == 0 {
			continue
		}
		pendingByID[block.ID] = queue[1:]
		ordered = append(ordered, results[queue[0]])
		emitted[queue[0]] = true
	}
	for index, rawResult := range results {
		if !emitted[index] {
			ordered = append(ordered, rawResult)
		}
	}
	return ordered
}

// projectServerToolUseToClient 把历史里的 server_tool_use 还原为 BYOK 的 ordinary
// tool_use，同时把协议名换成 gateway 实际声明给 BYOK 的独占工具名。
func projectServerToolUseToClient(rawBlock json.RawMessage, block webSearchProtocolBlock) (json.RawMessage, error) {
	upstreamID, err := upstreamWebSearchToolUseID(block.ID)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBlock, &fields); err != nil {
		return nil, err
	}
	fields["type"] = json.RawMessage(`"tool_use"`)
	encodedID, err := json.Marshal(upstreamID)
	if err != nil {
		return nil, err
	}
	fields["id"] = encodedID
	encodedName, err := json.Marshal(upstreamSearchToolName)
	if err != nil {
		return nil, err
	}
	fields["name"] = encodedName
	return json.Marshal(fields)
}

func projectServerResultToClient(block webSearchProtocolBlock) (json.RawMessage, error) {
	upstreamID, err := upstreamWebSearchToolUseID(block.ToolUseID)
	if err != nil {
		return nil, err
	}
	if errorCode := webSearchResultErrorCode(block.Content); errorCode != "" {
		message := `"web search unavailable"`
		if errorCode == searchErrorMaxUses {
			message = `"web search max uses exceeded"`
		}
		return marshalWebSearchToolResult(webSearchToolResultBlock{
			Type: "tool_result", ToolUseID: upstreamID, IsError: true, Content: json.RawMessage(message),
		})
	}
	var searchResults []webSearchResultBlock
	if err := json.Unmarshal(block.Content, &searchResults); err != nil {
		return nil, fmt.Errorf("decode web search result content: %w", err)
	}
	for index := range searchResults {
		searchResults[index].Type = ""
		searchResults[index].EncryptedContent = ""
	}
	content, err := json.Marshal(searchResults)
	if err != nil {
		return nil, err
	}
	return marshalWebSearchToolResult(webSearchToolResultBlock{Type: "tool_result", ToolUseID: upstreamID, Content: content})
}

func decodeWebSearchMessage(rawMessage json.RawMessage) (webSearchMessageEnvelope, error) {
	var message webSearchMessageEnvelope
	if err := json.Unmarshal(rawMessage, &message); err != nil {
		return webSearchMessageEnvelope{}, fmt.Errorf("decode messages transcript entry: %w", err)
	}
	return message, nil
}

func marshalWebSearchMessage(role string, blocks []json.RawMessage) (json.RawMessage, error) {
	encoded, err := json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: role, Content: blocks})
	if err != nil {
		return nil, fmt.Errorf("encode %s messages transcript entry: %w", role, err)
	}
	return encoded, nil
}

func replaceWebSearchMessageContent(rawMessage json.RawMessage, blocks []json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawMessage, &fields); err != nil {
		return nil, fmt.Errorf("decode messages transcript entry: %w", err)
	}
	content, err := json.Marshal(blocks)
	if err != nil {
		return nil, fmt.Errorf("encode messages transcript content: %w", err)
	}
	fields["content"] = content
	return json.Marshal(fields)
}

func projectPendingWebSearchContent(content []json.RawMessage) ([]json.RawMessage, error) {
	projected := make([]json.RawMessage, 0, len(content))
	for _, rawBlock := range content {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode mixed tool content block: %w", err)
		}
		if block.Type == "tool_use" && block.Name == upstreamSearchToolName {
			externalID, err := serverWebSearchToolUseID(block.ID)
			if err != nil {
				return nil, err
			}
			serverBlock, err := projectClientSearchCallToServer(rawBlock, externalID)
			if err != nil {
				return nil, err
			}
			projected = append(projected, serverBlock)
			continue
		}
		projected = append(projected, append(json.RawMessage(nil), rawBlock...))
	}
	return projected, nil
}

func projectCompletedWebSearchContent(content []json.RawMessage, executions []webSearchExecution) ([]json.RawMessage, error) {
	byID := make(map[string]webSearchExecution, len(executions))
	for _, execution := range executions {
		byID[execution.call.id] = execution
	}
	projected := make([]json.RawMessage, 0, len(content)+len(executions))
	for _, rawBlock := range content {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode web search content block: %w", err)
		}
		if block.Type != "tool_use" || block.Name != upstreamSearchToolName {
			projected = append(projected, append(json.RawMessage(nil), rawBlock...))
			continue
		}
		execution, ok := byID[block.ID]
		if !ok {
			return nil, fmt.Errorf("web search execution %q is missing", block.ID)
		}
		externalID, err := execution.serverToolUseID()
		if err != nil {
			return nil, err
		}
		serverBlock, err := projectClientSearchCallToServer(rawBlock, externalID)
		if err != nil {
			return nil, err
		}
		resultBlock, err := webSearchServerResultBlock(execution)
		if err != nil {
			return nil, err
		}
		projected = append(projected, serverBlock, resultBlock)
	}
	return projected, nil
}

// projectClientSearchCallToServer 把 BYOK 的 ordinary tool_use 投影为面向调用方的
// server_tool_use，并把独占工具名换回 Anthropic 协议名，避免内部名字泄漏给调用方。
func projectClientSearchCallToServer(rawBlock json.RawMessage, externalID string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBlock, &fields); err != nil {
		return nil, err
	}
	fields["type"] = json.RawMessage(`"server_tool_use"`)
	encodedID, err := json.Marshal(externalID)
	if err != nil {
		return nil, err
	}
	fields["id"] = encodedID
	encodedName, err := json.Marshal(searchToolName)
	if err != nil {
		return nil, err
	}
	fields["name"] = encodedName
	return json.Marshal(fields)
}

func webSearchExecutionResultBlocks(executions []webSearchExecution) ([]json.RawMessage, error) {
	blocks := make([]json.RawMessage, 0, len(executions))
	for _, execution := range executions {
		block, err := webSearchServerResultBlock(execution)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func webSearchServerResultBlock(execution webSearchExecution) (json.RawMessage, error) {
	externalID, err := execution.serverToolUseID()
	if err != nil {
		return nil, err
	}
	if execution.err != nil {
		errorCode := execution.errorCode
		if errorCode == "" {
			errorCode = searchErrorUnavailable
		}
		return json.Marshal(struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   struct {
				Type      string `json:"type"`
				ErrorCode string `json:"error_code"`
			} `json:"content"`
		}{Type: "web_search_tool_result", ToolUseID: externalID, Content: struct {
			Type      string `json:"type"`
			ErrorCode string `json:"error_code"`
		}{Type: "web_search_tool_result_error", ErrorCode: errorCode}})
	}
	resultContent := make([]webSearchResultBlock, 0, len(execution.results.Results))
	for _, result := range execution.results.Results {
		resultContent = append(resultContent, resultToServerContentItem(result))
	}
	encodedContent, err := json.Marshal(resultContent)
	if err != nil {
		return nil, fmt.Errorf("marshal web search content: %w", err)
	}
	return json.Marshal(struct {
		Type      string          `json:"type"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}{Type: "web_search_tool_result", ToolUseID: externalID, Content: encodedContent})
}

// serverWebSearchToolUseID 把 BYOK 的 ordinary tool_use ID 映射为面向调用方的 server
// tool ID。映射只是把 Anthropic 的 toolu_ 前缀换成 srvtoolu_，因此是双向唯一的；不引入
// 自定义编码可以避免同一上游 ID 存在多种外部表示、进而在同一条消息里产生重复 ID。
func serverWebSearchToolUseID(upstreamID string) (string, error) {
	suffix, ok := strings.CutPrefix(upstreamID, upstreamToolUseIDPrefix)
	if !ok || suffix == "" {
		return "", fmt.Errorf("unsupported upstream tool use id %q", upstreamID)
	}
	return serverToolUseIDPrefix + suffix, nil
}

// upstreamWebSearchToolUseID 是 serverWebSearchToolUseID 的逆映射。只接受 gateway 自己
// 生成的形状：这三个调用点处理的都是 OMA 铸造的 ID，放宽前缀只会让调用方伪造上游 ID。
func upstreamWebSearchToolUseID(externalID string) (string, error) {
	suffix, ok := strings.CutPrefix(externalID, serverToolUseIDPrefix)
	if !ok || suffix == "" {
		return "", fmt.Errorf("invalid server tool use id %q", externalID)
	}
	return upstreamToolUseIDPrefix + suffix, nil
}
