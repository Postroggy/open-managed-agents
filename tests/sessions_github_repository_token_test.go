package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSessionCreateGitHubRepositoryAuthorizationToken(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("github-token-bucket"))
	defer app.close()
	suffix := uuid.NewString()[:8]
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"github-token-agent-`+suffix+`"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"github-token-env-`+suffix+`"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)

	t.Run("failure empty authorization_token rejected", func(t *testing.T) {
		body := `{"agent":` + quoteJSON(agent.ID) + `,"environment_id":` + quoteJSON(env.ID) + `,"resources":[{"type":"github_repository","url":"https://github.com/example/repo","authorization_token":""}]}`
		resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(body), defaultTestKey, true)
		defer resp.Body.Close()
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success token persisted into secret_payload and not echoed", func(t *testing.T) {
		created := createSession(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"resources":[
				{"type":"github_repository","url":"https://github.com/example/private-repo","authorization_token":"ghp_secret_token_123"}
			]
		}`)
		if len(created.Resources) != 1 {
			t.Fatalf("created resources len = %d, want 1", len(created.Resources))
		}
		if bytes.Contains(created.Resources[0], []byte("ghp_secret_token_123")) {
			t.Fatalf("response resource leaked authorization_token: %s", created.Resources[0])
		}
		var secretPayload []byte
		err := app.db.Pool.QueryRow(context.Background(),
			`select secret_payload from session_resources where session_external_id = $1 and resource_type = 'github_repository'`,
			created.ID).Scan(&secretPayload)
		if err != nil {
			t.Fatalf("query secret_payload: %v", err)
		}
		var secret map[string]string
		if err := json.Unmarshal(secretPayload, &secret); err != nil {
			t.Fatalf("unmarshal secret_payload %s: %v", secretPayload, err)
		}
		if secret["authorization_token"] != "ghp_secret_token_123" {
			t.Fatalf("secret authorization_token = %q, want ghp_secret_token_123", secret["authorization_token"])
		}
	})

	t.Run("success public repo without token leaves secret_payload empty", func(t *testing.T) {
		created := createSession(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"resources":[
				{"type":"github_repository","url":"https://github.com/example/public-repo"}
			]
		}`)
		if len(created.Resources) != 1 {
			t.Fatalf("created resources len = %d, want 1", len(created.Resources))
		}
		var secretPayload []byte
		err := app.db.Pool.QueryRow(context.Background(),
			`select secret_payload from session_resources where session_external_id = $1 and resource_type = 'github_repository'`,
			created.ID).Scan(&secretPayload)
		if err != nil {
			t.Fatalf("query secret_payload: %v", err)
		}
		if len(secretPayload) != 0 {
			t.Fatalf("secret_payload = %s, want empty for public repo", secretPayload)
		}
	})
}
