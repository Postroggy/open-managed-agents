package messages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const omaServerToolIDPrefix = "srvtoolu_oma_"

type gatewayMessageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type gatewayProtocolBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type gatewayProjectedMessage struct {
	role   string
	blocks []json.RawMessage
}

type gatewayPendingTurn struct {
	orderedCalls  []gatewayToolCall
	clientResults map[string]json.RawMessage
}

func hasGatewayWebSearchHistory(messages []json.RawMessage) bool {
	for _, rawMessage := range messages {
		message, err := decodeGatewayMessage(rawMessage)
		if err != nil || message.Role != "assistant" {
			continue
		}
		var blocks []json.RawMessage
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, rawBlock := range blocks {
			var block gatewayProtocolBlock
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

func gatewayResponseContent(body []byte) ([]json.RawMessage, error) {
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

func gatewaySearchCalls(calls []gatewayToolCall) []gatewayToolCall {
	searchCalls := make([]gatewayToolCall, 0, len(calls))
	for _, call := range calls {
		if call.search != nil {
			searchCalls = append(searchCalls, call)
		}
	}
	return searchCalls
}

func finalizeGatewayResponse(response gatewayResponse, content []json.RawMessage, stream bool) (gatewayResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &fields); err != nil {
		return gatewayResponse{}, fmt.Errorf("decode messages response: %w", err)
	}
	encodedContent, err := json.Marshal(content)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("encode messages response content: %w", err)
	}
	fields["content"] = encodedContent
	response.body, err = json.Marshal(fields)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("encode messages response: %w", err)
	}
	if stream {
		response.body, err = encodeGatewaySSE(response.body)
		if err != nil {
			return gatewayResponse{}, fmt.Errorf("encode messages stream: %w", err)
		}
		response.header.Set("Content-Type", "text/event-stream")
		prepareResponseHeaders(response.header)
	}
	response.header.Del("Content-Length")
	return response, nil
}

func (g *gateway) prepareGatewayTranscript(ctx context.Context, messages []json.RawMessage, policy gatewaySearchPolicy) ([]json.RawMessage, []json.RawMessage, int, error) {
	pending, err := findPendingGatewayTurn(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	transcript, err := projectGatewayTranscript(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	if pending == nil {
		return transcript, nil, 0, nil
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
	transcript[len(transcript)-1], err = replaceGatewayMessageContent(transcript[len(transcript)-1], mergedResults)
	if err != nil {
		return nil, nil, 0, err
	}
	prefix, err := gatewayExecutionResultBlocks(executions)
	if err != nil {
		return nil, nil, 0, err
	}
	return transcript, prefix, searchUses, nil
}

func findPendingGatewayTurn(messages []json.RawMessage) (*gatewayPendingTurn, error) {
	if len(messages) < 2 {
		return nil, nil
	}
	assistant, err := decodeGatewayMessage(messages[len(messages)-2])
	if err != nil {
		return nil, err
	}
	user, err := decodeGatewayMessage(messages[len(messages)-1])
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
		var block gatewayProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil && block.Type == "web_search_tool_result" {
			completedSearches[block.ToolUseID] = struct{}{}
		}
	}
	turn := &gatewayPendingTurn{clientResults: make(map[string]json.RawMessage)}
	clientCalls := make(map[string]struct{})
	pendingSearches := 0
	for _, rawBlock := range assistantBlocks {
		var block gatewayProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode assistant content block: %w", err)
		}
		switch {
		case block.Type == "server_tool_use" && block.Name == searchToolName:
			if _, complete := completedSearches[block.ID]; complete {
				continue
			}
			call, err := pendingGatewaySearchCall(block)
			if err != nil {
				return nil, err
			}
			turn.orderedCalls = append(turn.orderedCalls, call)
			pendingSearches++
		case block.Type == "tool_use":
			if strings.TrimSpace(block.ID) == "" {
				return nil, errors.New("client tool use id is required")
			}
			turn.orderedCalls = append(turn.orderedCalls, gatewayToolCall{id: block.ID, name: block.Name, input: block.Input})
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
		var block gatewayProtocolBlock
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

func pendingGatewaySearchCall(block gatewayProtocolBlock) (gatewayToolCall, error) {
	if strings.TrimSpace(block.ID) == "" {
		return gatewayToolCall{}, errors.New("server web search tool use id is required")
	}
	upstreamID, err := upstreamGatewayToolUseID(block.ID)
	if err != nil {
		return gatewayToolCall{}, err
	}
	var input gatewaySearchInput
	if len(block.Input) == 0 || json.Unmarshal(block.Input, &input) != nil {
		return gatewayToolCall{}, errors.New("server web search tool input must be an object")
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return gatewayToolCall{}, errors.New("server web search query is required")
	}
	return gatewayToolCall{
		id:         upstreamID,
		externalID: block.ID,
		name:       searchToolName,
		input:      append(json.RawMessage(nil), block.Input...),
		search:     &input,
	}, nil
}

func (t *gatewayPendingTurn) searchCalls() []gatewayToolCall {
	calls := make([]gatewayToolCall, 0, len(t.orderedCalls))
	for _, call := range t.orderedCalls {
		if call.search != nil {
			calls = append(calls, call)
		}
	}
	return calls
}

func (t *gatewayPendingTurn) mergeResults(searchResults []json.RawMessage) ([]json.RawMessage, error) {
	byID := make(map[string]json.RawMessage, len(searchResults))
	for _, rawResult := range searchResults {
		var result gatewayProtocolBlock
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

func projectGatewayTranscript(messages []json.RawMessage) ([]json.RawMessage, error) {
	projected := make([]json.RawMessage, 0, len(messages))
	for _, rawMessage := range messages {
		message, err := decodeGatewayMessage(rawMessage)
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
		segments, err := projectAssistantGatewayMessages(blocks)
		if err != nil {
			return nil, err
		}
		for _, segment := range segments {
			projected, err = appendGatewayProjectedMessage(projected, segment)
			if err != nil {
				return nil, err
			}
		}
	}
	return projected, nil
}

func projectAssistantGatewayMessages(blocks []json.RawMessage) ([]gatewayProjectedMessage, error) {
	segments := make([]gatewayProjectedMessage, 0, 3)
	assistantBlocks := make([]json.RawMessage, 0, len(blocks))
	userBlocks := make([]json.RawMessage, 0, len(blocks))
	flush := func(role string, pending *[]json.RawMessage) {
		if len(*pending) == 0 {
			return
		}
		segments = append(segments, gatewayProjectedMessage{role: role, blocks: *pending})
		*pending = nil
	}
	for _, rawBlock := range blocks {
		var block gatewayProtocolBlock
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

func appendGatewayProjectedMessage(transcript []json.RawMessage, message gatewayProjectedMessage) ([]json.RawMessage, error) {
	if message.role == "user" && gatewayToolResultsOnly(message.blocks) && len(transcript) > 0 {
		last, err := decodeGatewayMessage(transcript[len(transcript)-1])
		if err != nil {
			return nil, err
		}
		var lastBlocks []json.RawMessage
		if last.Role == "user" && json.Unmarshal(last.Content, &lastBlocks) == nil && gatewayToolResultsOnly(lastBlocks) {
			merged := append(lastBlocks, message.blocks...)
			if len(transcript) >= 2 {
				merged = orderGatewayToolResults(transcript[len(transcript)-2], merged)
			}
			transcript[len(transcript)-1], err = replaceGatewayMessageContent(transcript[len(transcript)-1], merged)
			return transcript, err
		}
	}
	encoded, err := marshalGatewayMessage(message.role, message.blocks)
	if err != nil {
		return nil, err
	}
	return append(transcript, encoded), nil
}

func gatewayToolResultsOnly(blocks []json.RawMessage) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, rawBlock := range blocks {
		var block gatewayProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "tool_result" {
			return false
		}
	}
	return true
}

func orderGatewayToolResults(rawAssistant json.RawMessage, results []json.RawMessage) []json.RawMessage {
	assistant, err := decodeGatewayMessage(rawAssistant)
	if err != nil || assistant.Role != "assistant" {
		return results
	}
	var blocks []json.RawMessage
	if json.Unmarshal(assistant.Content, &blocks) != nil {
		return results
	}
	byID := make(map[string]json.RawMessage, len(results))
	for _, rawResult := range results {
		var result gatewayProtocolBlock
		if json.Unmarshal(rawResult, &result) == nil {
			byID[result.ToolUseID] = rawResult
		}
	}
	ordered := make([]json.RawMessage, 0, len(results))
	for _, rawBlock := range blocks {
		var block gatewayProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "tool_use" {
			continue
		}
		if result, ok := byID[block.ID]; ok {
			ordered = append(ordered, result)
			delete(byID, block.ID)
		}
	}
	for _, rawResult := range results {
		var result gatewayProtocolBlock
		if json.Unmarshal(rawResult, &result) == nil {
			if _, ok := byID[result.ToolUseID]; ok {
				ordered = append(ordered, rawResult)
				delete(byID, result.ToolUseID)
			}
		}
	}
	return ordered
}

func projectServerToolUseToClient(rawBlock json.RawMessage, block gatewayProtocolBlock) (json.RawMessage, error) {
	upstreamID, err := upstreamGatewayToolUseID(block.ID)
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
	return json.Marshal(fields)
}

func projectServerResultToClient(block gatewayProtocolBlock) (json.RawMessage, error) {
	upstreamID, err := upstreamGatewayToolUseID(block.ToolUseID)
	if err != nil {
		return nil, err
	}
	var resultError struct {
		Type      string `json:"type"`
		ErrorCode string `json:"error_code"`
	}
	if json.Unmarshal(block.Content, &resultError) == nil && resultError.Type == "web_search_tool_result_error" {
		message := `"web search unavailable"`
		if resultError.ErrorCode == searchErrorMaxUses {
			message = `"web search max uses exceeded"`
		}
		return marshalGatewayToolResult(gatewayToolResultBlock{
			Type: "tool_result", ToolUseID: upstreamID, IsError: true, Content: json.RawMessage(message),
		})
	}
	var searchResults []gatewaySearchResultBlock
	if err := json.Unmarshal(block.Content, &searchResults); err != nil {
		return nil, fmt.Errorf("decode web search result content: %w", err)
	}
	for index := range searchResults {
		searchResults[index].Type = ""
	}
	content, err := json.Marshal(searchResults)
	if err != nil {
		return nil, err
	}
	return marshalGatewayToolResult(gatewayToolResultBlock{Type: "tool_result", ToolUseID: upstreamID, Content: content})
}

func decodeGatewayMessage(rawMessage json.RawMessage) (gatewayMessageEnvelope, error) {
	var message gatewayMessageEnvelope
	if err := json.Unmarshal(rawMessage, &message); err != nil {
		return gatewayMessageEnvelope{}, fmt.Errorf("decode messages transcript entry: %w", err)
	}
	return message, nil
}

func marshalGatewayMessage(role string, blocks []json.RawMessage) (json.RawMessage, error) {
	encoded, err := json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: role, Content: blocks})
	if err != nil {
		return nil, fmt.Errorf("encode %s messages transcript entry: %w", role, err)
	}
	return encoded, nil
}

func replaceGatewayMessageContent(rawMessage json.RawMessage, blocks []json.RawMessage) (json.RawMessage, error) {
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

func projectPendingGatewayContent(content []json.RawMessage) ([]json.RawMessage, error) {
	projected := make([]json.RawMessage, 0, len(content))
	for _, rawBlock := range content {
		var block gatewayProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode mixed tool content block: %w", err)
		}
		if block.Type == "tool_use" && block.Name == searchToolName {
			serverBlock, err := projectClientSearchCallToServer(rawBlock, serverGatewayToolUseID(block.ID))
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

func projectCompletedGatewayContent(content []json.RawMessage, executions []gatewayExecution) ([]json.RawMessage, error) {
	byID := make(map[string]gatewayExecution, len(executions))
	for _, execution := range executions {
		byID[execution.call.id] = execution
	}
	projected := make([]json.RawMessage, 0, len(content)+len(executions))
	for _, rawBlock := range content {
		var block gatewayProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode web search content block: %w", err)
		}
		if block.Type != "tool_use" || block.Name != searchToolName {
			projected = append(projected, append(json.RawMessage(nil), rawBlock...))
			continue
		}
		execution, ok := byID[block.ID]
		if !ok {
			return nil, fmt.Errorf("web search execution %q is missing", block.ID)
		}
		serverBlock, err := projectClientSearchCallToServer(rawBlock, execution.call.externalID)
		if err != nil {
			return nil, err
		}
		resultBlock, err := gatewayWebSearchResultBlock(execution)
		if err != nil {
			return nil, err
		}
		projected = append(projected, serverBlock, resultBlock)
	}
	return projected, nil
}

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
	return json.Marshal(fields)
}

func gatewayExecutionResultBlocks(executions []gatewayExecution) ([]json.RawMessage, error) {
	blocks := make([]json.RawMessage, 0, len(executions))
	for _, execution := range executions {
		block, err := gatewayWebSearchResultBlock(execution)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func gatewayWebSearchResultBlock(execution gatewayExecution) (json.RawMessage, error) {
	externalID := execution.call.externalID
	if externalID == "" {
		externalID = serverGatewayToolUseID(execution.call.id)
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
	resultContent := make([]gatewaySearchResultBlock, 0, len(execution.results.Results))
	for _, result := range execution.results.Results {
		resultContent = append(resultContent, resultToContentItem(result, "web_search_result"))
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

func serverGatewayToolUseID(upstreamID string) string {
	return omaServerToolIDPrefix + base64.RawURLEncoding.EncodeToString([]byte(upstreamID))
}

func upstreamGatewayToolUseID(externalID string) (string, error) {
	if strings.HasPrefix(externalID, omaServerToolIDPrefix) {
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(externalID, omaServerToolIDPrefix))
		if err != nil || len(decoded) == 0 {
			return "", fmt.Errorf("invalid OMA server tool use id %q", externalID)
		}
		return string(decoded), nil
	}
	if strings.HasPrefix(externalID, "srvtoolu_") {
		return "toolu_" + strings.TrimPrefix(externalID, "srvtoolu_"), nil
	}
	if strings.HasPrefix(externalID, "toolu_") {
		return externalID, nil
	}
	return "", fmt.Errorf("invalid server tool use id %q", externalID)
}
