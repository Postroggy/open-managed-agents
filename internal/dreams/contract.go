package dreams

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const Beta = "dreaming-2026-04-21"

type Model struct {
	ID    string `json:"id"`
	Speed string `json:"speed"`
}

type Input struct {
	Type          string   `json:"type"`
	MemoryStoreID string   `json:"memory_store_id,omitempty"`
	SessionIDs    []string `json:"session_ids,omitempty"`
}

type OutputBehavior struct {
	Type          string `json:"type"`
	MemoryStoreID string `json:"memory_store_id,omitempty"`
}

type Request struct {
	Inputs         []Input        `json:"inputs"`
	Model          Model          `json:"model"`
	Instructions   *string        `json:"instructions"`
	OutputBehavior OutputBehavior `json:"output_behavior"`
}

type createEnvelope struct {
	Inputs         []Input         `json:"inputs"`
	Model          json.RawMessage `json:"model"`
	Instructions   *string         `json:"instructions"`
	OutputBehavior *OutputBehavior `json:"output_behavior"`
}

func DecodeRequest(r io.Reader) (Request, error) {
	var wire createEnvelope
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Request{}, invalid("invalid JSON: %v", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Request{}, invalid("expected one JSON object")
	}
	model, err := decodeModel(wire.Model)
	if err != nil {
		return Request{}, err
	}
	req := Request{Inputs: wire.Inputs, Model: model, Instructions: wire.Instructions, OutputBehavior: OutputBehavior{Type: "create_new"}}
	if wire.OutputBehavior != nil {
		if wire.OutputBehavior.Type == "" {
			return Request{}, invalid("output_behavior.type is required")
		}
		req.OutputBehavior = *wire.OutputBehavior
	}
	return Normalize(req)
}

func decodeModel(raw json.RawMessage) (Model, error) {
	var model Model
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return model, invalid("model is required")
	}
	if err := json.Unmarshal(raw, &model.ID); err == nil {
		model.Speed = "standard"
		return model, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&model); err != nil {
		return model, invalid("model must be a string or an object with id and speed")
	}
	if model.Speed == "" {
		model.Speed = "standard"
	}
	return model, nil
}

func Normalize(req Request) (Request, error) {
	if len(req.Inputs) != 2 {
		return Request{}, invalid("inputs must contain one memory_store and one sessions entry")
	}
	if req.Model.Speed == "" {
		req.Model.Speed = "standard"
	}
	if !validText(req.Model.ID, 1, 256) || req.Model.Speed != "standard" {
		return Request{}, invalid("model requires a nonempty id of at most 256 characters and standard speed")
	}
	if req.Instructions != nil && !validText(*req.Instructions, 1, 4096) {
		return Request{}, invalid("instructions must contain 1 to 4096 characters")
	}
	store, sessions, err := normalizeInputs(req.Inputs)
	if err != nil {
		return Request{}, err
	}
	if req.OutputBehavior.Type == "" {
		req.OutputBehavior.Type = "create_new"
	}
	switch req.OutputBehavior.Type {
	case "create_new":
		if req.OutputBehavior.MemoryStoreID != "" {
			return Request{}, invalid("create_new cannot specify memory_store_id")
		}
	case "update_existing":
		if req.OutputBehavior.MemoryStoreID != store.MemoryStoreID {
			return Request{}, invalid("update_existing must name the input memory store")
		}
	default:
		return Request{}, invalid("output_behavior must be create_new or update_existing")
	}
	req.Inputs = []Input{store, sessions}
	if req.Instructions != nil {
		value := *req.Instructions
		req.Instructions = &value
	}
	return req, nil
}

func normalizeInputs(inputs []Input) (Input, Input, error) {
	var store, sessions Input
	for _, input := range inputs {
		switch input.Type {
		case "memory_store":
			if store.Type != "" || !validID(input.MemoryStoreID) || input.SessionIDs != nil {
				return store, sessions, invalid("invalid or duplicate memory_store input")
			}
			store = input
		case "sessions":
			if sessions.Type != "" || input.MemoryStoreID != "" || len(input.SessionIDs) < 1 || len(input.SessionIDs) > 100 {
				return store, sessions, invalid("sessions requires 1 to 100 unique IDs")
			}
			seen := make(map[string]bool, len(input.SessionIDs))
			for _, id := range input.SessionIDs {
				if !validID(id) || seen[id] {
					return store, sessions, invalid("session IDs must be nonempty and unique")
				}
				seen[id] = true
			}
			sessions = Input{Type: "sessions", SessionIDs: append([]string(nil), input.SessionIDs...)}
			sort.Strings(sessions.SessionIDs)
		default:
			return store, sessions, invalid("unsupported input type")
		}
	}
	if store.Type == "" || sessions.Type == "" {
		return store, sessions, invalid("both memory_store and sessions inputs are required")
	}
	return store, sessions, nil
}

func validText(value string, minimum, maximum int) bool {
	n := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && n >= minimum && n <= maximum
}

func validID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && !strings.ContainsAny(value, "/\\\x00\r\n\t")
}

var ErrInvalidRequest = errors.New("invalid dream request")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}
