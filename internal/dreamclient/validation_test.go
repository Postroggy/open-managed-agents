package dreamclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

func TestCatalogReadsAvoidRepeatedDirectoryRequests(t *testing.T) {
	var mu sync.Mutex
	catalogs, reads := 0, 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/memories") {
			catalogs++
			items := make([]memoryResponse, 40)
			for i := range items {
				items[i] = memoryResponse{ID: fmt.Sprintf("mem_%d", i), Path: fmt.Sprintf("/page-%d.md", i), SHA256: hashText("fact")}
			}
			_ = json.NewEncoder(w).Encode(page[memoryResponse]{Data: items})
			return
		}
		reads++
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		s := "fact"
		_ = json.NewEncoder(w).Encode(memoryResponse{ID: id, Path: "/page-" + strings.TrimPrefix(id, "mem_") + ".md", Content: &s, SHA256: hashText(s)})
	})
	items, err := c.List(context.Background(), "memstore_one")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if _, err := c.ReadMemory(context.Background(), "memstore_one", item); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if catalogs != 1 || reads != 40 {
		t.Fatalf("catalogs=%d reads=%d", catalogs, reads)
	}
}
func TestCatalogReadRejectsChangedIdentityOrVersion(t *testing.T) {
	for _, field := range []string{"id", "path", "hash"} {
		t.Run(field, func(t *testing.T) {
			s := "fact"
			item := memoryResponse{ID: "mem_one", Path: "/fact.md", Content: &s, SHA256: hashText(s)}
			switch field {
			case "id":
				item.ID = "mem_other"
			case "path":
				item.Path = "/other.md"
			case "hash":
				item.SHA256 = hashText("different")
			}
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(item) })
			if _, err := c.ReadMemory(context.Background(), "memstore_one", dreams.Memory{ID: "mem_one", Path: "/fact.md", SHA256: hashText("fact")}); err == nil {
				t.Fatal("accepted changed " + field)
			}
		})
	}
}
func TestInputValidationRechecksAllSessionsWithoutExporting(t *testing.T) {
	var mu sync.Mutex
	missing := ""
	calls := []string{}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.URL.Path)
		if strings.Contains(r.URL.Path, "threads") || strings.Contains(r.URL.Path, "events") {
			t.Error("metadata validation reexported transcripts")
		}
		if strings.HasSuffix(r.URL.Path, "/"+missing) && missing != "" {
			w.WriteHeader(404)
			return
		}
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "idle"})
	})
	ids := []string{"sesn_a", "sesn_b"}
	if err := c.ValidateInputs(context.Background(), "memstore_one", ids); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	missing = "sesn_b"
	mu.Unlock()
	err := c.ValidateInputs(context.Background(), "memstore_one", ids)
	var failure *dreams.Failure
	if !errors.As(err, &failure) || failure.Type != "input_session_unavailable" {
		t.Fatal(err)
	}
	mu.Lock()
	missing = "memstore_one"
	mu.Unlock()
	err = c.ValidateInputs(context.Background(), "memstore_one", ids)
	if !errors.As(err, &failure) || failure.Type != "input_memory_store_unavailable" {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 7 {
		t.Fatalf("unexpected metadata calls %v", calls)
	}
}
func TestRetiredDeletionUsesServerSideHashPrecondition(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			stub := "MOVED TO [[target]]"
			hash := hashText(stub)
			deletes := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/memory_stores/memstore_one/memories/mem_old" {
					t.Error(r.URL.Path)
					w.WriteHeader(400)
					return
				}
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(memoryResponse{ID: "mem_old", Path: "/old.md", Content: &stub, SHA256: hash})
					return
				}
				if r.Method != http.MethodDelete || r.URL.Query().Get("expected_content_sha256") != hash || r.URL.Query().Get("beta") != "true" {
					t.Error("missing atomic delete precondition")
					w.WriteHeader(400)
					return
				}
				deletes++
				if conflict {
					w.WriteHeader(409)
					return
				}
				w.WriteHeader(204)
			})
			err := c.DeleteRetiredMemory(context.Background(), "memstore_one", dreams.Memory{ID: "mem_old", Path: "/old.md", Content: stub, SHA256: hash})
			if (err != nil) != conflict || deletes != 1 {
				t.Fatalf("deletes=%d error=%v", deletes, err)
			}
			if err := c.DeleteRetiredMemory(context.Background(), "memstore_one", dreams.Memory{ID: "mem_old", Path: "/old.md", Content: "ordinary fact", SHA256: hash}); err == nil {
				t.Fatal("accepted deletion of facts")
			}
		})
	}
}
func TestMissingPageArrayCannotLookLikeEmptyMemory(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"data":null,"next_page":null}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			if _, err := c.List(context.Background(), "memstore_one"); err == nil {
				t.Fatal("omitted data treated as empty store")
			}
		})
	}
}
