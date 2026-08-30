package auth

import (
	"strings"
	"testing"
)

// procedureCatalogue lists every procedure the platform serves.
//
// Kept as a literal rather than reflected out of the generated Connect code so
// that adding an RPC forces a deliberate decision in two places: here, and in
// the policy table. A single generated source would let both sides drift
// together and the coverage test would never notice.
var procedureCatalogue = []string{
	"/idp.v1.HealthService/Check",

	"/idp.v1.AuditService/ListAuditLogs",

	"/idp.v1.NamespaceService/CreateNamespace",
	"/idp.v1.NamespaceService/GetNamespace",
	"/idp.v1.NamespaceService/ListNamespaces",
	"/idp.v1.NamespaceService/DeleteNamespace",
	"/idp.v1.NamespaceService/SetNamespaceProject",

	"/idp.v1.DeploymentService/CreateDeployment",
	"/idp.v1.DeploymentService/GetDeployment",
	"/idp.v1.DeploymentService/ListDeployments",
	"/idp.v1.DeploymentService/ScaleDeployment",
	"/idp.v1.DeploymentService/RestartDeployment",
	"/idp.v1.DeploymentService/DeleteDeployment",
	"/idp.v1.DeploymentService/GetDeploymentConfig",
	"/idp.v1.DeploymentService/UpdateDeploymentConfig",
	"/idp.v1.DeploymentService/ListRollouts",
	"/idp.v1.DeploymentService/RollbackDeployment",
	"/idp.v1.DeploymentService/ListDeploymentTemplates",

	"/idp.v1.ClusterService/GetOverview",
	"/idp.v1.ClusterService/ListEvents",
	"/idp.v1.ClusterService/ListPods",
	"/idp.v1.ClusterService/ListServices",
	"/idp.v1.ClusterService/GetPodLogs",
	"/idp.v1.ClusterService/StreamPodLogs",
	"/idp.v1.ClusterService/StreamClusterLogs",
	"/idp.v1.ClusterService/ListNodes",
	"/idp.v1.ClusterService/GetResourceMetrics",
	"/idp.v1.ClusterService/ListClusterNamespaces",
	"/idp.v1.ClusterService/GetNamespaceResources",
	"/idp.v1.ClusterService/ListClusters",
	"/idp.v1.ClusterService/CreateCluster",
	"/idp.v1.ClusterService/ActivateCluster",
	"/idp.v1.ClusterService/StopCluster",
	"/idp.v1.ClusterService/RestartCluster",
	"/idp.v1.ClusterService/DeleteCluster",

	"/idp.v1.ProjectService/CreateProject",
	"/idp.v1.ProjectService/GetProject",
	"/idp.v1.ProjectService/ListProjects",
	"/idp.v1.ProjectService/UpdateProject",
	"/idp.v1.ProjectService/DeleteProject",
	"/idp.v1.ProjectService/AddMember",
	"/idp.v1.ProjectService/RemoveMember",
	"/idp.v1.ProjectService/ListMembers",

	"/idp.v1.RegistryService/SaveRegistryCredential",
	"/idp.v1.RegistryService/ListRegistryCredentials",
	"/idp.v1.RegistryService/DeleteRegistryCredential",
	"/idp.v1.RegistryService/TestRegistryConnection",

	"/idp.v1.BuildService/SaveGitRepository",
	"/idp.v1.BuildService/ListGitRepositories",
	"/idp.v1.BuildService/DeleteGitRepository",
	"/idp.v1.BuildService/TriggerBuild",
	"/idp.v1.BuildService/RetryBuild",
	"/idp.v1.BuildService/ListBuilds",

	"/idp.v1.StorageService/GetStorageOverview",
	"/idp.v1.StorageService/ListPersistentVolumeClaims",
	"/idp.v1.StorageService/ListPersistentVolumes",
	"/idp.v1.StorageService/ListStorageClasses",
	"/idp.v1.StorageService/ListNodeStorage",

	"/idp.v1.DatabaseService/ListDatabases",
	"/idp.v1.DatabaseService/InspectDatabase",
	"/idp.v1.DatabaseService/QueryDocuments",
	"/idp.v1.DatabaseService/ExportDatabase",
	"/idp.v1.DatabaseService/ImportDatabase",
	"/idp.v1.DatabaseService/EnsureDatabasePersistence",
}

