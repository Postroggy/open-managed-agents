package dreams

import (
	"context"
	"encoding/json"
)

type Memory struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"content_sha256"`
}

type Store struct {
	ID          string
	Name        string
	Description string
}

type Transcript struct {
	SessionID string
	Text      string
}

type Backend interface {
	Store(context.Context, string) (Store, error)
	CreateStore(context.Context, string, string) (Store, error)
	List(context.Context, string) ([]Memory, error)
	Read(context.Context, string, string) (Memory, error)
	Write(context.Context, string, Memory, string) (Memory, error)
	Transcript(context.Context, string) (Transcript, error)
}

type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ModelRequest struct {
	Model     string    `json:"model"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools"`
	MaxTokens int       `json:"max_tokens"`
}

type ModelResponse struct {
	Content    []Block `json:"content"`
	StopReason string  `json:"stop_reason"`
	Usage      Usage   `json:"usage"`
}

type ModelClient interface {
	Complete(context.Context, ModelRequest) (ModelResponse, error)
}

type Event struct {
	StoreID   string `json:"memory_store_id,omitempty"`
	Stage     string `json:"stage"`
	SessionID string `json:"session_id,omitempty"`
	Kind      string `json:"kind"`
	Tool      string `json:"tool,omitempty"`
	Path      string `json:"path,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Error     bool   `json:"error,omitempty"`
	Usage     Usage  `json:"usage"`
}

type Observer interface {
	Record(context.Context, Event) error
}
