package dreamclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base               *url.URL
	apiKey             string
	workspaceID        string
	http               *http.Client
	maxResponseBytes   int64
	maxTranscriptBytes int
}

type Options struct {
	BaseURL            string
	APIKey             string
	WorkspaceID        string
	HTTPClient         *http.Client
	MaxResponseBytes   int64
	MaxTranscriptBytes int
	AllowInsecure      bool
}

func New(options Options) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(options.BaseURL, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("invalid OMA base URL")
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && options.AllowInsecure) {
		return nil, errors.New("HTTPS is required; allow insecure HTTP explicitly for local OMA development")
	}
	if options.APIKey == "" {
		return nil, errors.New("OMA API key is required")
	}
	if options.MaxResponseBytes == 0 {
		options.MaxResponseBytes = 16 << 20
	}
	if options.MaxTranscriptBytes == 0 {
		options.MaxTranscriptBytes = 32 << 20
	}
	if options.MaxResponseBytes < 1 || options.MaxTranscriptBytes < 1 {
		return nil, errors.New("response limits must be positive")
	}
	var client http.Client
	if options.HTTPClient != nil {
		client = *options.HTTPClient
	} else {
		client = http.Client{Timeout: 5 * time.Minute}
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("redirects are not allowed for authenticated OMA requests")
	}
	return &Client{base: base, apiKey: options.APIKey, workspaceID: options.WorkspaceID, http: &client, maxResponseBytes: options.MaxResponseBytes, maxTranscriptBytes: options.MaxTranscriptBytes}, nil
}

type APIError struct {
	Status int
	Type   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("OMA API request failed (HTTP %d, %s)", e.Status, e.Type)
}
func (c *Client) request(ctx context.Context, method, endpoint, beta string, query url.Values, body, result any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	target := *c.base
	target.Path = strings.TrimRight(target.Path, "/") + endpoint
	target.RawPath = ""
	if query == nil {
		query = url.Values{}
	}
	if beta != "" {
		query.Set("beta", "true")
	}
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("X-Api-Key", c.apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")
	if beta != "" {
		request.Header.Set("anthropic-beta", beta)
	}
	if c.workspaceID != "" {
		request.Header.Set("anthropic-workspace-id", c.workspaceID)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("OMA transport error: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > c.maxResponseBytes {
		return errors.New("OMA response exceeds the configured byte limit")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		var envelope struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &envelope)
		kind := envelope.Error.Type
		if !safeID(kind) {
			kind = "upstream_error"
		}
		return &APIError{Status: response.StatusCode, Type: kind}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.New("OMA returned an invalid JSON response")
	}
	return nil
}

func safeID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func resourcePath(resource, id string) (string, error) {
	if !safeID(id) {
		return "", errors.New("invalid resource ID")
	}
	return "/v1/" + resource + "/" + id, nil
}

type page[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

func pages[T any](ctx context.Context, c *Client, endpoint, beta string, query url.Values, maxItems int, visit func(T) error) error {
	if query == nil {
		query = url.Values{}
	}
	query.Set("limit", "100")
	seen := map[string]bool{}
	count := 0
	for {
		var result page[T]
		if err := c.request(ctx, http.MethodGet, endpoint, beta, query, nil, &result); err != nil {
			return err
		}
		if result.Data == nil {
			return errors.New("OMA omitted the pagination data array")
		}
		for _, item := range result.Data {
			count++
			if count > maxItems {
				return errors.New("OMA pagination exceeds configured item limit")
			}
			if err := visit(item); err != nil {
				return err
			}
		}
		if result.NextPage == nil || *result.NextPage == "" {
			return nil
		}
		if seen[*result.NextPage] {
			return errors.New("OMA returned a repeating pagination cursor")
		}
		seen[*result.NextPage] = true
		if len(seen) > 10000 {
			return errors.New("OMA pagination exceeds page limit")
		}
		query.Set("page", *result.NextPage)
	}
}
