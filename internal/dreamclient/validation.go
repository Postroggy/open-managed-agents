package dreamclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

var retirementStub = regexp.MustCompile(`^MOVED TO \[\[([a-z0-9][a-z0-9-]*)\]\]$`)

func (c *Client) ValidateInputs(ctx context.Context, storeID string, sessionIDs []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := c.Store(ctx, storeID); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &dreams.Failure{Type: "input_memory_store_unavailable", Message: "Input memory store is unavailable"}
	}
	for _, id := range sessionIDs {
		endpoint, err := resourcePath("sessions", id)
		if err != nil {
			return err
		}
		var session sessionResponse
		if err := c.request(ctx, http.MethodGet, endpoint, sessionBeta, nil, nil, &session); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &dreams.Failure{Type: "input_session_unavailable", Message: "An input session is unavailable"}
		}
		if session.ID != id {
			return &dreams.Failure{Type: "input_session_unavailable", Message: "Input session identity changed"}
		}
	}
	return nil
}

func (c *Client) DeleteRetiredMemory(ctx context.Context, storeID string, memory dreams.Memory) error {
	if !retirementStub.MatchString(strings.TrimSpace(memory.Content)) || memory.SHA256 == "" {
		return errors.New("only exact retirement stubs may be removed")
	}
	current, err := c.ReadMemory(ctx, storeID, memory)
	if err != nil {
		return err
	}
	if current.Content != memory.Content || current.SHA256 != memory.SHA256 {
		return errors.New("retirement stub changed")
	}
	endpoint, err := resourcePath("memory_stores", storeID)
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, endpoint+"/memories/"+memory.ID, memoryBeta,
		url.Values{"expected_content_sha256": {memory.SHA256}}, nil, nil)
}
