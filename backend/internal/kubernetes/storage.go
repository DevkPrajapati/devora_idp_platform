package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// isDefaultStorageClassAnnotation marks the class the API server falls back to
// when a claim names none. The beta key is still what most clusters carry.
const (
	isDefaultStorageClassAnnotation     = "storageclass.kubernetes.io/is-default-class"
	isDefaultStorageClassAnnotationBeta = "storageclass.beta.kubernetes.io/is-default-class"
)

// PersistentVolumeClaim is the read model for one claim.
type PersistentVolumeClaim struct {
	Name           string
	Namespace      string
	Phase          string
	VolumeName     string
	Requested      string
	Capacity       string
	RequestedBytes int64
	CapacityBytes  int64
	StorageClass   string
	AccessModes    []string
	VolumeMode     string
	CreatedAt      string
	UsedBy         []string
}

// PersistentVolume is the read model for one cluster volume.
type PersistentVolume struct {
	Name          string
	Phase         string
	Capacity      string
	CapacityBytes int64
	StorageClass  string
	AccessModes   []string
	ReclaimPolicy string
	Claim         string
	Driver        string
	CreatedAt     string
}

// StorageClass is the read model for one provisioner class.
type StorageClass struct {
	Name                 string
	Provisioner          string
	ReclaimPolicy        string
	VolumeBindingMode    string
	AllowVolumeExpansion bool
	IsDefault            bool
	CreatedAt            string
}

// NodeStorage carries a node's container runtime and local disk footprint.
type NodeStorage struct {
	Name                             string
	ContainerRuntime                 string
	RuntimeName                      string
	RuntimeVersion                   string
	KubeletVersion                   string
	OSImage                          string
	KernelVersion                    string
	Architecture                     string
	OperatingSystem                  string
	EphemeralStorageCapacity         string
	EphemeralStorageAllocatable      string
	EphemeralStorageCapacityBytes    int64
	EphemeralStorageAllocatableBytes int64
	ImageCount                       int32
	ImageBytes                       int64
	ImageSize                        string
	DiskPressure                     bool
	Ready                            bool
}

// StorageOverview aggregates the counts a dashboard leads with.
type StorageOverview struct {
	PVCCount            int32
	BoundPVCCount       int32
	PendingPVCCount     int32
	PVCount             int32
	AvailablePVCount    int32
	StorageClassCount   int32
	TotalRequested      string
	TotalCapacity       string
	TotalRequestedBytes int64
	TotalCapacityBytes  int64
}

// ListPersistentVolumeClaims returns claims in a namespace, or across all
// namespaces when namespace is empty.
//
// Pods are listed alongside so each claim can report what is mounting it: a
// claim nothing references is still consuming its volume, and that is invisible
// from the claim object alone.
func (c *Client) ListPersistentVolumeClaims(ctx context.Context, namespace string) ([]PersistentVolumeClaim, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list persistentvolumeclaims: %w", err)
	}

	// A failure here degrades the "used by" column to empty rather than
	// failing the whole page — the claims themselves are still worth showing.
	mounts := map[string][]string{}
	if pods, podErr := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{}); podErr == nil {
		for i := range pods.Items {
			pod := &pods.Items[i]
			for _, vol := range pod.Spec.Volumes {
				if vol.PersistentVolumeClaim == nil {
					continue
				}
				key := pod.Namespace + "/" + vol.PersistentVolumeClaim.ClaimName
				mounts[key] = append(mounts[key], pod.Name)
			}
		}
	}

	claims := make([]PersistentVolumeClaim, 0, len(list.Items))
	for i := range list.Items {
		pvc := &list.Items[i]

		requested, requestedBytes := quantityFrom(pvc.Spec.Resources.Requests, corev1.ResourceStorage)
		capacity, capacityBytes := quantityFrom(pvc.Status.Capacity, corev1.ResourceStorage)

		volumeMode := ""
		if pvc.Spec.VolumeMode != nil {
			volumeMode = string(*pvc.Spec.VolumeMode)
		}

		storageClass := ""
		if pvc.Spec.StorageClassName != nil {
			storageClass = *pvc.Spec.StorageClassName
		}

		usedBy := mounts[pvc.Namespace+"/"+pvc.Name]
		sort.Strings(usedBy)

		claims = append(claims, PersistentVolumeClaim{
			Name:           pvc.Name,
			Namespace:      pvc.Namespace,
			Phase:          string(pvc.Status.Phase),
			VolumeName:     pvc.Spec.VolumeName,
			Requested:      requested,
			Capacity:       capacity,
			RequestedBytes: requestedBytes,
			CapacityBytes:  capacityBytes,
			StorageClass:   storageClass,
			AccessModes:    accessModeStrings(pvc.Spec.AccessModes),
			VolumeMode:     volumeMode,
			CreatedAt:      formatTimestamp(pvc.CreationTimestamp),
			UsedBy:         usedBy,
		})
	}

	sort.Slice(claims, func(a, b int) bool {
		if claims[a].Namespace != claims[b].Namespace {
			return claims[a].Namespace < claims[b].Namespace
		}
		return claims[a].Name < claims[b].Name
	})

	return claims, nil
}

