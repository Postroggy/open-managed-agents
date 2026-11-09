package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	mu          sync.Mutex
	stores      map[string]map[string]Memory
	transcripts map[string]string
	creates     int
	writes      int
	failRead    bool
}

func fixtureBackend() *fakeBackend {
	return &fakeBackend{stores: map[string]map[string]Memory{"memstore_source": {"/note.md": {ID: "mem_note", Path: "/note.md", Content: "Old claim: east", SHA256: digest("Old claim: east")}}}, transcripts: map[string]string{"sesn_a": "event_1: tool confirms the entrance is west", "sesn_b": "event_2: user confirms the entrance is west"}}
}
func (b *fakeBackend) Store(ctx context.Context, id string) (Store, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Store{}, err
	}
	if b.stores[id] == nil {
		return Store{}, errors.New("missing")
	}
	return Store{ID: id, Name: id}, nil
}
func (b *fakeBackend) CreateStore(ctx context.Context, name, desc string) (Store, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Store{}, err
	}
	b.creates++
	id := fmt.Sprintf("memstore_output%d", b.creates)
	b.stores[id] = map[string]Memory{}
	return Store{ID: id, Name: name, Description: desc}, nil
}
func (b *fakeBackend) List(ctx context.Context, id string) ([]Memory, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.stores[id] == nil {
		return nil, errors.New("missing")
	}
	items := []Memory{}
	for _, v := range b.stores[id] {
		items = append(items, v)
	}
	return items, nil
}
func (b *fakeBackend) Read(ctx context.Context, id, path string) (Memory, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	if b.failRead {
		return Memory{}, errors.New("unavailable")
	}
	m, ok := b.stores[id][path]
	if !ok {
		return Memory{}, errors.New("missing")
	}
	return m, nil
}
func (b *fakeBackend) Write(ctx context.Context, id string, m Memory, expected string) (Memory, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	if b.stores[id] == nil {
		return Memory{}, errors.New("missing")
	}
	old, ok := b.stores[id][m.Path]
	if (expected == "" && ok) || (expected != "" && (!ok || expected != old.SHA256)) {
		return Memory{}, errors.New("conflict")
	}
	m.ID = "mem_" + digest(m.Path)[:16]
	m.SHA256 = digest(m.Content)
	b.stores[id][m.Path] = m
	b.writes++
	return m, nil
}
func (b *fakeBackend) Transcript(ctx context.Context, id string) (Transcript, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Transcript{}, err
	}
	s, ok := b.transcripts[id]
	if !ok {
		return Transcript{}, errors.New("missing")
	}
	return Transcript{SessionID: id, Text: s}, nil
}

type modelFunc func(context.Context, ModelRequest) (ModelResponse, error)

func (f modelFunc) Complete(ctx context.Context, r ModelRequest) (ModelResponse, error) {
	return f(ctx, r)
}

type eventLog struct {
	events []Event
	fail   bool
}

func (l *eventLog) Record(_ context.Context, event Event) error {
	if l.fail {
		return errors.New("audit unavailable")
	}
	l.events = append(l.events, event)
	return nil
}

