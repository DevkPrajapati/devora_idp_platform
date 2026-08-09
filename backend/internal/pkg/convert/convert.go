package convert

import (
	"encoding/json"
	"time"

	db "github.com/idp/platform/backend/internal/database/sqlc"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/jackc/pgx/v5/pgtype"
)

// NamespaceToProto converts a database namespace to protobuf. projectSlug is
// resolved by the caller, which already holds the project repository; passing
// it in keeps this package free of database lookups.
func NamespaceToProto(ns db.Namespace, projectSlug string) *idpv1.Namespace {
	labels := jsonToMap(ns.Labels)
	annotations := jsonToMap(ns.Annotations)

	return &idpv1.Namespace{
		Id:          uuidToString(ns.ID),
		Name:        ns.Name,
		DisplayName: ns.DisplayName,
		Description: derefString(ns.Description),
		OwnerId:     ns.OwnerID,
		OwnerEmail:  ns.OwnerEmail,
		Labels:      labels,
		Annotations: annotations,
		Status:      ns.Status,
		ProjectSlug: projectSlug,
		CreatedAt:   timestamptzToString(ns.CreatedAt),
		UpdatedAt:   timestamptzToString(ns.UpdatedAt),
	}
}

// AuditLogToProto converts a database audit log to protobuf.
func AuditLogToProto(log db.AuditLog) *idpv1.AuditLog {
	return &idpv1.AuditLog{
		Id:           uuidToString(log.ID),
		UserId:       log.UserID,
		UserEmail:    log.UserEmail,
		Action:       log.Action,
		Namespace:    derefString(log.Namespace),
		Resource:     derefString(log.Resource),
		ResourceType: derefString(log.ResourceType),
		Result:       log.Result,
		Details:      string(log.Details),
		CreatedAt:    timestamptzToString(log.CreatedAt),
	}
}

// DeploymentToProto converts K8s deployment info to protobuf.
func DeploymentToProto(d *kubernetes.DeploymentInfo) *idpv1.Deployment {
	envVars := make([]*idpv1.KeyValue, 0, len(d.EnvVars))
	for k, v := range d.EnvVars {
		envVars = append(envVars, &idpv1.KeyValue{Key: k, Value: v})
	}

	return &idpv1.Deployment{
		Name:              d.Name,
		Namespace:         d.Namespace,
		Image:             d.Image,
		Replicas:          d.Replicas,
		ReadyReplicas:     d.ReadyReplicas,
		AvailableReplicas: d.AvailableReplicas,
		Status:            d.Status,
		StatusReason:      d.StatusReason,
		Port:              d.Port,
		ServiceType:       d.ServiceType,
		NodePort:          d.NodePort,
		ClusterIp:         d.ClusterIP,
		ClusterAddress:    d.ClusterAddress,
		EnvVars:           envVars,
		ConfigMapName:     d.ConfigMapName,
		SecretName:        d.SecretName,
		// Key names only. DeploymentInfo never carries secret values, so there
		// is nothing here that could leak one into an API response.
		SecretKeys:       d.SecretKeys,
		ImagePullSecrets: d.ImagePullSecrets,
		Url:              d.URL,
		IngressHost:      d.IngressHost,
		ReadinessProbe:   probeToProto(d.ReadinessProbe),
		LivenessProbe:    probeToProto(d.LivenessProbe),
		Resources:        resourcesToProto(d.Resources),
		CreatedAt:        d.CreatedAt.Format(time.RFC3339),
	}
}

// probeToProto renders a probe for display, or nil when the workload has none.
// Returning nil rather than a zeroed message lets the UI distinguish "no probe
// configured" from "a probe with empty settings".
func probeToProto(p kubernetes.ProbeSpec) *idpv1.Probe {
	if !p.Configured() {
		return nil
	}
	return &idpv1.Probe{
		Path:                p.Path,
		Port:                p.Port,
		InitialDelaySeconds: p.InitialDelaySeconds,
		TimeoutSeconds:      p.TimeoutSeconds,
		PeriodSeconds:       p.PeriodSeconds,
		FailureThreshold:    p.FailureThreshold,
	}
}

// resourcesToProto renders container resources for display, or nil when the
// workload has none — nil lets the UI say "unset" rather than showing zeros.
func resourcesToProto(r kubernetes.ResourceSpec) *idpv1.ResourceLimits {
	if r.Empty() {
		return nil
	}
	return &idpv1.ResourceLimits{
		CpuRequest:    r.CPURequest,
		CpuLimit:      r.CPULimit,
		MemoryRequest: r.MemoryRequest,
		MemoryLimit:   r.MemoryLimit,
	}
}

// RolloutToProto converts a rollout revision to protobuf.
func RolloutToProto(r kubernetes.RolloutInfo) *idpv1.Rollout {
	return &idpv1.Rollout{
		Revision:      r.Revision,
		CreatedAt:     r.CreatedAt.Format(time.RFC3339),
		Image:         r.Image,
		Replicas:      r.Replicas,
		ReadyReplicas: r.ReadyReplicas,
		Status:        r.Status,
		ChangeCause:   r.ChangeCause,
		Current:       r.Current,
	}
}

