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

// ErrClusterNotFound reports a missing fleet row.
var ErrClusterNotFound = errors.New("cluster not found")

// ClusterRepository persists managed Kubernetes clusters.
type ClusterRepository struct {
	queries *db.Queries
}

// NewClusterRepository creates a cluster repository.
func NewClusterRepository(pool *database.Pool) *ClusterRepository {
	return &ClusterRepository{queries: db.New(pool)}
}

// CreateClusterInput is a new fleet row.
type CreateClusterInput struct {
	Name                string
	DisplayName         string
	Provider            string
	Status              string
	KubeconfigEncrypted []byte
	ServerURL           string
	KubernetesVersion   string
	NodeCount           int32
	Active              bool
	LastError           string
	CreatedBy           string
}

// Create inserts a cluster row.
func (r *ClusterRepository) Create(ctx context.Context, in CreateClusterInput) (*db.Cluster, error) {
	row, err := r.queries.CreateCluster(ctx, db.CreateClusterParams{
		Name:                in.Name,
		DisplayName:         in.DisplayName,
		Provider:            in.Provider,
		Status:              in.Status,
		KubeconfigEncrypted: in.KubeconfigEncrypted,
		ServerUrl:           optionalString(in.ServerURL),
		KubernetesVersion:   optionalString(in.KubernetesVersion),
		NodeCount:           in.NodeCount,
		IsActive:            in.Active,
		LastError:           optionalString(in.LastError),
		CreatedBy:           in.CreatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("create cluster: %w", err)
	}
	return &row, nil
}

// Get returns a cluster by id.
func (r *ClusterRepository) Get(ctx context.Context, id pgtype.UUID) (*db.Cluster, error) {
	row, err := r.queries.GetCluster(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClusterNotFound
		}
		return nil, fmt.Errorf("get cluster: %w", err)
	}
	return &row, nil
}

// GetByName returns a cluster by unique name.
func (r *ClusterRepository) GetByName(ctx context.Context, name string) (*db.Cluster, error) {
	row, err := r.queries.GetClusterByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClusterNotFound
		}
		return nil, fmt.Errorf("get cluster by name: %w", err)
	}
	return &row, nil
}

// GetActive returns the active cluster, or nil when none is selected.
func (r *ClusterRepository) GetActive(ctx context.Context) (*db.Cluster, error) {
	row, err := r.queries.GetActiveCluster(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active cluster: %w", err)
	}
	return &row, nil
}

// List returns every fleet cluster.
func (r *ClusterRepository) List(ctx context.Context) ([]db.Cluster, error) {
	rows, err := r.queries.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	return rows, nil
}

// Count returns how many clusters are registered.
func (r *ClusterRepository) Count(ctx context.Context) (int64, error) {
	n, err := r.queries.CountClusters(ctx)
	if err != nil {
		return 0, fmt.Errorf("count clusters: %w", err)
	}
	return n, nil
}

// SetStatus updates lifecycle status and last_error.
func (r *ClusterRepository) SetStatus(ctx context.Context, id pgtype.UUID, status, lastError string) (*db.Cluster, error) {
	row, err := r.queries.UpdateClusterStatus(ctx, db.UpdateClusterStatusParams{
		ID:        id,
		Status:    status,
		LastError: optionalString(lastError),
	})
	if err != nil {
		return nil, fmt.Errorf("update cluster status: %w", err)
	}
	return &row, nil
}

// UpdateRuntimeInput is the post-provision / post-start snapshot.
type UpdateRuntimeInput struct {
	ID                  pgtype.UUID
	KubeconfigEncrypted []byte
	ServerURL           string
	KubernetesVersion   string
	NodeCount           int32
	Status              string
	LastError           string
}

// UpdateRuntime stores kubeconfig and live metadata.
func (r *ClusterRepository) UpdateRuntime(ctx context.Context, in UpdateRuntimeInput) (*db.Cluster, error) {
	row, err := r.queries.UpdateClusterRuntime(ctx, db.UpdateClusterRuntimeParams{
		ID:                  in.ID,
		KubeconfigEncrypted: in.KubeconfigEncrypted,
		ServerUrl:           optionalString(in.ServerURL),
		KubernetesVersion:   optionalString(in.KubernetesVersion),
		NodeCount:           in.NodeCount,
		Status:              in.Status,
		LastError:           optionalString(in.LastError),
	})
	if err != nil {
		return nil, fmt.Errorf("update cluster runtime: %w", err)
	}
	return &row, nil
}

// SetActive marks id as the sole active cluster.
func (r *ClusterRepository) SetActive(ctx context.Context, id pgtype.UUID) (*db.Cluster, error) {
	if err := r.queries.ClearActiveCluster(ctx); err != nil {
		return nil, fmt.Errorf("clear active cluster: %w", err)
	}
	row, err := r.queries.SetClusterActive(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("set active cluster: %w", err)
	}
	return &row, nil
}

// ClearActive leaves the platform with no selected cluster.
func (r *ClusterRepository) ClearActive(ctx context.Context) error {
	if err := r.queries.ClearActiveCluster(ctx); err != nil {
		return fmt.Errorf("clear active cluster: %w", err)
	}
	return nil
}

// SetProvider reclassifies which tool owns a cluster.
func (r *ClusterRepository) SetProvider(ctx context.Context, id pgtype.UUID, provider string) (*db.Cluster, error) {
	row, err := r.queries.SetClusterProvider(ctx, db.SetClusterProviderParams{
		ID:       id,
		Provider: provider,
	})
	if err != nil {
		return nil, fmt.Errorf("set cluster provider: %w", err)
	}
	return &row, nil
}

// SetIdentity records the UID of the physical cluster behind this row.
func (r *ClusterRepository) SetIdentity(ctx context.Context, id pgtype.UUID, clusterUID string) (*db.Cluster, error) {
	row, err := r.queries.SetClusterIdentity(ctx, db.SetClusterIdentityParams{
		ID:         id,
		ClusterUid: optionalString(clusterUID),
	})
	if err != nil {
		return nil, fmt.Errorf("set cluster identity: %w", err)
	}
	return &row, nil
}

// Delete removes a cluster row.
func (r *ClusterRepository) Delete(ctx context.Context, id pgtype.UUID) error {
	if err := r.queries.DeleteCluster(ctx, id); err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	return nil
}
