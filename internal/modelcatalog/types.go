package modelcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const GlobalCatalogKey = "default"

var (
	ErrUnavailable       = errors.New("model catalog is unavailable")
	ErrUnknownModel      = errors.New("model is not in the catalog")
	ErrRefreshInProgress = errors.New("model catalog refresh is already in progress")
)

type Capabilities map[string]json.RawMessage

type KnownCapabilities struct {
	Batch             *bool
	Citations         *bool
	CodeExecution     *bool
	ContextManagement *bool
	ClearThinking     *bool
	ClearToolUses     *bool
	CompactContext    *bool
	Effort            *bool
	LowEffort         *bool
	MediumEffort      *bool
	HighEffort        *bool
	XHighEffort       *bool
	MaxEffort         *bool
	ImageInput        *bool
	PDFInput          *bool
	StructuredOutputs *bool
	Thinking          *bool
	ThinkingEnabled   *bool
	AdaptiveThinking  *bool
	ToolUse           *bool
}

type capabilityPayload struct {
	Supported *bool                      `json:"supported"`
	Types     map[string]json.RawMessage `json:"types"`
}

func (c *Capabilities) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = nil
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("capabilities must be an object: %w", err)
	}
	if err := validateKnownCapabilities(fields); err != nil {
		return err
	}
	*c = Capabilities(cloneCapabilityFields(fields))
	return nil
}

func (c *Capabilities) setSupported(name string, supported *bool) {
	if supported == nil {
		return
	}
	if *c == nil {
		*c = make(Capabilities)
	}
	(*c)[name] = mergeSupportedCapability((*c)[name], supported)
}

func (c Capabilities) Known() KnownCapabilities {
	thinkingFields := capabilityObject(c["thinking"])
	thinkingTypes := capabilityObject(thinkingFields["types"])
	contextFields := capabilityObject(c["context_management"])
	effortFields := capabilityObject(c["effort"])
	return KnownCapabilities{
		Batch:             capabilitySupported(c["batch"]),
		Citations:         capabilitySupported(c["citations"]),
		CodeExecution:     capabilitySupported(c["code_execution"]),
		ContextManagement: capabilitySupported(c["context_management"]),
		ClearThinking:     capabilitySupported(contextFields["clear_thinking_20251015"]),
		ClearToolUses:     capabilitySupported(contextFields["clear_tool_uses_20250919"]),
		CompactContext:    capabilitySupported(contextFields["compact_20260112"]),
		Effort:            capabilitySupported(c["effort"]),
		LowEffort:         capabilitySupported(effortFields["low"]),
		MediumEffort:      capabilitySupported(effortFields["medium"]),
		HighEffort:        capabilitySupported(effortFields["high"]),
		XHighEffort:       capabilitySupported(effortFields["xhigh"]),
		MaxEffort:         capabilitySupported(effortFields["max"]),
		ImageInput:        capabilitySupported(c["image_input"]),
		PDFInput:          capabilitySupported(c["pdf_input"]),
		StructuredOutputs: capabilitySupported(c["structured_outputs"]),
		Thinking:          capabilitySupported(c["thinking"]),
		ThinkingEnabled:   capabilitySupported(thinkingTypes["enabled"]),
		AdaptiveThinking:  capabilitySupported(thinkingTypes["adaptive"]),
		ToolUse:           capabilitySupported(c["tool_use"]),
	}
}

func validateKnownCapabilities(fields map[string]json.RawMessage) error {
	for _, name := range []string{
		"batch", "citations", "code_execution", "context_management", "effort",
		"image_input", "pdf_input", "structured_outputs", "thinking", "tool_use",
	} {
		if _, err := supportedCapability(fields[name], name); err != nil {
			return err
		}
	}
	thinkingFields := capabilityObject(fields["thinking"])
	thinkingTypes := capabilityObject(thinkingFields["types"])
	for _, name := range []string{"enabled", "adaptive"} {
		if _, err := supportedCapability(thinkingTypes[name], "thinking.types."+name); err != nil {
			return err
		}
	}
	contextFields := capabilityObject(fields["context_management"])
	for _, name := range []string{"clear_thinking_20251015", "clear_tool_uses_20250919", "compact_20260112"} {
		if _, err := supportedCapability(contextFields[name], "context_management."+name); err != nil {
			return err
		}
	}
	effortFields := capabilityObject(fields["effort"])
	for _, name := range []string{"low", "medium", "high", "xhigh", "max"} {
		if _, err := supportedCapability(effortFields[name], "effort."+name); err != nil {
			return err
		}
	}
	return nil
}

func supportedCapability(raw json.RawMessage, name string) (*bool, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var payload capabilityPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("%s capability must be an object: %w", name, err)
	}
	return cloneBool(payload.Supported), nil
}

func capabilitySupported(raw json.RawMessage) *bool {
	var payload capabilityPayload
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return cloneBool(payload.Supported)
}

func mergeSupportedCapability(raw json.RawMessage, supported *bool) json.RawMessage {
	fields := capabilityObject(raw)
	fields["supported"], _ = json.Marshal(*supported)
	encoded, _ := json.Marshal(fields)
	return encoded
}

func capabilityObject(raw json.RawMessage) map[string]json.RawMessage {
	fields := make(map[string]json.RawMessage)
	_ = json.Unmarshal(raw, &fields)
	return fields
}

func cloneCapabilityFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]json.RawMessage, len(fields))
	for name, value := range fields {
		cloned[name] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

type Model struct {
	ID             string       `json:"id"`
	DisplayName    string       `json:"display_name"`
	Description    string       `json:"description,omitempty"`
	CreatedAt      string       `json:"created_at,omitempty"`
	MaxInputTokens *int         `json:"max_input_tokens,omitempty"`
	MaxTokens      *int         `json:"max_tokens,omitempty"`
	Capabilities   Capabilities `json:"capabilities,omitempty"`
}

type Snapshot struct {
	Models           []Model    `json:"models"`
	DefaultModelID   string     `json:"default_model_id,omitempty"`
	LastAttemptAt    *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	Stale            bool       `json:"stale"`
	DefaultAvailable bool       `json:"default_available"`
}

type StoredSnapshot struct {
	Models        []Model
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	LastError     string
}

type Store interface {
	Load(context.Context) (StoredSnapshot, bool, error)
	SaveSuccess(context.Context, StoredSnapshot) error
	RecordFailure(context.Context, time.Time, string) error
}

type RefreshLocker interface {
	TryAcquireRefresh(context.Context) (release func(), acquired bool, err error)
}

type Page struct {
	Models  []Model
	HasMore bool
	LastID  string
}

type Upstream interface {
	List(context.Context, string) (Page, error)
}

type Reader interface {
	Snapshot(context.Context) (Snapshot, error)
	ValidateModel(context.Context, string) error
}

type Refresher interface {
	TryRefresh(context.Context) error
}

type UnavailableReader struct{}

func (UnavailableReader) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{}, ErrUnavailable
}

func (UnavailableReader) ValidateModel(context.Context, string) error {
	return ErrUnavailable
}

type Options struct {
	DefaultModelID  string
	RefreshInterval time.Duration
	RefreshTimeout  time.Duration
	Now             func() time.Time
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func IsUnknownModel(err error) bool {
	return errors.Is(err, ErrUnknownModel)
}
