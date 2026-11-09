package dreamclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func transcriptHandler(t *testing.T, changed bool) http.HandlerFunc {
	reads := 0
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("beta") != "true" || r.Header.Get("anthropic-beta") != sessionBeta {
			t.Error("missing session beta contract")
		}
		var result any
		switch r.URL.Path {
		case "/v1/sessions/sesn_one":
			reads++
			stamp := "2026-11-09T00:00:00Z"
			if changed && reads > 1 {
				stamp = "2026-11-09T00:00:01Z"
			}
			result = map[string]any{"id": "sesn_one", "status": "idle", "updated_at": stamp}
		case "/v1/sessions/sesn_one/threads":
			if r.URL.Query().Get("include_archived") != "true" {
				t.Error("archived threads omitted")
			}
			if r.URL.Query().Get("page") == "children" {
				result = map[string]any{"data": []any{map[string]any{"id": "sthr_child", "parent_thread_id": "sthr_main"}}, "next_page": nil}
			} else {
				result = map[string]any{"data": []any{map[string]any{"id": "sthr_main", "parent_thread_id": nil}}, "next_page": "children"}
			}
		case "/v1/sessions/sesn_one/threads/sthr_main/events":
			if r.URL.Query().Get("order") != "asc" {
				t.Error("events not chronological")
			}
			if r.URL.Query().Get("page") == "more" {
				result = map[string]any{"data": []any{map[string]any{"id": "ev_2", "type": "agent.message", "content": []any{map[string]any{"type": "text", "text": "unverified guess"}}}}, "next_page": nil}
			} else {
				result = map[string]any{"data": []any{map[string]any{"id": "ev_1", "type": "user.message", "content": []any{map[string]any{"type": "text", "text": "test request"}}}}, "next_page": "more"}
			}
		case "/v1/sessions/sesn_one/threads/sthr_child/events":
			result = map[string]any{"data": []any{map[string]any{"id": "ev_child", "type": "agent.tool_result", "is_error": true, "content": "child command failed"}}, "next_page": nil}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL)
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}

func TestTranscriptExportsAllPagesAndSubagents(t *testing.T) {
	c := testClient(t, transcriptHandler(t, false))
	transcript, err := c.Transcript(context.Background(), "sesn_one")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"sthr_main", "sthr_child", "ev_1", "ev_2", "ev_child", "child command failed", `"is_error":true`, "attachment bodies are not fetched"} {
		if !strings.Contains(transcript.Text, part) {
			t.Fatalf("missing %q in transcript", part)
		}
	}
	if strings.Index(transcript.Text, "ev_1") > strings.Index(transcript.Text, "ev_2") {
		t.Fatal("event order reversed")
	}
}

func TestTranscriptRefusesChangingOrTruncatedSources(t *testing.T) {
	t.Run("changed", func(t *testing.T) {
		c := testClient(t, transcriptHandler(t, true))
		if _, err := c.Transcript(context.Background(), "sesn_one"); err == nil {
			t.Fatal("accepted changing transcript")
		}
	})
	t.Run("size", func(t *testing.T) {
		c := testClient(t, transcriptHandler(t, false))
		c.maxTranscriptBytes = 10
		if _, err := c.Transcript(context.Background(), "sesn_one"); err == nil {
			t.Fatal("silently truncated source")
		}
	})
	t.Run("active", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"id":"sesn_one","status":"running"}`))
		})
		if _, err := c.Transcript(context.Background(), "sesn_one"); err == nil {
			t.Fatal("exported running session")
		}
	})
	t.Run("malformed event", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"type":"user.message"}],"next_page":null}`))
		})
		var text strings.Builder
		if err := c.appendEvents(context.Background(), &text, "/events"); err == nil {
			t.Fatal("accepted event without identity")
		}
	})
}