func toolResponse(id, name string, args any) ModelResponse {
	raw, _ := json.Marshal(args)
	return ModelResponse{Content: []Block{{Type: "tool_use", ID: id, Name: name, Input: raw}}, StopReason: "tool_use", Usage: Usage{InputTokens: 10, OutputTokens: 2, CacheReadInputTokens: 1}}
}
func doneResponse() ModelResponse {
	return ModelResponse{Content: []Block{{Type: "text", Text: "done"}}, StopReason: "end_turn", Usage: Usage{InputTokens: 5, OutputTokens: 1}}
}
func newDream(t *testing.T) *Dream {
	t.Helper()
	d, err := New("drm_test", requestFixture(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func scriptedModel(t *testing.T) modelFunc {
	return func(ctx context.Context, r ModelRequest) (ModelResponse, error) {
		t.Helper()
		if strings.Contains(r.System, "event_1") || strings.Contains(r.Messages[0].Content[0].Text, "event_1") {
			t.Fatal("transcript injected into system or initial task")
		}
		stageTurn := (len(r.Messages) - 1) / 2
		if strings.Contains(r.System, analyzerPrompt) {
			id := "sesn_a"
			if strings.Contains(r.Messages[0].Content[0].Text, "sesn_b") {
				id = "sesn_b"
			}
			switch stageTurn {
			case 0:
				return toolResponse("call_source", "read", toolArgs{Target: "source", SessionID: id}), nil
			case 1:
				return toolResponse("call_read", "read", toolArgs{Target: "memory", Path: "/note.md"}), nil
			case 2:
				var result readResult
				if err := json.Unmarshal([]byte(r.Messages[len(r.Messages)-1].Content[0].Content), &result); err != nil {
					t.Fatal(err)
				}
				return toolResponse("call_write", "write", toolArgs{Path: "/note.md", Content: "Verified west; sources sesn_a/event_1 and sesn_b/event_2", ExpectedSHA256: result.SHA256}), nil
			default:
				return doneResponse(), nil
			}
		}
		if stageTurn == 0 {
			return toolResponse("call_index", "write", toolArgs{Path: "/_index.md", Content: "[Entrance](note.md)"}), nil
		}
		return doneResponse(), nil
	}
}

func TestEngineEndToEndCreatesIndependentOutput(t *testing.T) {
	backend := fixtureBackend()
	events := &eventLog{}
	d := newDream(t)
	engine := Engine{Backend: backend, Model: scriptedModel(t), Observer: events, Limits: DefaultLimits()}
	if err := engine.Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if d.Status != Completed || len(d.Outputs) != 1 {
		t.Fatalf("unexpected result %+v", d)
	}
	if backend.stores["memstore_source"]["/note.md"].Content != "Old claim: east" {
		t.Fatal("source mutated")
	}
	if !strings.Contains(backend.stores[d.Outputs[0].MemoryStoreID]["/note.md"].Content, "west") {
		t.Fatal("output not persisted")
	}
	if d.Usage.InputTokens != 85 || d.Usage.OutputTokens != 17 || d.Usage.CacheReadInputTokens != 7 {
		t.Fatalf("usage not fully accounted: %+v", d.Usage)
	}
	var stages []string
	for _, event := range events.events {
		if event.Kind == "stage_started" {
			stages = append(stages, event.Stage+":"+event.SessionID)
		}
	}
	if strings.Join(stages, ",") != "analyze:sesn_a,analyze:sesn_b,organize:" {
		t.Fatalf("wrong phase barrier: %v", stages)
	}
	raw, _ := json.Marshal(events.events)
	if strings.Contains(string(raw), "Old claim") {
		t.Fatal("audit contains memory body")
	}
}

func TestEngineRefusesIncompleteTranscriptAndPreservesPartial(t *testing.T) {
	backend := fixtureBackend()
	d := newDream(t)
	engine := Engine{Backend: backend, Model: modelFunc(func(context.Context, ModelRequest) (ModelResponse, error) { return doneResponse(), nil }), Limits: DefaultLimits()}
	err := engine.Run(context.Background(), d)
	if err == nil || d.Status != Failed || d.Error.Type != "incomplete_transcript_read" {
		t.Fatalf("unexpected result %+v %v", d, err)
	}
	if len(d.Outputs) != 1 || backend.stores[d.Outputs[0].MemoryStoreID]["/note.md"].Content == "" {
		t.Fatal("partial output discarded")
	}
}

func TestEngineCancellationPreventsFurtherWrites(t *testing.T) {
	backend := fixtureBackend()
	d := newDream(t)
	ctx, cancel := context.WithCancel(context.Background())
	engine := Engine{Backend: backend, Limits: DefaultLimits(), Model: modelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
		cancel()
		return toolResponse("write", "write", toolArgs{Path: "/bad.md", Content: "must not be written"}), nil
	})}
	err := engine.Run(ctx, d)
	if !errors.Is(err, context.Canceled) || d.Status != Canceled {
		t.Fatalf("unexpected cancellation %v %+v", err, d)
	}
	if _, ok := backend.stores[d.Outputs[0].MemoryStoreID]["/bad.md"]; ok {
		t.Fatal("write executed after cancellation")
	}
}

func TestEngineRejectsUnsafeUpdateBeforeSideEffects(t *testing.T) {
	b := fixtureBackend()
	d := newDream(t)
	d.OutputBehavior = OutputBehavior{Type: "update_existing", MemoryStoreID: "memstore_source"}
	e := Engine{Backend: b, Model: scriptedModel(t), Limits: DefaultLimits()}
	if err := e.Run(context.Background(), d); !errors.Is(err, ErrUpdateRequiresCoordinator) {
		t.Fatal(err)
	}
	if b.creates != 0 || b.writes != 0 || d.Status != Pending {
		t.Fatal("unsupported mode had side effects")
	}
}

func TestEngineFailureBudgets(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		limits     func(*Limits)
		model      modelFunc
	}{
		{"context", "context_budget_exceeded", func(l *Limits) { l.MaxContextBytes = 1 }, scriptedModel(t)},
		{"tokens", "token_budget_exceeded", func(l *Limits) { l.MaxTotalTokens = 1 }, scriptedModel(t)},
		{"turns", "turn_limit_exceeded", func(l *Limits) { l.MaxTurns = 1 }, scriptedModel(t)},
		{"transcript bytes", "input_session_too_large", func(l *Limits) { l.MaxTranscriptBytes = 1 }, scriptedModel(t)},
		{"model truncation", "incomplete_model_turn", func(*Limits) {}, modelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
			return ModelResponse{StopReason: "max_tokens"}, nil
		})},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := fixtureBackend()
			d := newDream(t)
			limits := DefaultLimits()
			test.limits(&limits)
			e := Engine{Backend: b, Model: test.model, Limits: limits}
			err := e.Run(context.Background(), d)
			if err == nil || d.Error == nil || d.Error.Type != test.kind {
				t.Fatalf("expected %s, got %v %+v", test.kind, err, d.Error)
			}
		})
	}
}

