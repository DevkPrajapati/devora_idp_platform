package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/idp/platform/backend/internal/database"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// RegistryCredentialRow is a stored credential. PasswordEncrypted is an
// AES-256-GCM envelope; nothing in this layer decrypts it.
type RegistryCredentialRow = db.RegistryCredential

// ErrCredentialNotFound reports a missing credential distinctly from a
// database failure, so the service can map it to NotFound rather than Internal.
var ErrCredentialNotFound = errors.New("registry credential not found")

// RegistryRepository persists per-project registry credentials.
type RegistryRepository struct {
	queries *db.Queries
}

// NewRegistryRepository creates a registry credential repository.
func NewRegistryRepository(pool *database.Pool) *RegistryRepository {
	return &RegistryRepository{queries: db.New(pool)}
}

// UpsertRegistryCredentialInput holds credential write parameters.
type UpsertRegistryCredentialInput struct {
	ProjectID pgtype.UUID
	Name      string
	// RegistryURL is stored as the user entered it; normalisation to a Docker
	// auth key happens when the Secret is rendered, so the UI can echo back
	// what was typed.
	RegistryURL       string
	Username          string
	PasswordEncrypted []byte
	Email             string
	CreatedBy         string
}

// Upsert stores a credential, replacing any existing one with the same name
// inside the project.
func (r *RegistryRepository) Upsert(ctx context.Context, in UpsertRegistryCredentialInput) (*RegistryCredentialRow, error) {
	if len(in.PasswordEncrypted) == 0 {
		// A zero-length envelope would mean an unencrypted or lost password
		// silently reaching the database.
		return nil, fmt.Errorf("encrypted password is required")
	}

	row, err := r.queries.UpsertRegistryCredential(ctx, db.UpsertRegistryCredentialParams{
		ProjectID:         in.ProjectID,
		Name:              in.Name,
		RegistryUrl:       in.RegistryURL,
		Username:          in.Username,
		PasswordEncrypted: in.PasswordEncrypted,
		Email:             optionalString(in.Email),
		CreatedBy:         in.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert registry credential: %w", err)
	}
	return &row, nil
}

// Get returns a single credential.
func (r *RegistryRepository) Get(ctx context.Context, projectID pgtype.UUID, name string) (*RegistryCredentialRow, error) {
	row, err := r.queries.GetRegistryCredential(ctx, db.GetRegistryCredentialParams{
		ProjectID: projectID,
		Name:      name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCredentialNotFound
		}
		return nil, fmt.Errorf("get registry credential: %w", err)
	}
	return &row, nil
}

// List returns every credential belonging to a project, ordered by name.
func (r *RegistryRepository) List(ctx context.Context, projectID pgtype.UUID) ([]RegistryCredentialRow, error) {
	rows, err := r.queries.ListRegistryCredentials(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list registry credentials: %w", err)
	}
	return rows, nil
}

// Delete removes a credential. Deleting a non-existent credential is a no-op.
func (r *RegistryRepository) Delete(ctx context.Context, projectID pgtype.UUID, name string) error {
	err := r.queries.DeleteRegistryCredential(ctx, db.DeleteRegistryCredentialParams{
		ProjectID: projectID,
		Name:      name,
	})
	if err != nil {
		return fmt.Errorf("delete registry credential: %w", err)
	}
	return nil
}