// ListPersistentVolumes returns every cluster-scoped volume.
func (c *Client) ListPersistentVolumes(ctx context.Context) ([]PersistentVolume, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list persistentvolumes: %w", err)
	}

	volumes := make([]PersistentVolume, 0, len(list.Items))
	for i := range list.Items {
		pv := &list.Items[i]

		capacity, capacityBytes := quantityFrom(pv.Spec.Capacity, corev1.ResourceStorage)

		claim := ""
		if pv.Spec.ClaimRef != nil {
			claim = pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name
		}

		volumes = append(volumes, PersistentVolume{
			Name:          pv.Name,
			Phase:         string(pv.Status.Phase),
			Capacity:      capacity,
			CapacityBytes: capacityBytes,
			StorageClass:  pv.Spec.StorageClassName,
			AccessModes:   accessModeStrings(pv.Spec.AccessModes),
			ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy),
			Claim:         claim,
			Driver:        volumeDriver(pv),
			CreatedAt:     formatTimestamp(pv.CreationTimestamp),
		})
	}

	sort.Slice(volumes, func(a, b int) bool { return volumes[a].Name < volumes[b].Name })
	return volumes, nil
}

// ListStorageClasses returns the provisioner classes available to claims.
func (c *Client) ListStorageClasses(ctx context.Context) ([]StorageClass, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list storageclasses: %w", err)
	}

	classes := make([]StorageClass, 0, len(list.Items))
	for i := range list.Items {
		sc := &list.Items[i]

		reclaim := ""
		if sc.ReclaimPolicy != nil {
			reclaim = string(*sc.ReclaimPolicy)
		}
		binding := ""
		if sc.VolumeBindingMode != nil {
			binding = string(*sc.VolumeBindingMode)
		}
		expansion := sc.AllowVolumeExpansion != nil && *sc.AllowVolumeExpansion

		classes = append(classes, StorageClass{
			Name:                 sc.Name,
			Provisioner:          sc.Provisioner,
			ReclaimPolicy:        reclaim,
			VolumeBindingMode:    binding,
			AllowVolumeExpansion: expansion,
			IsDefault: sc.Annotations[isDefaultStorageClassAnnotation] == "true" ||
				sc.Annotations[isDefaultStorageClassAnnotationBeta] == "true",
			CreatedAt: formatTimestamp(sc.CreationTimestamp),
		})
	}

	sort.Slice(classes, func(a, b int) bool { return classes[a].Name < classes[b].Name })
	return classes, nil
}