func TestPolicyCoversEveryProcedure(t *testing.T) {
	for _, procedure := range procedureCatalogue {
		if _, ok := AccessFor(procedure); !ok {
			t.Errorf("procedure %s has no access policy entry; "+
				"add one to policy in authz.go or it will be denied at runtime", procedure)
		}
	}
}

func TestPolicyHasNoStaleEntries(t *testing.T) {
	known := make(map[string]bool, len(procedureCatalogue))
	for _, procedure := range procedureCatalogue {
		known[procedure] = true
	}

	for _, procedure := range Procedures() {
		if !known[procedure] {
			t.Errorf("policy classifies %s, which is not a served procedure", procedure)
		}
	}
}

// TestMutatingProceduresAreNotReadable is the regression guard for the finding
// that RequireRole was defined and never wired: every verb that changes state
// must demand more than AccessRead.
func TestMutatingProceduresAreNotReadable(t *testing.T) {
	mutatingVerbs := []string{
		"Create", "Update", "Delete", "Scale", "Restart",
		"Rollback", "Save", "Add", "Remove", "Trigger", "Retry", "Set",
		"Activate", "Stop",
	}

	for _, procedure := range procedureCatalogue {
		_, method, _ := strings.Cut(strings.TrimPrefix(procedure, "/"), "/")

		mutating := false
		for _, verb := range mutatingVerbs {
			if strings.HasPrefix(method, verb) {
				mutating = true
				break
			}
		}
		if !mutating {
			continue
		}

		level, ok := AccessFor(procedure)
		if !ok {
			continue // reported by TestPolicyCoversEveryProcedure
		}
		if level != AccessWrite && level != AccessAdmin {
			t.Errorf("mutating procedure %s is classified below AccessWrite", procedure)
		}
	}
}

func TestAllows(t *testing.T) {
	viewer := &User{Roles: []Role{RoleViewer}}
	developer := &User{Roles: []Role{RoleDeveloper}}
	admin := &User{Roles: []Role{RoleAdmin}}
	roleless := &User{Roles: []Role{"some-unrelated-keycloak-role"}}

	tests := []struct {
		name  string
		user  *User
		level Access
		want  bool
	}{
		{"public allows nil user", nil, AccessPublic, true},
		{"read allows viewer", viewer, AccessRead, true},
		{"read allows developer", developer, AccessRead, true},
		{"read allows admin", admin, AccessRead, true},
		{"read denies roleless token", roleless, AccessRead, false},
		{"read denies nil user", nil, AccessRead, false},

		{"write denies viewer", viewer, AccessWrite, false},
		{"write allows developer", developer, AccessWrite, true},
		{"write allows admin", admin, AccessWrite, true},

		{"admin denies viewer", viewer, AccessAdmin, false},
		{"admin denies developer", developer, AccessAdmin, false},
		{"admin allows admin", admin, AccessAdmin, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Allows(tt.user, tt.level); got != tt.want {
				t.Errorf("Allows() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestViewerCannotReachDestructiveProcedures spells out the concrete claim from
// the audit: a viewer token used to be accepted for these.
func TestViewerCannotReachDestructiveProcedures(t *testing.T) {
	viewer := &User{Roles: []Role{RoleViewer}}

	destructive := []string{
		"/idp.v1.NamespaceService/DeleteNamespace",
		"/idp.v1.DeploymentService/DeleteDeployment",
		"/idp.v1.RegistryService/SaveRegistryCredential",
		"/idp.v1.ProjectService/DeleteProject",
		"/idp.v1.BuildService/TriggerBuild",
	}

	for _, procedure := range destructive {
		level, ok := AccessFor(procedure)
		if !ok {
			t.Fatalf("%s missing from policy", procedure)
		}
		if Allows(viewer, level) {
			t.Errorf("viewer is permitted to call %s", procedure)
		}
	}
}
