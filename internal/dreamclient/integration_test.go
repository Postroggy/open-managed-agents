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

func TestEngineWithOMAHTTPAdapter(t *testing.T) {
	var mu sync.Mutex
	old := "Entrance is east."
	stores := map[string]map[string]memoryResponse{"memstore_source": {"mem_note": {ID: "mem_note", Path: "/note.md", Content: &old, SHA256: hashText(old)}}}
	modelCalls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		emit := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/v1/memory_stores":
			stores["memstore_output"] = map[string]memoryResponse{}
			emit(storeResponse{ID: "memstore_output", Name: "output"})
			return
		case "/v1/sessions/sesn_one":
			emit(sessionResponse{ID: "sesn_one", Status: "idle", UpdatedAt: "fixed"})
			return
		case "/v1/sessions/sesn_one/threads":
			emit(page[threadResponse]{Data: []threadResponse{{ID: "sthr_main"}}})
			return
		case "/v1/sessions/sesn_one/threads/sthr_main/events":
			emit(map[string]any{"data": []any{map[string]any{"id": "event_one", "type": "agent.tool_result", "content": "Verified entrance is west"}}, "next_page": nil})
			return
		case "/v1/messages":
			modelCalls++
			var request dreams.ModelRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			turn := (len(request.Messages) - 1) / 2
			call := func(name string, args any) {
				raw, _ := json.Marshal(args)
				emit(dreams.ModelResponse{StopReason: "tool_use", Content: []dreams.Block{{Type: "tool_use", ID: "call", Name: name, Input: raw}}, Usage: dreams.Usage{InputTokens: 10, OutputTokens: 2}})
			}
			if strings.Contains(request.Messages[0].Content[0].Text, "analyze") {
				switch turn {
				case 0:
					call("read", map[string]any{"target": "source", "session_id": "sesn_one"})
					return
				case 1:
					call("read", map[string]any{"target": "memory", "path": "/note.md"})
					return
				case 2:
					var data struct {
						SHA256 string `json:"sha256"`
					}
					_ = json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content[0].Content), &data)
					call("write", map[string]any{"path": "/note.md", "content": "Verified west; source sesn_one/event_one", "expected_sha256": data.SHA256})
					return
				}
			} else if turn == 0 {
				call("write", map[string]any{"path": "/_index.md", "content": "[Entrance](note.md)"})
				return
			}
			emit(dreams.ModelResponse{StopReason: "end_turn", Content: []dreams.Block{{Type: "text", Text: "done"}}, Usage: dreams.Usage{InputTokens: 5, OutputTokens: 1}})
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[1] == "memory_stores" {
			id := parts[2]
			items, ok := stores[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			if len(parts) == 3 {
				emit(storeResponse{ID: id, Name: id})
				return
			}
			if len(parts) == 4 && r.Method == http.MethodGet {
				all := []memoryResponse{}
				for _, item := range items {
					item.Content = nil
					all = append(all, item)
				}
				emit(page[memoryResponse]{Data: all})
				return
			}
			if len(parts) == 5 && r.Method == http.MethodGet {
				item, ok := items[parts[4]]
				if !ok {
					w.WriteHeader(404)
					return
				}
				emit(item)
				return
			}
			if r.Method == http.MethodPost {
				var body memoryWrite
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				itemID := "mem_" + hashText(body.Path)[:12]
				if len(parts) == 5 {
					itemID = parts[4]
					current := items[itemID]
					if body.Precondition == nil || body.Precondition.SHA256 != current.SHA256 {
						w.WriteHeader(409)
						return
					}
				}
				item := memoryResponse{ID: itemID, Path: body.Path, Content: &body.Content, SHA256: hashText(body.Content)}
				items[itemID] = item
				emit(item)
				return
			}
		}
		t.Errorf("unexpected endpoint %s %s", r.Method, r.URL)
		w.WriteHeader(404)
	})
	request := dreams.Request{Inputs: []dreams.Input{{Type: "memory_store", MemoryStoreID: "memstore_source"}, {Type: "sessions", SessionIDs: []string{"sesn_one"}}}, Model: dreams.Model{ID: "provider/model"}}
	d, err := dreams.New("drm_integration", request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	e := dreams.Engine{Backend: c, Model: c, Limits: dreams.DefaultLimits()}
	if err := e.Run(context.Background(), &d); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if d.Status != dreams.Completed || d.Outputs[0].MemoryStoreID != "memstore_output" || modelCalls != 6 {
		t.Fatalf("unexpected run: %+v, calls=%d", d, modelCalls)
	}
	if *stores["memstore_source"]["mem_note"].Content != old {
		t.Fatal("source store was modified")
	}
	found := false
	for _, memory := range stores["memstore_output"] {
		if memory.Path == "/note.md" && strings.Contains(*memory.Content, "Verified west") {
			found = true
		}
	}
	if !found {
		t.Fatal("engine changes did not persist through OMA HTTP API")
	}
}
