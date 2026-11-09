package dreamclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

func hashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func TestMemoryReadAndVersionedWriteRoundTrip(t *testing.T) {
	content := "old"
	writes := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("beta") != "true" || r.Header.Get("anthropic-beta") != memoryBeta {
			t.Error("missing memory beta")
		}
		item := memoryResponse{ID: "mem_one", Path: "/note.md", SHA256: hashText(content)}
		switch {
		case r.URL.Path == "/v1/memory_stores/memstore_one/memories" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(page[memoryResponse]{Data: []memoryResponse{item}})
		case r.URL.Path == "/v1/memory_stores/memstore_one/memories/mem_one" && r.Method == http.MethodGet:
			if r.URL.Query().Get("view") != "full" {
				t.Error("missing full view")
			}
			item.Content = &content
			_ = json.NewEncoder(w).Encode(item)
		case r.URL.Path == "/v1/memory_stores/memstore_one/memories/mem_one" && r.Method == http.MethodPost:
			writes++
			var body memoryWrite
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if body.Precondition == nil || body.Precondition.Type != "content_sha256" || body.Precondition.SHA256 != hashText(content) {
				t.Error("missing server-side CAS")
			}
			content = body.Content
			item.Content = &content
			item.SHA256 = hashText(content)
			_ = json.NewEncoder(w).Encode(item)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(400)
		}
	})
	memory, err := c.Read(context.Background(), "memstore_one", "/note.md")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := c.Write(context.Background(), "memstore_one", dreams.Memory{Path: "/note.md", Content: "new"}, memory.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != "new" || writes != 1 {
		t.Fatal("update failed")
	}
	if _, err := c.Write(context.Background(), "memstore_one", dreams.Memory{Path: "/note.md", Content: "stale"}, memory.SHA256); err == nil || writes != 1 {
		t.Fatal("stale write reached server")
	}
}

func TestMemoryRejectsArchivedAndCorruptResponses(t *testing.T) {
	t.Run("archived", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"id":"memstore_one","archived_at":"2026-11-09T00:00:00Z"}`))
		})
		if _, err := c.Store(context.Background(), "memstore_one"); err == nil {
			t.Fatal("accepted archived store")
		}
	})
	t.Run("missing body", func(t *testing.T) {
		if _, err := fullMemory(memoryResponse{ID: "mem_one"}); err == nil {
			t.Fatal("accepted absent body as empty text")
		}
	})
	t.Run("hash", func(t *testing.T) {
		s := "text"
		if _, err := fullMemory(memoryResponse{ID: "mem_one", Content: &s, SHA256: "wrong"}); err == nil {
			t.Fatal("accepted corrupt body")
		}
	})
	t.Run("new store identity", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"id":"../x"}`)) })
		if _, err := c.CreateStore(context.Background(), "name", "desc"); err == nil {
			t.Fatal("accepted invalid output identity")
		}
	})
	t.Run("duplicate path", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"a","path":"/x"},{"id":"b","path":"/x"}],"next_page":null}`))
		})
		if _, err := c.List(context.Background(), "memstore_one"); err == nil {
			t.Fatal("accepted duplicate catalog paths")
		}
	})
	t.Run("error privacy", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"error":{"type":"secret with spaces","message":"secret"}}`))
		})
		_, err := c.Store(context.Background(), "memstore_one")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("error leaked server text")
		}
	})
}