// ListNodeStorage reports each node's container runtime and image footprint.
func (c *Client) ListNodeStorage(ctx context.Context) ([]NodeStorage, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	nodes := make([]NodeStorage, 0, len(list.Items))
	for i := range list.Items {
		node := &list.Items[i]
		info := node.Status.NodeInfo

		runtimeName, runtimeVersion := splitRuntime(info.ContainerRuntimeVersion)

		capacity, capacityBytes := quantityFrom(node.Status.Capacity, corev1.ResourceEphemeralStorage)
		allocatable, allocatableBytes := quantityFrom(node.Status.Allocatable, corev1.ResourceEphemeralStorage)

		var imageBytes int64
		for _, img := range node.Status.Images {
			imageBytes += img.SizeBytes
		}

		var diskPressure, ready bool
		for _, cond := range node.Status.Conditions {
			switch cond.Type {
			case corev1.NodeDiskPressure:
				diskPressure = cond.Status == corev1.ConditionTrue
			case corev1.NodeReady:
				ready = cond.Status == corev1.ConditionTrue
			}
		}

		nodes = append(nodes, NodeStorage{
			Name:                             node.Name,
			ContainerRuntime:                 info.ContainerRuntimeVersion,
			RuntimeName:                      runtimeName,
			RuntimeVersion:                   runtimeVersion,
			KubeletVersion:                   info.KubeletVersion,
			OSImage:                          info.OSImage,
			KernelVersion:                    info.KernelVersion,
			Architecture:                     info.Architecture,
			OperatingSystem:                  info.OperatingSystem,
			EphemeralStorageCapacity:         capacity,
			EphemeralStorageAllocatable:      allocatable,
			EphemeralStorageCapacityBytes:    capacityBytes,
			EphemeralStorageAllocatableBytes: allocatableBytes,
			ImageCount:                       int32(len(node.Status.Images)),
			ImageBytes:                       imageBytes,
			ImageSize:                        FormatBytes(imageBytes),
			DiskPressure:                     diskPressure,
			Ready:                            ready,
		})
	}

	sort.Slice(nodes, func(a, b int) bool { return nodes[a].Name < nodes[b].Name })
	return nodes, nil
}

// GetStorageOverview aggregates claim, volume and class counts.
func (c *Client) GetStorageOverview(ctx context.Context) (StorageOverview, error) {
	claims, err := c.ListPersistentVolumeClaims(ctx, "")
	if err != nil {
		return StorageOverview{}, err
	}

	volumes, err := c.ListPersistentVolumes(ctx)
	if err != nil {
		return StorageOverview{}, err
	}

	classes, err := c.ListStorageClasses(ctx)
	if err != nil {
		return StorageOverview{}, err
	}

	overview := StorageOverview{
		PVCCount:          int32(len(claims)),
		PVCount:           int32(len(volumes)),
		StorageClassCount: int32(len(classes)),
	}

	for _, claim := range claims {
		switch claim.Phase {
		case string(corev1.ClaimBound):
			overview.BoundPVCCount++
		case string(corev1.ClaimPending):
			overview.PendingPVCCount++
		}
		overview.TotalRequestedBytes += claim.RequestedBytes
	}

	for _, volume := range volumes {
		if volume.Phase == string(corev1.VolumeAvailable) {
			overview.AvailablePVCount++
		}
		overview.TotalCapacityBytes += volume.CapacityBytes
	}

	overview.TotalRequested = FormatBytes(overview.TotalRequestedBytes)
	overview.TotalCapacity = FormatBytes(overview.TotalCapacityBytes)

	return overview, nil
}

// quantityFrom reads one resource out of a quantity map, returning both the
// canonical string and the byte count. A missing key yields ("", 0) rather than
// "0" so the UI can distinguish "unset" from "zero".
func quantityFrom(list corev1.ResourceList, name corev1.ResourceName) (string, int64) {
	qty, ok := list[name]
	if !ok {
		return "", 0
	}
	return qty.String(), qty.Value()
}

// FormatBytes renders a byte count in Kubernetes binary notation (Ki/Mi/Gi),
// which is what users see in manifests and in kubectl output.
func FormatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0"
	}
	return resource.NewQuantity(bytes, resource.BinarySI).String()
}

func accessModeStrings(modes []corev1.PersistentVolumeAccessMode) []string {
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		out = append(out, string(mode))
	}
	return out
}

// splitRuntime turns "containerd://1.7.13" into ("containerd", "1.7.13").
func splitRuntime(runtime string) (string, string) {
	name, version, found := strings.Cut(runtime, "://")
	if !found {
		return runtime, ""
	}
	return name, version
}

// volumeDriver names whatever backs the volume: the CSI driver where one is in
// play, otherwise the in-tree source kind.
func volumeDriver(pv *corev1.PersistentVolume) string {
	switch {
	case pv.Spec.CSI != nil:
		return pv.Spec.CSI.Driver
	case pv.Spec.HostPath != nil:
		return "hostPath"
	case pv.Spec.Local != nil:
		return "local"
	case pv.Spec.NFS != nil:
		return "nfs"
	default:
		return "unknown"
	}
}

func formatTimestamp(ts metav1.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}
