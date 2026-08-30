package auth

// Access classifies what a caller must hold to invoke a procedure.
type Access int

const (
	// AccessPublic requires nothing. Reserved for liveness checks.
	AccessPublic Access = iota
	// AccessRead requires any authenticated user, including a viewer.
	AccessRead
	// AccessWrite requires a role that may change cluster state.
	AccessWrite
	// AccessAdmin requires the admin role. Used for operations that widen
	// access, destroy tenant data, or touch stored credentials.
	AccessAdmin
)

// policy maps every RPC procedure to the access level it demands.
//
// The map is exhaustive by design and TestPolicyCoversEveryProcedure fails the
// build if a procedure is added without a matching entry. An unlisted procedure
// is denied rather than defaulted, so forgetting to classify a new mutating RPC
// makes it unreachable instead of unprotected — the failure lands on the
// developer, not on a tenant.
var policy = map[string]Access{
	// Health. Public so a load balancer can probe without a token.
	"/idp.v1.HealthService/Check": AccessPublic,

	// Audit. Reading the audit trail reveals activity across every tenant,
	// so it is not a general read.
	"/idp.v1.AuditService/ListAuditLogs": AccessAdmin,

	// Namespace. Creation and deletion define tenant boundaries.
	"/idp.v1.NamespaceService/ListNamespaces":      AccessRead,
	"/idp.v1.NamespaceService/GetNamespace":        AccessRead,
	"/idp.v1.NamespaceService/CreateNamespace":     AccessAdmin,
	"/idp.v1.NamespaceService/DeleteNamespace":     AccessAdmin,
	"/idp.v1.NamespaceService/SetNamespaceProject": AccessAdmin,

	// Deployment. Developers own the workload lifecycle inside a namespace.
	"/idp.v1.DeploymentService/ListDeployments":         AccessRead,
	"/idp.v1.DeploymentService/GetDeployment":           AccessRead,
	"/idp.v1.DeploymentService/GetDeploymentConfig":     AccessRead,
	"/idp.v1.DeploymentService/ListRollouts":            AccessRead,
	"/idp.v1.DeploymentService/ListDeploymentTemplates": AccessRead,
	"/idp.v1.DeploymentService/CreateDeployment":        AccessWrite,
	"/idp.v1.DeploymentService/UpdateDeploymentConfig":  AccessWrite,
	"/idp.v1.DeploymentService/ScaleDeployment":         AccessWrite,
	"/idp.v1.DeploymentService/RestartDeployment":       AccessWrite,
	"/idp.v1.DeploymentService/RollbackDeployment":      AccessWrite,
	"/idp.v1.DeploymentService/DeleteDeployment":        AccessWrite,

	// Cluster. Read-only views, but pod logs can contain application secrets,
	// so they are held to the same bar as other reads rather than made public.
	"/idp.v1.ClusterService/GetOverview":           AccessRead,
	"/idp.v1.ClusterService/ListEvents":            AccessRead,
	"/idp.v1.ClusterService/ListPods":              AccessRead,
	"/idp.v1.ClusterService/ListServices":          AccessRead,
	"/idp.v1.ClusterService/ListNodes":             AccessRead,
	"/idp.v1.ClusterService/GetResourceMetrics":    AccessRead,
	"/idp.v1.ClusterService/GetPodLogs":            AccessRead,
	"/idp.v1.ClusterService/StreamPodLogs":         AccessRead,
	"/idp.v1.ClusterService/StreamClusterLogs":     AccessRead,
	"/idp.v1.ClusterService/ListClusterNamespaces": AccessRead,
	"/idp.v1.ClusterService/GetNamespaceResources": AccessRead,
	"/idp.v1.ClusterService/ListClusters":          AccessRead,
	"/idp.v1.ClusterService/CreateCluster":         AccessAdmin,
	"/idp.v1.ClusterService/ActivateCluster":       AccessAdmin,
	"/idp.v1.ClusterService/StopCluster":           AccessAdmin,
	"/idp.v1.ClusterService/RestartCluster":        AccessAdmin,
	"/idp.v1.ClusterService/DeleteCluster":         AccessAdmin,

	// Storage. Entirely read-only today.
	"/idp.v1.StorageService/GetStorageOverview":         AccessRead,
	"/idp.v1.StorageService/ListPersistentVolumeClaims": AccessRead,
	"/idp.v1.StorageService/ListPersistentVolumes":      AccessRead,
	"/idp.v1.StorageService/ListStorageClasses":         AccessRead,
	"/idp.v1.StorageService/ListNodeStorage":            AccessRead,

	// Databases. Browse is read; dump/restore and PVC attach change tenant data.
	"/idp.v1.DatabaseService/ListDatabases":             AccessRead,
	"/idp.v1.DatabaseService/InspectDatabase":           AccessRead,
	"/idp.v1.DatabaseService/QueryDocuments":            AccessRead,
	"/idp.v1.DatabaseService/ExportDatabase":            AccessWrite,
	"/idp.v1.DatabaseService/ImportDatabase":            AccessWrite,
	"/idp.v1.DatabaseService/EnsureDatabasePersistence": AccessWrite,

	// Project. Membership changes grant access, so they are admin-only.
	"/idp.v1.ProjectService/ListProjects":  AccessRead,
	"/idp.v1.ProjectService/GetProject":    AccessRead,
	"/idp.v1.ProjectService/ListMembers":   AccessRead,
	"/idp.v1.ProjectService/CreateProject": AccessWrite,
	"/idp.v1.ProjectService/UpdateProject": AccessWrite,
	"/idp.v1.ProjectService/DeleteProject": AccessAdmin,
	"/idp.v1.ProjectService/AddMember":     AccessAdmin,
	"/idp.v1.ProjectService/RemoveMember":  AccessAdmin,

	// Registry. Every write here handles a credential.
	"/idp.v1.RegistryService/ListRegistryCredentials":  AccessRead,
	"/idp.v1.RegistryService/TestRegistryConnection":   AccessWrite,
	"/idp.v1.RegistryService/SaveRegistryCredential":   AccessAdmin,
	"/idp.v1.RegistryService/DeleteRegistryCredential": AccessAdmin,

	// Build. Triggering a build runs arbitrary repository code in-cluster.
	"/idp.v1.BuildService/ListGitRepositories": AccessRead,
	"/idp.v1.BuildService/ListBuilds":          AccessRead,
	"/idp.v1.BuildService/TriggerBuild":        AccessWrite,
	"/idp.v1.BuildService/RetryBuild":          AccessWrite,
	"/idp.v1.BuildService/SaveGitRepository":   AccessWrite,
	"/idp.v1.BuildService/DeleteGitRepository": AccessAdmin,
}

// AccessFor reports the level a procedure demands and whether it is classified.
func AccessFor(procedure string) (Access, bool) {
	level, ok := policy[procedure]
	return level, ok
}

// Procedures returns every classified procedure. Used by the coverage test.
func Procedures() []string {
	out := make([]string, 0, len(policy))
	for procedure := range policy {
		out = append(out, procedure)
	}
	return out
}

// Allows reports whether a user satisfies an access level.
func Allows(user *User, level Access) bool {
	switch level {
	case AccessPublic:
		return true
	case AccessRead:
		// Any recognised role. A token carrying none of the platform's roles
		// authenticated successfully but was never granted access here.
		return user != nil &&
			(user.HasRole(RoleViewer) || user.HasRole(RoleDeveloper) || user.HasRole(RoleAdmin))
	case AccessWrite:
		return user != nil && user.CanWrite()
	case AccessAdmin:
		return user != nil && user.IsAdmin()
	default:
		return false
	}
}
