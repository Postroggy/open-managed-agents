package dreamclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

const memoryBeta = "agent-memory-2026-07-22"

type storeResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ArchivedAt  *time.Time `json:"archived_at"`
}

type memoryResponse struct {
	ID      string  `json:"id"`
	Path    string  `json:"path"`
	SHA256  string  `json:"content_sha256"`
	Content *string `json:"content"`
}

func (c *Client) Store(ctx context.Context, id string) (dreams.Store, error) {
	endpoint, err := resourcePath("memory_stores", id)
	if err != nil {
		return dreams.Store{}, err
	}
	var result storeResponse
	if err := c.request(ctx, http.MethodGet, endpoint, memoryBeta, nil, nil, &result); err != nil {
		return dreams.Store{}, err
	}
	if result.ID != id || result.ArchivedAt != nil {
		return dreams.Store{}, errors.New("memory store is unavailable")
	}
	return dreams.Store{ID: result.ID, Name: result.Name, Description: result.Description}, nil
}
func (c *Client) CreateStore(ctx context.Context, name, description string) (dreams.Store, error) {
	var result storeResponse
	body := struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}{name, description}
	if err := c.request(ctx, http.MethodPost, "/v1/memory_stores", memoryBeta, nil, body, &result); err != nil {
		return dreams.Store{}, err
	}
	if !safeID(result.ID) {
		return dreams.Store{}, errors.New("OMA returned an invalid memory store ID")
	}
	return dreams.Store{ID: result.ID, Name: result.Name, Description: result.Description}, nil
}
func (c *Client) List(ctx context.Context, id string) ([]dreams.Memory, error) {
	endpoint, err := resourcePath("memory_stores", id)
	if err != nil {
		return nil, err
	}
	result := make([]dreams.Memory, 0)
	seen := map[string]bool{}
	err = pages[memoryResponse](ctx, c, endpoint+"/memories", memoryBeta, url.Values{"depth": {"0"}, "view": {"basic"}}, 2000, func(item memoryResponse) error {
		if !safeID(item.ID) || item.Path == "" || seen[item.Path] {
			return errors.New("invalid or duplicate memory catalog entry")
		}
		seen[item.Path] = true
		result = append(result, dreams.Memory{ID: item.ID, Path: item.Path, SHA256: item.SHA256})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
func (c *Client) lookup(ctx context.Context, storeID, p string) (dreams.Memory, error) {
	items, err := c.List(ctx, storeID)
	if err != nil {
		return dreams.Memory{}, err
	}
	for _, item := range items {
		if item.Path == p {
			return item, nil
		}
	}
	return dreams.Memory{}, &APIError{Status: http.StatusNotFound, Type: "not_found_error"}
}
func (c *Client) Read(ctx context.Context, storeID, p string) (dreams.Memory, error) {
	item, err := c.lookup(ctx, storeID, p)
	if err != nil {
		return dreams.Memory{}, err
	}
	return c.ReadMemory(ctx, storeID, item)
}
func (c *Client) ReadMemory(ctx context.Context, storeID string, item dreams.Memory) (dreams.Memory, error) {
	endpoint, err := resourcePath("memory_stores", storeID)
	if err != nil {
		return dreams.Memory{}, err
	}
	if !safeID(item.ID) {
		return dreams.Memory{}, errors.New("invalid memory ID")
	}
	var result memoryResponse
	if err := c.request(ctx, http.MethodGet, endpoint+"/memories/"+item.ID, memoryBeta, url.Values{"view": {"full"}}, nil, &result); err != nil {
		return dreams.Memory{}, err
	}
	if result.ID != item.ID || result.Path != item.Path {
		return dreams.Memory{}, errors.New("memory identity changed during read")
	}
	if item.SHA256 != "" && result.SHA256 != item.SHA256 {
		return dreams.Memory{}, errors.New("memory version changed during read")
	}
	return fullMemory(result)
}

type precondition struct {
	Type   string `json:"type"`
	SHA256 string `json:"content_sha256"`
}

type memoryWrite struct {
	Path         string        `json:"path"`
	Content      string        `json:"content"`
	Precondition *precondition `json:"precondition,omitempty"`
}

func (c *Client) Write(ctx context.Context, storeID string, memory dreams.Memory, expected string) (dreams.Memory, error) {
	endpoint, err := resourcePath("memory_stores", storeID)
	if err != nil {
		return dreams.Memory{}, err
	}
	endpoint += "/memories"
	body := memoryWrite{Path: memory.Path, Content: memory.Content}
	if expected != "" {
		current, err := c.lookup(ctx, storeID, memory.Path)
		if err != nil {
			return dreams.Memory{}, err
		}
		if current.SHA256 != expected {
			return dreams.Memory{}, &APIError{Status: http.StatusConflict, Type: "memory_precondition_failed_error"}
		}
		endpoint += "/" + current.ID
		body.Precondition = &precondition{Type: "content_sha256", SHA256: expected}
	}
	var result memoryResponse
	if err := c.request(ctx, http.MethodPost, endpoint, memoryBeta, url.Values{"view": {"full"}}, body, &result); err != nil {
		return dreams.Memory{}, err
	}
	if result.Path != memory.Path {
		return dreams.Memory{}, errors.New("OMA returned a mismatched memory path")
	}
	return fullMemory(result)
}

func fullMemory(result memoryResponse) (dreams.Memory, error) {
	if !safeID(result.ID) || result.Content == nil {
		return dreams.Memory{}, errors.New("OMA omitted full memory content")
	}
	hash := sha256.Sum256([]byte(*result.Content))
	if result.SHA256 != hex.EncodeToString(hash[:]) {
		return dreams.Memory{}, errors.New("OMA memory content hash mismatch")
	}
	return dreams.Memory{ID: result.ID, Path: result.Path, Content: *result.Content, SHA256: result.SHA256}, nil
}
func (c *Client) Complete(ctx context.Context, request dreams.ModelRequest) (dreams.ModelResponse, error) {
	var response dreams.ModelResponse
	err := c.request(ctx, http.MethodPost, "/v1/messages", "", nil, request, &response)
	return response, err
}
