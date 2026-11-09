package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

func (e *Engine) stage(ctx context.Context, d *Dream, stage, sessionID, purpose string, tools *toolkit) error {
	system := safetyPrompt + "\n" + purpose
	task := "Perform the " + stage + " pass. Start by discovering the memory files with glob."
	if sessionID != "" {
		task += " Read the complete source transcript with session_id=" + sessionID + "."
	} else {
		task += " Available source session IDs: " + strings.Join(d.Inputs[1].SessionIDs, ", ") + "."
	}
	if d.Instructions != nil {
		task += "\nCaller synthesis guidance:\n" + *d.Instructions
	}
	request := ModelRequest{Model: d.Model.ID, System: system, Tools: toolDefinitions(), MaxTokens: e.Limits.MaxOutputTokens, Messages: []Message{{Role: "user", Content: []Block{{Type: "text", Text: task}}}}}
	if err := e.record(ctx, Event{Stage: stage, SessionID: sessionID, Kind: "stage_started"}); err != nil {
		return err
	}
	for turn := 0; turn < e.Limits.MaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if totalTokens(d.Usage) >= e.Limits.MaxTotalTokens {
			return &Failure{Type: "token_budget_exceeded", Message: "Configured token budget reached"}
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if len(encoded) > e.Limits.MaxContextBytes {
			return &Failure{Type: "context_budget_exceeded", Message: "Stage context exceeds the configured limit; no input was silently truncated"}
		}
		response, err := e.Model.Complete(ctx, request)
		if err != nil {
			return fmt.Errorf("%s model call: %w", stage, err)
		}
		if err := validateUsage(response.Usage, d.Usage); err != nil {
			return err
		}
		d.Usage = d.Usage.Add(response.Usage)
		if err := e.record(ctx, Event{Stage: stage, SessionID: sessionID, Kind: "model_response", Usage: response.Usage}); err != nil {
			return err
		}
		if totalTokens(d.Usage) > e.Limits.MaxTotalTokens {
			return &Failure{Type: "token_budget_exceeded", Message: "Configured token budget exceeded; no further tools will run"}
		}
		calls, err := validateResponse(response, e.Limits.MaxToolsPerTurn)
		if err != nil {
			return err
		}
		if len(calls) == 0 {
			if stage == "analyze" && !tools.sourceRead(sessionID) {
				return &Failure{Type: "incomplete_transcript_read", Message: "Analyzer ended before reading its complete transcript"}
			}
			return e.record(ctx, Event{Stage: stage, SessionID: sessionID, Kind: "stage_completed"})
		}
		request.Messages = append(request.Messages, Message{Role: "assistant", Content: response.Content})
		results, err := e.executeCalls(ctx, stage, sessionID, calls, tools)
		if err != nil {
			return err
		}
		request.Messages = append(request.Messages, Message{Role: "user", Content: results})
	}
	return &Failure{Type: "turn_limit_exceeded", Message: "Stage did not complete within the configured turn limit"}
}

func (e *Engine) executeCalls(ctx context.Context, stage, sessionID string, calls []Block, tools *toolkit) ([]Block, error) {
	results := make([]Block, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		content, event, err := tools.execute(ctx, call.Name, call.Input)
		event.Stage, event.SessionID, event.Kind, event.Tool = stage, sessionID, "tool_result", call.Name
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			content = err.Error()
			event.Error = true
		}
		if err := e.record(ctx, event); err != nil {
			return nil, err
		}
		results = append(results, Block{Type: "tool_result", ToolUseID: call.ID, Content: content, IsError: event.Error})
	}
	return results, nil
}

func validateResponse(response ModelResponse, maxTools int) ([]Block, error) {
	calls := make([]Block, 0)
	ids := map[string]bool{}
	for _, block := range response.Content {
		if block.Type != "text" && block.Type != "tool_use" {
			return nil, errors.New("unsupported model content block; thinking must not be enabled for this runner")
		}
		if block.Type == "tool_use" {
			if block.ID == "" || ids[block.ID] {
				return nil, errors.New("model returned missing or duplicate tool call IDs")
			}
			ids[block.ID] = true
			calls = append(calls, block)
		}
	}
	if len(calls) > maxTools {
		return nil, errors.New("model returned too many tool calls")
	}
	if response.StopReason == "tool_use" && len(calls) > 0 {
		return calls, nil
	}
	if response.StopReason == "end_turn" && len(calls) == 0 && len(response.Content) > 0 {
		return calls, nil
	}
	return nil, &Failure{Type: "incomplete_model_turn", Message: "Model stopped without a complete end_turn or valid tool_use turn"}
}

func validateUsage(next, current Usage) error {
	a := []int64{next.InputTokens, next.OutputTokens, next.CacheReadInputTokens, next.CacheCreationInputTokens}
	b := []int64{current.InputTokens, current.OutputTokens, current.CacheReadInputTokens, current.CacheCreationInputTokens}
	var sum int64
	for i, value := range a {
		if value < 0 || b[i] < 0 || value > math.MaxInt64-b[i] {
			return errors.New("invalid model usage")
		}
		if sum > math.MaxInt64-value-b[i] {
			return errors.New("model usage overflow")
		}
		sum += value + b[i]
	}
	return nil
}

func totalTokens(u Usage) int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}
