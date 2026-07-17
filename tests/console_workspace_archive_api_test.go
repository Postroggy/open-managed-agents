package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestConsoleWorkspaceArchive(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("console-workspace-archive-bucket"))
	defer app.close()
	cookies := app.platformLoginCookies(t, "console-workspace-archive@example.com")

	var orgUUID string
	var orgID int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select uuid::text, id from organizations where external_id = 'org_default'
	`).Scan(&orgUUID, &orgID); err != nil {
		t.Fatalf("load default organization: %v", err)
	}
	base := "/api/console/organizations/" + orgUUID

	t.Run("default workspace cannot be archived", func(t *testing.T) {
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/default/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("unknown workspace is not found", func(t *testing.T) {
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/ws_archive_missing/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	targetWS := seedArchiveTargetWorkspace(t, app, orgID, "Archive Target")
	keyID := seedConsoleAPIKeyForWorkspace(t, app, orgUUID, targetWS, "target key")

	t.Run("archive succeeds and cascades to api keys", func(t *testing.T) {
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+targetWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var archived map[string]any
		decodeJSON(t, resp.Body, &archived)
		if archived["id"] != targetWS || archived["archived_at"] == nil {
			t.Fatalf("archive response mismatch: %#v", archived)
		}
		var keyArchivedAt *time.Time
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at from console_api_keys where external_id = $1
		`, keyID).Scan(&keyArchivedAt); err != nil {
			t.Fatalf("load cascaded api key: %v", err)
		}
		if keyArchivedAt == nil {
			t.Fatalf("api key %q was not cascaded to archived", keyID)
		}
	})

	t.Run("archive is idempotent", func(t *testing.T) {
		var firstArchivedAt time.Time
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at from workspaces where external_id = $1
		`, targetWS).Scan(&firstArchivedAt); err != nil {
			t.Fatalf("load workspace archived_at: %v", err)
		}
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+targetWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("repeat status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var secondArchivedAt time.Time
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at from workspaces where external_id = $1
		`, targetWS).Scan(&secondArchivedAt); err != nil {
			t.Fatalf("reload workspace archived_at: %v", err)
		}
		if !secondArchivedAt.Equal(firstArchivedAt) {
			t.Fatalf("archived_at changed on repeat archive: first=%s second=%s", firstArchivedAt, secondArchivedAt)
		}
	})

	t.Run("isolated by organization", func(t *testing.T) {
		otherOrgID := seedArchiveOrganization(t, app, "org_archive_isolation_"+uniqueAdminSuffix())
		otherWS := seedArchiveTargetWorkspace(t, app, otherOrgID, "Other Org WS")
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+otherWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (org isolation): %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})
}

func seedArchiveOrganization(t *testing.T, app *testApp, externalID string) int64 {
	t.Helper()
	var id int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		insert into organizations (external_id, name)
		values ($1, $1)
		on conflict (external_id) do update set name = excluded.name
		returning id
	`, externalID).Scan(&id); err != nil {
		t.Fatalf("seed archive organization: %v", err)
	}
	return id
}

func seedArchiveTargetWorkspace(t *testing.T, app *testApp, orgID int64, name string) string {
	t.Helper()
	suffix := uniqueAdminSuffix()
	externalID := "ws_archive_" + suffix
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into workspaces (external_id, organization_id, name)
		values ($1, $2, $3)
	`, externalID, orgID, name+" "+suffix); err != nil {
		t.Fatalf("seed archive target workspace: %v", err)
	}
	return externalID
}

func seedConsoleAPIKeyForWorkspace(t *testing.T, app *testApp, orgUUID, workspaceExternalID, name string) string {
	t.Helper()
	externalID := "cak_archive_" + uniqueAdminSuffix()
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into console_api_keys (external_id, api_key_uuid, org_uuid, workspace_id, name, key_prefix, key_suffix, key_hash, status)
		values ($1, $2, $3, $4, $5, $6, $7, $8, 'active')
	`, externalID, "akuuid_"+externalID, orgUUID, workspaceExternalID, name, "sk-ant-", "ARCH",
		auth.HashAPIKey("secret-"+externalID)); err != nil {
		t.Fatalf("seed console api key for workspace: %v", err)
	}
	return externalID
}
