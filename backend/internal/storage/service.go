package storage

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/auth"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
)

// Service exposes the cluster's storage read model.
type Service struct {
	k8s *kubernetes.Client
}

// NewService creates a storage service. A nil client means no cluster is
// reachable; every read below degrades rather than panicking.
func NewService(k8s *kubernetes.Client) *Service {
	return &Service{k8s: k8s}
}

// requireCluster rejects reads that cannot be served, and rejects anonymous
// callers. Storage layout names every namespace and every mounted volume in the
// cluster, so it is not public the way the health probe is.
func (s *Service) requireCluster(ctx context.Context) error {
	if _, err := auth.UserFromContext(ctx); err != nil {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	if s.k8s == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("kubernetes cluster not connected"))
	}
	return nil
}

// GetStorageOverview returns aggregate claim, volume and class counts.
func (s *Service) GetStorageOverview(
	ctx context.Context,
	_ *idpv1.GetStorageOverviewRequest,
) (*idpv1.GetStorageOverviewResponse, error) {
	if _, err := auth.UserFromContext(ctx); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// The overview drives a status card, so a disconnected cluster is reported
	// as a value rather than an error — the page still renders.
	if s.k8s == nil {
		return &idpv1.GetStorageOverviewResponse{Connected: false}, nil
	}

	overview, err := s.k8s.GetStorageOverview(ctx)
	if err != nil {
		return &idpv1.GetStorageOverviewResponse{Connected: false}, nil
	}

	return &idpv1.GetStorageOverviewResponse{
		Connected:           true,
		PvcCount:            overview.PVCCount,
		BoundPvcCount:       overview.BoundPVCCount,
		PendingPvcCount:     overview.PendingPVCCount,
		PvCount:             overview.PVCount,
		AvailablePvCount:    overview.AvailablePVCount,
		StorageClassCount:   overview.StorageClassCount,
		TotalRequested:      overview.TotalRequested,
		TotalCapacity:       overview.TotalCapacity,
		TotalRequestedBytes: overview.TotalRequestedBytes,
		TotalCapacityBytes:  overview.TotalCapacityBytes,
	}, nil
}

// ListPersistentVolumeClaims returns claims, scoped to a namespace when given.
func (s *Service) ListPersistentVolumeClaims(
	ctx context.Context,
	req *idpv1.ListPersistentVolumeClaimsRequest,
) (*idpv1.ListPersistentVolumeClaimsResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}

	claims, err := s.k8s.ListPersistentVolumeClaims(ctx, req.Namespace)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.PersistentVolumeClaimInfo, 0, len(claims))
	for _, c := range claims {
		out = append(out, &idpv1.PersistentVolumeClaimInfo{
			Name:           c.Name,
			Namespace:      c.Namespace,
			Phase:          c.Phase,
			VolumeName:     c.VolumeName,
			Requested:      c.Requested,
			Capacity:       c.Capacity,
			RequestedBytes: c.RequestedBytes,
			CapacityBytes:  c.CapacityBytes,
			StorageClass:   c.StorageClass,
			AccessModes:    c.AccessModes,
			VolumeMode:     c.VolumeMode,
			CreatedAt:      c.CreatedAt,
			UsedBy:         c.UsedBy,
		})
	}

	return &idpv1.ListPersistentVolumeClaimsResponse{Claims: out}, nil
}

// ListPersistentVolumes returns every cluster-scoped volume.
func (s *Service) ListPersistentVolumes(
	ctx context.Context,
	_ *idpv1.ListPersistentVolumesRequest,
) (*idpv1.ListPersistentVolumesResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}

	volumes, err := s.k8s.ListPersistentVolumes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.PersistentVolumeInfo, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, &idpv1.PersistentVolumeInfo{
			Name:          v.Name,
			Phase:         v.Phase,
			Capacity:      v.Capacity,
			CapacityBytes: v.CapacityBytes,
			StorageClass:  v.StorageClass,
			AccessModes:   v.AccessModes,
			ReclaimPolicy: v.ReclaimPolicy,
			Claim:         v.Claim,
			Driver:        v.Driver,
			CreatedAt:     v.CreatedAt,
		})
	}

	return &idpv1.ListPersistentVolumesResponse{Volumes: out}, nil
}

// ListStorageClasses returns the provisioner classes claims can request.
func (s *Service) ListStorageClasses(
	ctx context.Context,
	_ *idpv1.ListStorageClassesRequest,
) (*idpv1.ListStorageClassesResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}

	classes, err := s.k8s.ListStorageClasses(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.StorageClassInfo, 0, len(classes))
	for _, c := range classes {
		out = append(out, &idpv1.StorageClassInfo{
			Name:                 c.Name,
			Provisioner:          c.Provisioner,
			ReclaimPolicy:        c.ReclaimPolicy,
			VolumeBindingMode:    c.VolumeBindingMode,
			AllowVolumeExpansion: c.AllowVolumeExpansion,
			IsDefault:            c.IsDefault,
			CreatedAt:            c.CreatedAt,
		})
	}

	return &idpv1.ListStorageClassesResponse{StorageClasses: out}, nil
}

// ListNodeStorage returns per-node container runtime and image footprint.
func (s *Service) ListNodeStorage(
	ctx context.Context,
	_ *idpv1.ListNodeStorageRequest,
) (*idpv1.ListNodeStorageResponse, error) {
	if err := s.requireCluster(ctx); err != nil {
		return nil, err
	}

	nodes, err := s.k8s.ListNodeStorage(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*idpv1.NodeStorageInfo, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, &idpv1.NodeStorageInfo{
			Name:                             n.Name,
			ContainerRuntime:                 n.ContainerRuntime,
			RuntimeName:                      n.RuntimeName,
			RuntimeVersion:                   n.RuntimeVersion,
			KubeletVersion:                   n.KubeletVersion,
			OsImage:                          n.OSImage,
			KernelVersion:                    n.KernelVersion,
			Architecture:                     n.Architecture,
			OperatingSystem:                  n.OperatingSystem,
			EphemeralStorageCapacity:         n.EphemeralStorageCapacity,
			EphemeralStorageAllocatable:      n.EphemeralStorageAllocatable,
			EphemeralStorageCapacityBytes:    n.EphemeralStorageCapacityBytes,
			EphemeralStorageAllocatableBytes: n.EphemeralStorageAllocatableBytes,
			ImageCount:                       n.ImageCount,
			ImageBytes:                       n.ImageBytes,
			ImageSize:                        n.ImageSize,
			DiskPressure:                     n.DiskPressure,
			Ready:                            n.Ready,
		})
	}

	return &idpv1.ListNodeStorageResponse{Nodes: out}, nil
}