func TestEngineAuditFailureStopsBeforeCopy(t *testing.T) {
	b := fixtureBackend()
	d := newDream(t)
	e := Engine{Backend: b, Model: scriptedModel(t), Observer: &eventLog{fail: true}, Limits: DefaultLimits()}
	if err := e.Run(context.Background(), d); err == nil {
		t.Fatal("ignored failed audit")
	}
	if b.writes != 0 || len(d.Outputs) != 1 {
		t.Fatal("wrote without output checkpoint")
	}
}

func TestResponseValidation(t *testing.T) {
	valid := toolResponse("a", "read", toolArgs{Target: "source", SessionID: "s"})
	tests := []ModelResponse{{StopReason: "max_tokens"}, {StopReason: "end_turn"}, {StopReason: "end_turn", Content: valid.Content}, {StopReason: "tool_use", Content: []Block{{Type: "text", Text: "no calls"}}}, {StopReason: "tool_use", Content: append(valid.Content, valid.Content...)}, {StopReason: "end_turn", Content: []Block{{Type: "thinking"}}}}
	for i, r := range tests {
		if _, err := validateResponse(r, 10); err == nil {
			t.Errorf("accepted case %d", i)
		}
	}
	if _, err := validateResponse(valid, 0); err == nil {
		t.Fatal("ignored max tools")
	}
	if err := validateUsage(Usage{InputTokens: -1}, Usage{}); err == nil {
		t.Fatal("accepted negative usage")
	}
	if err := validateUsage(Usage{InputTokens: 1 << 62, OutputTokens: 1 << 62}, Usage{}); err == nil {
		t.Fatal("accepted usage overflow")
	}
}
