//go:build e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/google/uuid"
	e2b "github.com/superduck-ai/e2b-go-sdk"
)

// TestUnrestrictedNetwork — unrestricted 环境应允许访问任何外网地址。
func TestUnrestrictedNetwork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg, database, apiKey, template, cleanup := setupE2BNetworkingTest(t, ctx)
	defer cleanup()

	envConfig := mustJSON(t, map[string]any{
		"type":       "cloud",
		"runtime":    "self_hosted",
		"image":      template,
		"packages":   []any{},
		"networking": map[string]any{"type": "unrestricted"},
	})
	_, _, provider, sandboxID := createSandboxForNetworkTest(t, ctx, database, apiKey, template, envConfig, cfg)
	defer killE2BSandbox(t, ctx, provider, sandboxID)

	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err != nil {
		t.Fatalf("connect sandbox: %v", err)
	}

	assertCurlOK(t, sandbox, ctx, "https://example.com", "unrestricted")
	assertCurlOK(t, sandbox, ctx, "https://google.com", "unrestricted")
}

// TestLimitedNetworkBlocksAll — limited 且无 allowed_hosts 时，所有外网访问应被阻断。
func TestLimitedNetworkBlocksAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg, database, apiKey, template, cleanup := setupE2BNetworkingTest(t, ctx)
	defer cleanup()

	envConfig := mustJSON(t, map[string]any{
		"type":    "cloud",
		"runtime": "self_hosted",
		"image":   template,
		"packages": []any{},
		"networking": map[string]any{
			"type":                   "limited",
			"allowed_hosts":          []string{},
			"allow_mcp_servers":      false,
			"allow_package_managers": false,
		},
	})
	_, _, provider, sandboxID := createSandboxForNetworkTest(t, ctx, database, apiKey, template, envConfig, cfg)
	defer killE2BSandbox(t, ctx, provider, sandboxID)

	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err != nil {
		t.Fatalf("connect sandbox: %v", err)
	}

	assertCurlFails(t, sandbox, ctx, "https://example.com", "limited (empty allowlist)")
	assertCurlFails(t, sandbox, ctx, "https://google.com", "limited (empty allowlist)")
}

// TestLimitedNetworkWithAllowedHosts — limited 且 allowed_hosts 包含特定域名时，
// 应允许访问该域名，但阻断其他外网域名。
func TestLimitedNetworkWithAllowedHosts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg, database, apiKey, template, cleanup := setupE2BNetworkingTest(t, ctx)
	defer cleanup()

	envConfig := mustJSON(t, map[string]any{
		"type":    "cloud",
		"runtime": "self_hosted",
		"image":   template,
		"packages": []any{},
		"networking": map[string]any{
			"type":                   "limited",
			"allowed_hosts":          []string{"example.com"},
			"allow_mcp_servers":      false,
			"allow_package_managers": false,
		},
	})
	_, _, provider, sandboxID := createSandboxForNetworkTest(t, ctx, database, apiKey, template, envConfig, cfg)
	defer killE2BSandbox(t, ctx, provider, sandboxID)

	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err != nil {
		t.Fatalf("connect sandbox: %v", err)
	}

	assertCurlOK(t, sandbox, ctx, "https://example.com", "allowed host")
	assertCurlFails(t, sandbox, ctx, "https://google.com", "disallowed host")
}

// TestLimitedNetworkAllowsPackageManagers — limited 且 allow_package_managers=true 时，
// 包管理器域名应可访问。
func TestLimitedNetworkAllowsPackageManagers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg, database, apiKey, template, cleanup := setupE2BNetworkingTest(t, ctx)
	defer cleanup()

	envConfig := mustJSON(t, map[string]any{
		"type":    "cloud",
		"runtime": "self_hosted",
		"image":   template,
		"packages": []any{},
		"networking": map[string]any{
			"type":                   "limited",
			"allowed_hosts":          []string{},
			"allow_mcp_servers":      false,
			"allow_package_managers": true,
		},
	})
	_, _, provider, sandboxID := createSandboxForNetworkTest(t, ctx, database, apiKey, template, envConfig, cfg)
	defer killE2BSandbox(t, ctx, provider, sandboxID)

	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err != nil {
		t.Fatalf("connect sandbox: %v", err)
	}

	assertCurlOK(t, sandbox, ctx, "https://pypi.org", "package manager")
	assertCurlFails(t, sandbox, ctx, "https://google.com", "package manager (disallowed)")
}

// ---------------------------------------------------------------------------
// 测试辅助函数
// ---------------------------------------------------------------------------

func setupE2BNetworkingTest(t *testing.T, ctx context.Context) (
	cfg config.Config, database *db.DB, apiKey db.APIKey,
	template string, cleanup func(),
) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(cfg.E2BAPIKey) == "" {
		t.Fatal("E2B_API_KEY is required for the real E2B integration test")
	}
	if cfg.E2BRequestTimeout < 2*time.Minute {
		cfg.E2BRequestTimeout = 2 * time.Minute
	}
	if cfg.E2BSandboxTimeout < time.Minute {
		cfg.E2BSandboxTimeout = time.Minute
	}
	cfg.E2BDebug = false

	database, err = db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := database.Seed(ctx, cfg.SeedAPIKeys); err != nil {
		t.Fatalf("seed database: %v", err)
	}

	apiKey, err = database.GetAPIKey(ctx, auth.HashAPIKey(config.DefaultAPIKey))
	if err != nil {
		t.Fatalf("load default api key: %v", err)
	}

	template = strings.TrimSpace(cfg.E2BTemplate)
	if template == "" {
		template = "claude-code-interpreter"
	}

	cleanup = func() { database.Close() }
	return
}

