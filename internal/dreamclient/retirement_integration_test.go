package dreamclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

func TestEngineRetirementThroughOMAHTTP(t *testing.T) {
	for _, interfere := range []bool{false, true} {
		name := "complete"
		if interfere {
			name = "concurrent-stub-replacement"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			stores := map[string]map[string]memoryResponse{}
			put := func(store, id, path, content string) {
				if stores[store] == nil {
					stores[store] = map[string]memoryResponse{}
				}
				stores[store][id] = memoryResponse{ID: id, Path: path, Content: &content, SHA256: hashText(content)}
			}
			put("source", "mem_a", "/a.md", "First fact.")
			put("source", "mem_b", "/b.md", "Second fact.")
			merge := "---\nname: a\ndescription: Both verified facts\n---\nFirst fact. Second fact."
			step := 0
			deletes := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				emit := func(value any) { _ = json.NewEncoder(w).Encode(value) }
				fail := func(code int) {
					w.WriteHeader(code)
					emit(map[string]any{"error": map[string]string{"type": "conflict_error"}})
				}
				switch r.URL.Path {
				case "/v1/sessions/sesn_input":
					emit(sessionResponse{ID: "sesn_input", Status: "idle", UpdatedAt: "fixed"})
					return
				case "/v1/sessions/sesn_input/threads":
					emit(page[threadResponse]{Data: []threadResponse{{ID: "sthr_main"}}})
					return
				case "/v1/sessions/sesn_input/threads/sthr_main/events":
					emit(map[string]any{"data": []any{map[string]string{"id": "event_fact", "type": "agent.message", "text": "First fact and second fact are verified."}}})
					return
				case "/v1/memory_stores":
					stores["output"] = map[string]memoryResponse{}
					emit(storeResponse{ID: "output"})
					return
				case "/v1/messages":
					var request dreams.ModelRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
						fail(400)
						return
					}
					call := func(name string, args map[string]any) {
						raw, _ := json.Marshal(args)
						emit(dreams.ModelResponse{StopReason: "tool_use", Content: []dreams.Block{{Type: "tool_use", ID: "call", Name: name, Input: raw}}, Usage: dreams.Usage{InputTokens: 2, OutputTokens: 1}})
					}
					read := func(path string) { call("read", map[string]any{"target": "memory", "path": path}) }
					write := func(path, content, expected string) {
						args := map[string]any{"path": path, "content": content}
						if expected != "" {
							args["expected_sha256"] = expected
						}
						call("write", args)
					}
					if strings.Contains(request.Messages[0].Content[0].Text, "analyze") {
						if len(request.Messages) == 1 {
							call("read", map[string]any{"target": "source", "session_id": "sesn_input"})
							return
						}
					} else {
						step++
						switch step {
						case 1:
							read("/a.md")
							return
						case 2:
							read("/b.md")
							return
						case 3:
							write("/a.md", merge, hashText("First fact."))
							return
						case 4:
							read("/a.md")
							return
						case 5:
							write("/b.md", "MOVED TO [[a]]", hashText("Second fact."))
							return
						case 6:
							read("/b.md")
							return
						case 7:
							write("/_index.md", "## Facts\n[[a]] — verified facts", "")
							return
						case 8:
							read("/_index.md")
							return
						}
					}
					emit(dreams.ModelResponse{StopReason: "end_turn", Content: []dreams.Block{{Type: "text", Text: "done"}}, Usage: dreams.Usage{InputTokens: 2, OutputTokens: 1}})
					return
				}
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				if len(parts) < 3 || parts[1] != "memory_stores" {
					t.Errorf("unexpected route %s", r.URL)
					fail(404)
					return
				}
				store := parts[2]
				items, ok := stores[store]
				if !ok {
					fail(404)
					return
				}
				if len(parts) == 3 {
					emit(storeResponse{ID: store})
					return
				}
				if len(parts) == 4 && r.Method == http.MethodGet {
					all := []memoryResponse{}
					for _, m := range items {
						m.Content = nil
						all = append(all, m)
					}
					emit(page[memoryResponse]{Data: all})
					return
				}
				if len(parts) == 5 && r.Method == http.MethodGet {
					m, ok := items[parts[4]]
					if !ok {
						fail(404)
						return
					}
					emit(m)
					return
				}
				if r.Method == http.MethodPost {
					var body memoryWrite
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						fail(400)
						return
					}
					id := "mem_" + hashText(body.Path)[:12]
					if len(parts) == 5 {
						id = parts[4]
						current, ok := items[id]
						if !ok || body.Precondition == nil || body.Precondition.SHA256 != current.SHA256 {
							fail(409)
							return
						}
					}
					put(store, id, body.Path, body.Content)
					emit(stores[store][id])
					return
				}
				if len(parts) == 5 && r.Method == http.MethodDelete {
					deletes++
					id := parts[4]
					m, ok := items[id]
					if !ok {
						fail(404)
						return
					}
					if store != "output" || m.Path != "/b.md" {
						t.Error("cleanup escaped its output stub")
						fail(403)
						return
					}
					if interfere {
						put(store, id, m.Path, "Concurrent new fact.")
						m = stores[store][id]
					}
					if r.URL.Query().Get("expected_content_sha256") != m.SHA256 {
						fail(409)
						return
					}
					delete(items, id)
					emit(map[string]any{"id": id, "deleted": true})
					return
				}
				t.Errorf("unexpected method %s %s", r.Method, r.URL)
				fail(400)
			})
			d, err := dreams.New("drm_retirement", dreams.Request{Inputs: []dreams.Input{{Type: "memory_store", MemoryStoreID: "source"}, {Type: "sessions", SessionIDs: []string{"sesn_input"}}}, Model: dreams.Model{ID: "test"}}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			engine := dreams.Engine{Backend: c, Model: c, Limits: dreams.DefaultLimits()}
			err = engine.Run(context.Background(), &d)
			mu.Lock()
			defer mu.Unlock()
			if deletes != 1 {
				t.Fatalf("cleanup calls=%d", deletes)
			}
			if *stores["source"]["mem_a"].Content != "First fact." || *stores["source"]["mem_b"].Content != "Second fact." {
				t.Fatal("source changed")
			}
			if d.Outputs[0].MemoryStoreID != "output" {
				t.Fatal("lost output reference")
			}
			foundStub := false
			for _, m := range stores["output"] {
				if m.Path == "/b.md" {
					foundStub = true
					if interfere && *m.Content != "Concurrent new fact." {
						t.Fatal("replacement lost")
					}
				}
				if m.Path == "/a.md" && *m.Content != merge {
					t.Fatal("merged destination changed")
				}
			}
			if interfere {
				if err == nil || d.Status != dreams.Failed || !foundStub {
					t.Fatalf("unsafe conflict result %+v %v", d, err)
				}
			} else if err != nil || d.Status != dreams.Completed || foundStub {
				t.Fatalf("unexpected completion %+v %v", d, err)
			}
		})
	}
}
