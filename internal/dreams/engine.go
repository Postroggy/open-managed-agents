package dreams

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Limits struct {
	MaxTurns           int
	MaxToolsPerTurn    int
	MaxOutputTokens    int
	MaxTotalTokens     int64
	MaxContextBytes    int
	MaxTranscriptBytes int
	MaxMemoryBytes     int
	MaxMemories        int
	Timeout            time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxTurns: 48, MaxToolsPerTurn: 64, MaxOutputTokens: 8192, MaxTotalTokens: 1000000, MaxContextBytes: 2 << 20, MaxTranscriptBytes: 32 << 20, MaxMemoryBytes: 102400, MaxMemories: 2000, Timeout: time.Hour}
}

func (l Limits) Validate() error {
	if l.MaxTurns < 1 || l.MaxToolsPerTurn < 1 || l.MaxOutputTokens < 1 || l.MaxTotalTokens < 1 || l.MaxContextBytes < 1 || l.MaxTranscriptBytes < 1 || l.MaxMemoryBytes < 1 || l.MaxMemoryBytes > 102400 || l.MaxMemories < 1 || l.MaxMemories > 2000 || l.Timeout <= 0 {
		return invalid("all limits must be positive; memory limits cannot exceed OMA limits")
	}
	return nil
}

type Engine struct {
	Backend  Backend
	Model    ModelClient
	Observer Observer
	Limits   Limits
}

var ErrUpdateRequiresCoordinator = errors.New("update_existing requires a durable, fenced store coordinator; the standalone engine supports create_new only")

func (e *Engine) Run(ctx context.Context, dream *Dream) error {
	if e.Backend == nil || e.Model == nil || dream == nil {
		return invalid("backend, model and dream are required")
	}
	if err := e.Limits.Validate(); err != nil {
		return err
	}
	req, err := Normalize(dream.Request)
	if err != nil {
		return err
	}
	if req.OutputBehavior.Type == "update_existing" {
		return ErrUpdateRequiresCoordinator
	}
	if err := dream.Start(); err != nil {
		return err
	}
	dream.Request = req
	runCtx, cancel := context.WithTimeout(ctx, e.Limits.Timeout)
	defer cancel()
	err = e.run(runCtx, dream)
	if err == nil {
		err = runCtx.Err()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			_ = dream.Cancel(time.Now())
			return err
		}
		failure := classifyFailure(err)
		_ = dream.Finish(Failed, failure, time.Now())
		return err
	}
	return dream.Finish(Completed, nil, time.Now())
}

func classifyFailure(err error) *Failure {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Failure{Type: "timeout", Message: "Dream exceeded its runtime budget"}
	}
	return &Failure{Type: "internal_error", Message: "Dream execution failed; inspect the private run log"}
}

func (e *Engine) run(ctx context.Context, d *Dream) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := e.Backend.Store(ctx, d.Inputs[0].MemoryStoreID)
	if err != nil {
		return inputFailure(ctx, "input_memory_store_unavailable", "Input memory store is unavailable")
	}
	output, err := e.Backend.CreateStore(ctx, "Dream "+d.ID, source.Description)
	if err != nil {
		return fmt.Errorf("create output store: %w", err)
	}
	d.Outputs = []Output{{Type: "memory_store", MemoryStoreID: output.ID}}
	if err := e.record(ctx, Event{Stage: "copy", Kind: "output_created", StoreID: output.ID}); err != nil {
		return err
	}
	if output.ID == source.ID {
		return errors.New("backend returned the input store as the new output")
	}
	if err := e.copyStore(ctx, source.ID, output.ID); err != nil {
		return err
	}
	transcripts := make(map[string]Transcript, len(d.Inputs[1].SessionIDs))
	totalTranscriptBytes := 0
	for _, id := range d.Inputs[1].SessionIDs {
		if _, err := e.Backend.Store(ctx, source.ID); err != nil {
			return inputFailure(ctx, "input_memory_store_unavailable", "Input store became unavailable")
		}
		transcript, err := e.Backend.Transcript(ctx, id)
		if err != nil {
			return inputFailure(ctx, "input_session_unavailable", "Input session is unavailable or cannot be fully exported")
		}
		if transcript.SessionID != id {
			return errors.New("transcript identity mismatch")
		}
		if len(transcript.Text) > e.Limits.MaxTranscriptBytes-totalTranscriptBytes {
			return &Failure{Type: "input_session_too_large", Message: "Transcript exceeds the configured export limit"}
		}
		totalTranscriptBytes += len(transcript.Text)
		transcripts[id] = transcript
		tools := newToolkit(e.Backend, output.ID, map[string]Transcript{id: transcript}, e.Limits)
		if err := e.stage(ctx, d, "analyze", id, analyzerPrompt, tools); err != nil {
			return err
		}
	}
	if _, err := e.Backend.Store(ctx, source.ID); err != nil {
		return inputFailure(ctx, "input_memory_store_unavailable", "Input store became unavailable")
	}
	tools := newToolkit(e.Backend, output.ID, transcripts, e.Limits)
	if err := e.stage(ctx, d, "organize", "", organizerPrompt, tools); err != nil {
		return err
	}
	return e.validateOutput(ctx, output.ID)
}

func (e *Engine) copyStore(ctx context.Context, sourceID, targetID string) error {
	memories, err := e.Backend.List(ctx, sourceID)
	if err != nil {
		return err
	}
	if len(memories) > e.Limits.MaxMemories {
		return &Failure{Type: "input_memory_store_too_large", Message: "Input store exceeds the configured document limit"}
	}
	sort.Slice(memories, func(i, j int) bool { return memories[i].Path < memories[j].Path })
	for _, item := range memories {
		if err := ctx.Err(); err != nil {
			return err
		}
		memory, err := e.Backend.Read(ctx, sourceID, item.Path)
		if err != nil {
			return err
		}
		if item.SHA256 != "" && memory.SHA256 != item.SHA256 {
			return &Failure{Type: "input_memory_store_changed", Message: "Input changed while its copy was being made; retry from a stable store"}
		}
		if len(memory.Content) > e.Limits.MaxMemoryBytes {
			return &Failure{Type: "input_memory_store_too_large", Message: "Input document exceeds the configured size limit"}
		}
		if _, err := e.Backend.Write(ctx, targetID, Memory{Path: memory.Path, Content: memory.Content}, ""); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) record(ctx context.Context, event Event) error {
	if e.Observer == nil {
		return nil
	}
	return e.Observer.Record(ctx, event)
}

func inputFailure(ctx context.Context, kind, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return &Failure{Type: kind, Message: message}
}

func (e *Engine) validateOutput(ctx context.Context, storeID string) error {
	items, err := e.Backend.List(ctx, storeID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	if len(items) > e.Limits.MaxMemories {
		return &Failure{Type: "invalid_output", Message: "Output exceeds the memory count limit"}
	}
	indexed := false
	for _, item := range items {
		if err := validPath(item.Path); err != nil {
			return err
		}
		if item.Path == "/_index.md" {
			index, err := e.Backend.Read(ctx, storeID, item.Path)
			if err != nil {
				return err
			}
			if index.Content != "" {
				indexed = true
			}
		}
	}
	if indexed {
		return nil
	}
	return &Failure{Type: "invalid_output", Message: "Organizer did not produce a nonempty /_index.md"}
}
