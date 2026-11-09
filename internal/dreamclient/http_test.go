package dreamclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Options{BaseURL: server.URL, APIKey: "test-only-key", WorkspaceID: "wrkspc_test", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientRequiresExplicitTransportAndCredentials(t *testing.T) {
	for _, options := range []Options{{BaseURL: "http://localhost:38080", APIKey: "key"}, {BaseURL: "https://user:secret@example.com", APIKey: "key"}, {BaseURL: "https://example.com?token=secret", APIKey: "key"}, {BaseURL: "https://example.com#x", APIKey: "key"}, {BaseURL: "https://example.com"}, {BaseURL: "file:///tmp", APIKey: "key"}, {BaseURL: "https://example.com", APIKey: "key", MaxResponseBytes: -1}} {
		if _, err := New(options); err == nil {
			t.Fatalf("accepted unsafe options %+v", options)
		}
	}
}

func TestClientForwardsScopeAndDoesNotRetryModelWrites(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Api-Key") != "test-only-key" || r.Header.Get("anthropic-workspace-id") != "wrkspc_test" {
			t.Error("missing credential scope")
		}
		if r.URL.Query().Has("beta") {
			t.Error("messages sent with unrelated beta query")
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"SECRET_DO_NOT_LOG"}}`))
	})
	_, err := client.Complete(context.Background(), dreams.ModelRequest{Model: "test", MaxTokens: 10})
	if err == nil || calls != 1 || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe retry or error %v, calls=%d", err, calls)
	}
}

func TestClientRejectsRedirectsWithoutSendingSecrets(t *testing.T) {
	received := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true; w.WriteHeader(200) }))
	defer target.Close()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if err := client.request(context.Background(), http.MethodGet, "/v1/memory_stores", memoryBeta, nil, nil, new(any)); err == nil || received {
		t.Fatal("followed authenticated redirect")
	}
}

func TestClientRejectsOversizedMalformedAndRepeatedPages(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 100))) })
		client.maxResponseBytes = 10
		if err := client.request(context.Background(), http.MethodGet, "/v1/x", "", nil, nil, new(any)); err == nil {
			t.Fatal("accepted oversized response")
		}
	})
	t.Run("malformed", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`bad json`)) })
		if err := client.request(context.Background(), http.MethodGet, "/v1/x", "", nil, nil, new(any)); err == nil {
			t.Fatal("accepted malformed response")
		}
	})
	t.Run("repeated cursor", func(t *testing.T) {
		calls := 0
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			_, _ = w.Write([]byte(`{"data":[],"next_page":"repeat"}`))
		})
		err := pages[int](context.Background(), client, "/v1/x", memoryBeta, nil, 10, func(int) error { return nil })
		if err == nil || calls != 2 {
			t.Fatalf("cursor cycle not rejected: %v %d", err, calls)
		}
	})
	t.Run("item limit", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[1,2],"next_page":null}`))
		})
		if err := pages[int](context.Background(), client, "/v1/x", memoryBeta, url.Values{}, 1, func(int) error { return nil }); err == nil {
			t.Fatal("ignored item budget")
		}
	})
}

func TestClientPropagatesCancellationAndRejectsPathIDs(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request sent after cancellation") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Complete(ctx, dreams.ModelRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, id := range []string{"", "../x", "a/b", "a?x=y", "a%2fb", "a\n"} {
		if _, err := resourcePath("sessions", id); err == nil {
			t.Fatalf("unsafe ID accepted: %q", id)
		}
	}
}