func createSandboxForNetworkTest(
	t *testing.T, ctx context.Context, database *db.DB,
	apiKey db.APIKey, template string, envConfig json.RawMessage,
	cfg config.Config,
) (envExternalID, workExternalID string, provider *e2bruntime.E2BProvider, sandboxID string) {
	t.Helper()

	envID, err := ids.New("env_")
	if err != nil {
		t.Fatalf("create environment id: %v", err)
	}
	workID, err := ids.New("work_")
	if err != nil {
		t.Fatalf("create work id: %v", err)
	}

	now := time.Now().UTC()
	env, err := database.CreateEnvironment(ctx, db.Environment{
		UUID:              uuid.NewString(),
		ExternalID:        envID,
		OrganizationID:    apiKey.OrganizationID,
		WorkspaceID:       apiKey.WorkspaceID,
		CreatedByAPIKeyID: apiKey.ID,
		Name:              "networking-test-" + envID[len("env_"):len("env_")+8],
		Description:       "E2B networking integration test",
		Config:            envConfig,
		Metadata:          mustJSON(t, map[string]any{"source": "e2b_networking_test"}),
		Provider:          "e2b",
		ResolvedTemplate:  template,
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	t.Cleanup(func() {
		cpCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = database.Pool.Exec(cpCtx, `delete from environment_sandboxes where environment_external_id = $1`, env.ExternalID)
		_, _ = database.Pool.Exec(cpCtx, `delete from environment_work where environment_external_id = $1`, env.ExternalID)
		_, _ = database.Pool.Exec(cpCtx, `delete from environments where external_id = $1`, env.ExternalID)
	})

	_, err = database.CreateEnvironmentWork(ctx, db.EnvironmentWork{
		UUID:                  uuid.NewString(),
		ExternalID:            workID,
		OrganizationID:        env.OrganizationID,
		WorkspaceID:           env.WorkspaceID,
		EnvironmentID:         env.ID,
		EnvironmentExternalID: env.ExternalID,
		Data:                  mustJSON(t, map[string]any{"task": "networking e2b test"}),
		Metadata:              mustJSON(t, map[string]any{"source": "e2b_networking_test"}),
		State:                 "queued",
		CreatedAt:             now,
	})
	if err != nil {
		t.Fatalf("create environment work: %v", err)
	}

	provider = e2bruntime.NewProvider(cfg)
	runner := environments.NewRunner(database, provider)
	processed, err := runner.RunOnce(ctx, "e2b-networking-test")
	if err != nil {
		t.Fatalf("run environment runner once: %v", err)
	}
	if !processed {
		t.Fatal("environment runner did not process queued work")
	}

	sbxID, _, state, _, errMsg := loadE2BSandboxRow(t, database, env.ExternalID, workID)
	if errMsg != "" {
		t.Fatalf("sandbox row has last_error: %s", errMsg)
	}
	if state != "running" {
		t.Fatalf("sandbox state = %s, want running", state)
	}
	if strings.TrimSpace(sbxID) == "" {
		t.Fatal("provider sandbox id was not recorded")
	}

	return env.ExternalID, workID, provider, sbxID
}

func assertCurlOK(t *testing.T, sandbox *e2b.Sandbox, ctx context.Context, url, label string) {
	t.Helper()
	timeoutMs := 20_000
	execution, err := sandbox.Commands.Run(ctx, curlCommand(url), &e2b.CommandStartOpts{TimeoutMs: &timeoutMs})
	if err != nil {
		t.Fatalf("[%s] curl %s should succeed but got error: %v", label, url, err)
	}
	result, ok := execution.(*e2b.CommandResult)
	if !ok {
		t.Fatalf("[%s] curl %s unexpected result type %T", label, url, execution)
	}
	if result.ExitCode != 0 {
		t.Fatalf("[%s] curl %s exited with code %d, want 0: stdout=%q stderr=%q", label, url, result.ExitCode, result.Stdout, result.Stderr)
	}
}

func assertCurlFails(t *testing.T, sandbox *e2b.Sandbox, ctx context.Context, url, label string) {
	t.Helper()
	timeoutMs := 20_000
	execution, err := sandbox.Commands.Run(ctx, curlCommand(url), &e2b.CommandStartOpts{TimeoutMs: &timeoutMs})
	if err == nil {
		if result, ok := execution.(*e2b.CommandResult); ok && result.ExitCode == 0 {
			t.Fatalf("[%s] curl %s should fail but succeeded: stdout=%q", label, url, result.Stdout)
		}
		return
	}
}

func curlCommand(url string) string {
	return "curl -sS --connect-timeout 10 -o /dev/null -w '%{http_code}' " + url
}

func killE2BSandbox(t *testing.T, ctx context.Context, provider *e2bruntime.E2BProvider, sandboxID string) {
	t.Helper()
	killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = provider.Kill(killCtx, sandboxID)
}