// LogLineToProto converts a streamed log line to protobuf.
func LogLineToProto(l kubernetes.LogLine) *idpv1.LogLine {
	return &idpv1.LogLine{
		PodName:   l.PodName,
		Timestamp: l.Timestamp,
		Message:   l.Message,
	}
}

// ClusterOverviewToProto converts cluster overview to protobuf.
func ClusterOverviewToProto(o *kubernetes.ClusterOverview) *idpv1.GetOverviewResponse {
	return &idpv1.GetOverviewResponse{
		ClusterName:     o.ClusterName,
		Connected:       o.Connected,
		NamespaceCount:  o.NamespaceCount,
		DeploymentCount: o.DeploymentCount,
		ServiceCount:    o.ServiceCount,
		PodCount:        o.PodCount,
		RunningPods:     o.RunningPods,
		NodeCount:       o.NodeCount,
		ReadyNodes:      o.ReadyNodes,
	}
}

// ClusterEventToProto converts a cluster event to protobuf.
func ClusterEventToProto(e kubernetes.ClusterEvent) *idpv1.ClusterEvent {
	return &idpv1.ClusterEvent{
		Type:      e.Type,
		Reason:    e.Reason,
		Message:   e.Message,
		Namespace: e.Namespace,
		Object:    e.Object,
		Timestamp: e.Timestamp.Format(time.RFC3339),
	}
}

func jsonToMap(data []byte) map[string]string {
	result := make(map[string]string)
	if len(data) == 0 {
		return result
	}
	_ = json.Unmarshal(data, &result)
	return result
}

func uuidToString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	val, err := id.Value()
	if err != nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

func timestamptzToString(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.Format(time.RFC3339)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PodToProto converts K8s pod info to protobuf.
func PodToProto(p kubernetes.PodInfo) *idpv1.PodInfo {
	return &idpv1.PodInfo{
		Name:         p.Name,
		Namespace:    p.Namespace,
		Status:       p.Status,
		Ip:           p.IP,
		Node:         p.Node,
		RestartCount: p.RestartCount,
		CreatedAt:    p.CreatedAt.Format(time.RFC3339),
	}
}

// ServiceToProto converts K8s service info to protobuf.
func ServiceToProto(s kubernetes.ServiceInfo) *idpv1.ServiceInfo {
	return &idpv1.ServiceInfo{
		Name:       s.Name,
		Namespace:  s.Namespace,
		Type:       s.Type,
		ClusterIp:  s.ClusterIP,
		ExternalIp: s.ExternalIP,
		Ports:      s.Ports,
		CreatedAt:  s.CreatedAt.Format(time.RFC3339),
	}
}

// NodeToProto converts node info to protobuf.
func NodeToProto(n kubernetes.NodeInfo) *idpv1.NodeInfo {
	return &idpv1.NodeInfo{
		Name:              n.Name,
		Status:            n.Status,
		Role:              n.Role,
		CpuCapacity:       n.CPUCapacity,
		MemoryCapacity:    n.MemoryCapacity,
		CpuAllocatable:    n.CPUAllocatable,
		MemoryAllocatable: n.MemoryAllocatable,
	}
}

// ProjectToProto converts a database project row to protobuf.
func ProjectToProto(p db.Project, memberCount, namespaceCount int32) *idpv1.Project {
	return &idpv1.Project{
		Id:             uuidToString(p.ID),
		Slug:           p.Slug,
		Name:           p.Name,
		Description:    derefString(p.Description),
		OwnerId:        p.OwnerID,
		OwnerEmail:     p.OwnerEmail,
		Labels:         jsonToMap(p.Labels),
		Status:         p.Status,
		MemberCount:    memberCount,
		NamespaceCount: namespaceCount,
		CreatedAt:      timestamptzToString(p.CreatedAt),
		UpdatedAt:      timestamptzToString(p.UpdatedAt),
	}
}

// ProjectMemberToProto converts a project_members row to protobuf.
func ProjectMemberToProto(m db.ProjectMember) *idpv1.ProjectMember {
	return &idpv1.ProjectMember{
		ProjectId: uuidToString(m.ProjectID),
		UserId:    m.UserID,
		UserEmail: m.UserEmail,
		Role:      m.Role,
		AddedAt:   timestamptzToString(m.AddedAt),
	}
}

// ResourceMetricsToProto converts resource metrics to protobuf.
func ResourceMetricsToProto(m *kubernetes.ResourceMetrics) *idpv1.GetResourceMetricsResponse {
	return &idpv1.GetResourceMetricsResponse{
		CpuRequests:        m.CPURequests,
		CpuCapacity:        m.CPUCapacity,
		CpuUsagePercent:    m.CPUUsagePercent,
		MemoryRequests:     m.MemoryRequests,
		MemoryCapacity:     m.MemoryCapacity,
		MemoryUsagePercent: m.MemoryUsagePercent,
	}
}
