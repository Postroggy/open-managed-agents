package db

import (
	"context"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PlatformAuthUserContext struct {
	UserExternalID string
	OrgUUID        string
}

type PlatformAuthOrganizationInput struct {
	ExternalID string
	Name       string
}

type PlatformAuthOrganizationRef struct {
	ID   int64
	UUID string
}

type PlatformAuthUserInput struct {
	UUID           string
	ExternalID     string
	OrganizationID int64
	Email          string
	Name           string
	Role           string
}

type PlatformAuthUserRef struct {
	ID int64
}

type PlatformAuthWorkspaceInput struct {
	UUID           string
	ExternalID     string
	OrganizationID int64
	Name           string
	CompartmentID  string
}

type PlatformAuthWorkspaceRef struct {
	ID int64
}

type PlatformAuthWorkspaceMemberInput struct {
	ExternalID          string
	OrganizationID      int64
	WorkspaceID         int64
	WorkspaceExternalID string
	UserID              int64
	UserExternalID      string
	WorkspaceRole       string
}

type PlatformAuthAPIKeyInput struct {
	ExternalID      string
	WorkspaceID     int64
	KeyHash         string
	Status          string
	CreatedByUserID int64
	Name            string
	PartialKeyHint  string
}

type PlatformAuthTx struct {
	tx pgx.Tx
}

type PlatformAuthTxStore interface {
	FindUserContextByEmail(ctx context.Context, email string) (PlatformAuthUserContext, error)
	UpdateEmptyUserName(ctx context.Context, userExternalID string, defaultName string) error
	InsertOrganization(ctx context.Context, input PlatformAuthOrganizationInput) (PlatformAuthOrganizationRef, error)
	InsertUser(ctx context.Context, input PlatformAuthUserInput) (PlatformAuthUserRef, error)
	InsertWorkspace(ctx context.Context, input PlatformAuthWorkspaceInput) (PlatformAuthWorkspaceRef, error)
	InsertWorkspaceMember(ctx context.Context, input PlatformAuthWorkspaceMemberInput) error
	InsertAPIKey(ctx context.Context, input PlatformAuthAPIKeyInput) error
}

func (d *DB) WithPlatformAuthTx(ctx context.Context, fn func(PlatformAuthTxStore) error) error {
	if d == nil || d.Pool == nil {
		return ErrNotFound
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(PlatformAuthTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (tx PlatformAuthTx) FindUserContextByEmail(ctx context.Context, email string) (PlatformAuthUserContext, error) {
	var out PlatformAuthUserContext
	if strings.TrimSpace(email) == "" {
		return out, ErrNotFound
	}
	err := tx.tx.QueryRow(ctx, `
		select u.external_id, o.uuid::text
		from users u
		join organizations o on o.id = u.organization_id
		where lower(u.email) = lower($1)
		  and u.deleted_at is null
		  and exists (
			select 1
			from workspace_members wm
			where wm.organization_id = o.id
			  and wm.user_id = u.id
			  and wm.deleted_at is null
		)
		order by u.added_at asc, u.id asc
		limit 1
	`, strings.TrimSpace(email)).Scan(&out.UserExternalID, &out.OrgUUID)
	if err != nil {
		return PlatformAuthUserContext{}, mapNoRows(err)
	}
	return out, nil
}

func (tx PlatformAuthTx) UpdateEmptyUserName(ctx context.Context, userExternalID string, defaultName string) error {
	_, err := tx.tx.Exec(ctx, `
			update users
			set name = $2,
				updated_at = now()
			where external_id = $1
			  and name = ''
		`, strings.TrimSpace(userExternalID), strings.TrimSpace(defaultName))
	return err
}

func (tx PlatformAuthTx) InsertOrganization(ctx context.Context, input PlatformAuthOrganizationInput) (PlatformAuthOrganizationRef, error) {
	var out PlatformAuthOrganizationRef
	if err := tx.tx.QueryRow(ctx, `
		insert into organizations (external_id, name)
		values ($1, $2)
		returning id, uuid::text
	`, input.ExternalID, input.Name).Scan(&out.ID, &out.UUID); err != nil {
		return PlatformAuthOrganizationRef{}, err
	}
	return out, nil
}

func (tx PlatformAuthTx) InsertUser(ctx context.Context, input PlatformAuthUserInput) (PlatformAuthUserRef, error) {
	var out PlatformAuthUserRef
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "admin"
	}
	if err := tx.tx.QueryRow(ctx, `
		insert into users (uuid, external_id, organization_id, email, name, role)
		values ($1, $2, $3, $4, $5, $6)
		returning id
	`, input.UUID, input.ExternalID, input.OrganizationID, input.Email, input.Name, role).Scan(&out.ID); err != nil {
		return PlatformAuthUserRef{}, err
	}
	return out, nil
}

func (tx PlatformAuthTx) InsertWorkspace(ctx context.Context, input PlatformAuthWorkspaceInput) (PlatformAuthWorkspaceRef, error) {
	var out PlatformAuthWorkspaceRef
	if err := tx.tx.QueryRow(ctx, `
		insert into workspaces (uuid, external_id, organization_id, name, compartment_id)
		values ($1, $2, $3, $4, $5)
		returning id
	`, input.UUID, input.ExternalID, input.OrganizationID, input.Name, input.CompartmentID).Scan(&out.ID); err != nil {
		return PlatformAuthWorkspaceRef{}, err
	}
	return out, nil
}

func (tx PlatformAuthTx) InsertWorkspaceMember(ctx context.Context, input PlatformAuthWorkspaceMemberInput) error {
	workspaceRole := strings.TrimSpace(input.WorkspaceRole)
	if workspaceRole == "" {
		workspaceRole = "workspace_admin"
	}
	_, err := tx.tx.Exec(ctx, `
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role
		)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, input.ExternalID, input.OrganizationID, input.WorkspaceID, input.WorkspaceExternalID, input.UserID, input.UserExternalID, workspaceRole)
	return err
}

func (tx PlatformAuthTx) InsertAPIKey(ctx context.Context, input PlatformAuthAPIKeyInput) error {
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "default"
	}
	_, err := tx.tx.Exec(ctx, `
		insert into api_keys (external_id, workspace_id, key_hash, status, created_by_user_id, name, partial_key_hint)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, input.ExternalID, input.WorkspaceID, input.KeyHash, status, input.CreatedByUserID, name, input.PartialKeyHint)
	return err
}

func (d *DB) ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error) {
	if d == nil || d.Pool == nil {
		return platformsession.Session{}, ErrNotFound
	}
	if strings.TrimSpace(input.SessionKey) == "" || strings.TrimSpace(input.UserUUID) == "" || strings.TrimSpace(input.OrgUUID) == "" {
		return platformsession.Session{}, ErrNotFound
	}

	var session platformsession.Session
	if err := d.Pool.QueryRow(ctx, `
			select o.id, o.uuid::text, o.external_id,
				w.id, w.uuid::text, w.external_id,
				u.id, u.external_id,
				ak.id, ak.external_id
			from organizations o
			join users u on u.organization_id = o.id
			join lateral (
				select id, uuid, external_id
				from workspaces
				where organization_id = o.id
				  and archived_at is null
				order by case when external_id = 'workspace_default' then 0 else 1 end, created_at asc, id asc
				limit 1
			) w on true
			join lateral (
				select id, external_id
				from api_keys
				where workspace_id = w.id
				  and status = 'active'
				  and (expires_at is null or expires_at > now())
				order by case when external_id = 'api_key_default' then 0 else 1 end, created_at asc, id asc
				limit 1
		) ak on true
		where (o.uuid::text = $1 or o.external_id = $1)
		  and u.deleted_at is null
			  and (
				u.external_id = $2
				or u.uuid::text = $2
				or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $2
			  )
			limit 1
		`, strings.TrimSpace(input.OrgUUID), strings.TrimSpace(input.UserUUID)).Scan(
		&session.OrganizationID, &session.OrganizationUUID, &session.OrganizationExternalID,
		&session.WorkspaceID, &session.WorkspaceUUID, &session.WorkspaceExternalID,
		&session.UserID, &session.UserExternalID,
		&session.APIKeyID, &session.APIKeyExternalID,
	); err != nil {
		return platformsession.Session{}, mapNoRows(err)
	}
	sessionUUID := uuid.NewString()
	session.ExternalID = "platform_session_" + strings.ReplaceAll(sessionUUID, "-", "")
	session.ExpiresAt = input.ExpiresAt
	return session, nil
}
