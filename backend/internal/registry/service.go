// Package registry manages private container-registry credentials.
//
// A credential lives in three places at once: encrypted in Postgres (the
// source of truth), as a kubernetes.io/dockerconfigjson Secret in every
// namespace the owning project has, and as an imagePullSecrets reference on
// every managed Deployment in those namespaces. This package owns keeping the
// three in step.
package registry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"github.com/idp/platform/backend/internal/audit"
	"github.com/idp/platform/backend/internal/auth"
	db "github.com/idp/platform/backend/internal/database/sqlc"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/pkg/secretbox"
	"github.com/idp/platform/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgtype"
)

// credentialNameRegex matches an RFC 1123 label, because the name becomes part
// of a Kubernetes Secret name.
var credentialNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

const maxCredentialNameLen = 48

// Service implements registry credential business logic.
type Service struct {
	creds      *repository.RegistryRepository
	projects   *repository.ProjectRepository
	namespaces *repository.NamespaceRepository
	k8s        *kubernetes.Client
	box        *secretbox.Box
	prober     Prober
	audit      *audit.Service
}

// Prober authenticates against a remote registry. It is an interface so tests
// can exercise the service without network access.
type Prober interface {
	Probe(ctx context.Context, registryURL, username, password string) error
}

// NewService creates a registry service.
func NewService(
	creds *repository.RegistryRepository,
	projects *repository.ProjectRepository,
	namespaces *repository.NamespaceRepository,
	k8s *kubernetes.Client,
	box *secretbox.Box,
	prober Prober,
	auditSvc *audit.Service,
) *Service {
	return &Service{
		creds:      creds,
		projects:   projects,
		namespaces: namespaces,
		k8s:        k8s,
		box:        box,
		prober:     prober,
		audit:      auditSvc,
	}
}

// authorizeProject resolves the caller and confirms they may act on a project.
// A project the caller cannot see reports NotFound rather than PermissionDenied
// so the error does not confirm that the slug exists.
func (s *Service) authorizeProject(ctx context.Context, slug string, needWrite bool) (*auth.User, *db.Project, error) {
	user, err := auth.UserFromContext(ctx)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if needWrite && !user.CanWrite() {
		return nil, nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
	}
	if strings.TrimSpace(slug) == "" {
		return nil, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_slug is required"))
	}

	project, err := s.projects.GetBySlug(ctx, slug)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
	}

	if !user.IsAdmin() && project.OwnerEmail != user.Email {
		member, memberErr := s.projects.GetMember(ctx, project.ID, user.Email)
		if memberErr != nil {
			return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("project not found"))
		}
		// Viewers may list credential metadata but must not change what the
		// cluster pulls with.
		if needWrite && member.Role != "developer" {
			return nil, nil, connect.NewError(connect.CodePermissionDenied, auth.ErrInsufficientPermissions)
		}
	}

	return user, project, nil
}

func (s *Service) requireK8s() error {
	if !s.k8s.Available() {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("kubernetes cluster not connected"))
	}
	return nil
}

func (s *Service) requireEncryption() error {
	if !s.box.Enabled() {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New(
			"IDP_ENCRYPTION_KEY is not configured; refusing to store registry passwords unencrypted"))
	}
	return nil
}

func validateCredentialName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > maxCredentialNameLen {
		return "", fmt.Errorf("name must be at most %d characters", maxCredentialNameLen)
	}
	if !credentialNameRegex.MatchString(name) {
		return "", errors.New("name must be lowercase alphanumeric with hyphens")
	}
	return name, nil
}

// Save stores or replaces a credential and reconciles it into the cluster.
func (s *Service) Save(ctx context.Context, req *idpv1.SaveRegistryCredentialRequest) (*idpv1.SaveRegistryCredentialResponse, error) {
	user, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}
	if err := s.requireEncryption(); err != nil {
		return nil, err
	}

	name, err := validateCredentialName(req.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Normalise first: a bad registry URL should be rejected before anything is
	// written, not discovered later when a pod fails to pull.
	host, err := kubernetes.NormalizeRegistryHost(req.RegistryUrl)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username is required"))
	}
	if req.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("password is required"))
	}

	sealed, err := s.box.Encrypt(req.Password)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt password: %w", err))
	}

	row, err := s.creds.Upsert(ctx, repository.UpsertRegistryCredentialInput{
		ProjectID:         project.ID,
		Name:              name,
		RegistryURL:       strings.TrimSpace(req.RegistryUrl),
		Username:          username,
		PasswordEncrypted: sealed,
		Email:             strings.TrimSpace(req.Email),
		CreatedBy:         user.Email,
	})
	if err != nil {
		s.auditFailure(ctx, "registry.credential.save", project.Slug, name, err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	cred := kubernetes.RegistryCredential{
		Name:        name,
		RegistryURL: row.RegistryUrl,
		Username:    row.Username,
		Password:    req.Password,
		Email:       derefString(row.Email),
		ProjectSlug: project.Slug,
	}

	synced, updatedDeployments, err := s.reconcile(ctx, project, cred)
	if err != nil {
		s.auditFailure(ctx, "registry.credential.save", project.Slug, name, err)
		return nil, err
	}

	s.audit.RecordFromUser(ctx, "registry.credential.save", "", name, "registry_credential", "success", map[string]any{
		"project":             project.Slug,
		"registry_host":       host,
		"synced_namespaces":   synced,
		"updated_deployments": updatedDeployments,
	})

	return &idpv1.SaveRegistryCredentialResponse{
		Credential:         credentialToProto(project.Slug, row, host, synced),
		SyncedNamespaces:   synced,
		UpdatedDeployments: int32(updatedDeployments),
	}, nil
}

// reconcile writes the Secret into every namespace the project owns and
// refreshes imagePullSecrets on existing Deployments there.
//
// The second step is what satisfies "updates without namespace recreation":
// Deployments created before the credential existed carry no pull secret, and
// writing the Secret alone would never reach them.
func (s *Service) reconcile(
	ctx context.Context,
	project *db.Project,
	cred kubernetes.RegistryCredential,
) (synced []string, updatedDeployments int, err error) {
	if err := s.requireK8s(); err != nil {
		return nil, 0, err
	}

	namespaces, listErr := s.namespaces.ListByProject(ctx, project.ID)
	if listErr != nil {
		return nil, 0, connect.NewError(connect.CodeInternal, listErr)
	}

	synced = make([]string, 0, len(namespaces))
	for i := range namespaces {
		ns := namespaces[i].Name
		if secretErr := s.k8s.EnsureRegistrySecret(ctx, ns, cred); secretErr != nil {
			return synced, updatedDeployments, connect.NewError(connect.CodeInternal, secretErr)
		}
		synced = append(synced, ns)

		managed, secretsErr := s.k8s.ListManagedRegistrySecrets(ctx, ns)
		if secretsErr != nil {
			return synced, updatedDeployments, connect.NewError(connect.CodeInternal, secretsErr)
		}
		count, syncErr := s.k8s.SyncImagePullSecrets(ctx, ns, managed)
		if syncErr != nil {
			return synced, updatedDeployments, connect.NewError(connect.CodeInternal, syncErr)
		}
		updatedDeployments += count
	}

	return synced, updatedDeployments, nil
}

// ProjectSlugForNamespace reports which project owns a namespace, or "" when
// it is unattached. Callers use it to scope names — an ingress hostname, for
// instance — so a lookup failure degrades to the empty string rather than
// failing the operation that needed it.
func (s *Service) ProjectSlugForNamespace(ctx context.Context, namespace string) string {
	ns, err := s.namespaces.GetByName(ctx, namespace)
	if err != nil || !ns.ProjectID.Valid {
		return ""
	}
	project, err := s.projects.GetByID(ctx, ns.ProjectID)
	if err != nil {
		return ""
	}
	return project.Slug
}

// EnsureNamespacePullSecrets materialises every credential owned by the
// namespace's project and returns the Secret names to attach as
// imagePullSecrets.
//
// It runs immediately before each deployment rather than relying on the Secret
// already being there. A namespace attached to a project after its credentials
// were saved, or one whose Secret was deleted out-of-band, would otherwise land
// straight in ImagePullBackOff — the exact failure this feature exists to stop.
//
// A namespace with no project returns no secrets and no error: public images
// must keep deploying without any credential configured.
func (s *Service) EnsureNamespacePullSecrets(ctx context.Context, namespace string) ([]string, error) {
	if !s.k8s.Available() {
		return nil, nil
	}

	ns, err := s.namespaces.GetByName(ctx, namespace)
	if err != nil || !ns.ProjectID.Valid {
		return nil, nil
	}

	project, err := s.projects.GetByID(ctx, ns.ProjectID)
	if err != nil {
		return nil, nil
	}

	rows, err := s.creds.List(ctx, ns.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("load registry credentials: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if !s.box.Enabled() {
		return nil, errors.New(
			"project has registry credentials but IDP_ENCRYPTION_KEY is not configured")
	}

	names := make([]string, 0, len(rows))
	for i := range rows {
		row := &rows[i]

		password, decErr := s.box.Decrypt(row.PasswordEncrypted)
		if decErr != nil {
			// Deploying anyway would produce an ImagePullBackOff with no
			// explanation. Fail with one the user can act on.
			return nil, fmt.Errorf(
				"credential %q cannot be decrypted (wrong IDP_ENCRYPTION_KEY?); re-save it", row.Name)
		}

		cred := kubernetes.RegistryCredential{
			Name:        row.Name,
			RegistryURL: row.RegistryUrl,
			Username:    row.Username,
			Password:    password,
			Email:       derefString(row.Email),
			ProjectSlug: project.Slug,
		}
		if ensureErr := s.k8s.EnsureRegistrySecret(ctx, namespace, cred); ensureErr != nil {
			return nil, ensureErr
		}
		names = append(names, kubernetes.RegistrySecretName(row.Name))
	}

	sort.Strings(names)
	return names, nil
}

// List returns credential metadata. Passwords are never included.
func (s *Service) List(ctx context.Context, req *idpv1.ListRegistryCredentialsRequest) (*idpv1.ListRegistryCredentialsResponse, error) {
	_, project, err := s.authorizeProject(ctx, req.ProjectSlug, false)
	if err != nil {
		return nil, err
	}

	rows, err := s.creds.List(ctx, project.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Report where each Secret actually exists rather than where it should, so
	// a namespace that failed to sync is visible in the UI instead of silently
	// assumed healthy.
	presence := s.secretPresence(ctx, project)

	out := make([]*idpv1.RegistryCredential, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		host, hostErr := kubernetes.NormalizeRegistryHost(row.RegistryUrl)
		if hostErr != nil {
			host = row.RegistryUrl
		}
		out = append(out, credentialToProto(project.Slug, row, host, presence[row.Name]))
	}

	return &idpv1.ListRegistryCredentialsResponse{Credentials: out}, nil
}

// secretPresence maps credential name -> namespaces where its Secret exists.
// Cluster errors degrade to an empty map: this is display detail, never a
// reason to fail a read.
func (s *Service) secretPresence(ctx context.Context, project *db.Project) map[string][]string {
	presence := make(map[string][]string)
	if !s.k8s.Available() {
		return presence
	}

	namespaces, err := s.namespaces.ListByProject(ctx, project.ID)
	if err != nil {
		return presence
	}
	for i := range namespaces {
		ns := namespaces[i].Name
		names, listErr := s.k8s.ListManagedRegistrySecrets(ctx, ns)
		if listErr != nil {
			continue
		}
		for _, secretName := range names {
			credName := strings.TrimPrefix(secretName, kubernetes.RegistrySecretPrefix)
			presence[credName] = append(presence[credName], ns)
		}
	}
	return presence
}

// Delete removes a credential and the Secrets rendered from it.
func (s *Service) Delete(ctx context.Context, req *idpv1.DeleteRegistryCredentialRequest) (*idpv1.DeleteRegistryCredentialResponse, error) {
	_, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}

	name, err := validateCredentialName(req.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := s.creds.Get(ctx, project.ID, name); err != nil {
		if errors.Is(err, repository.ErrCredentialNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("registry credential not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Remove the cluster-side copies first. Deleting the database row first
	// would strand Secrets that nothing knows how to clean up.
	if s.k8s != nil {
		namespaces, listErr := s.namespaces.ListByProject(ctx, project.ID)
		if listErr != nil {
			return nil, connect.NewError(connect.CodeInternal, listErr)
		}
		for i := range namespaces {
			ns := namespaces[i].Name
			if delErr := s.k8s.DeleteRegistrySecret(ctx, ns, name); delErr != nil {
				s.auditFailure(ctx, "registry.credential.delete", project.Slug, name, delErr)
				return nil, connect.NewError(connect.CodeInternal, delErr)
			}
			managed, secretsErr := s.k8s.ListManagedRegistrySecrets(ctx, ns)
			if secretsErr != nil {
				return nil, connect.NewError(connect.CodeInternal, secretsErr)
			}
			// Drops the now-dangling reference from every Deployment.
			if _, syncErr := s.k8s.SyncImagePullSecrets(ctx, ns, managed); syncErr != nil {
				return nil, connect.NewError(connect.CodeInternal, syncErr)
			}
		}
	}

	if err := s.creds.Delete(ctx, project.ID, name); err != nil {
		s.auditFailure(ctx, "registry.credential.delete", project.Slug, name, err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.audit.RecordFromUser(ctx, "registry.credential.delete", "", name, "registry_credential", "success",
		map[string]any{"project": project.Slug})
	return &idpv1.DeleteRegistryCredentialResponse{}, nil
}

// TestConnection authenticates against the registry without storing anything.
// An empty password falls back to the stored credential so the UI can re-test
// an existing entry without the user retyping a secret it never received.
func (s *Service) TestConnection(ctx context.Context, req *idpv1.TestRegistryConnectionRequest) (*idpv1.TestRegistryConnectionResponse, error) {
	_, project, err := s.authorizeProject(ctx, req.ProjectSlug, true)
	if err != nil {
		return nil, err
	}

	host, err := kubernetes.NormalizeRegistryHost(req.RegistryUrl)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	username := strings.TrimSpace(req.Username)
	password := req.Password

	if password == "" {
		name, nameErr := validateCredentialName(req.Name)
		if nameErr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("password is required when no saved credential is referenced"))
		}
		row, getErr := s.creds.Get(ctx, project.ID, name)
		if getErr != nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("registry credential not found"))
		}
		if err := s.requireEncryption(); err != nil {
			return nil, err
		}
		stored, decErr := s.box.Decrypt(row.PasswordEncrypted)
		if decErr != nil {
			return nil, connect.NewError(connect.CodeInternal,
				errors.New("stored password could not be decrypted; re-save the credential"))
		}
		password = stored
		if username == "" {
			username = row.Username
		}
	}

	if username == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username is required"))
	}

	// A failed probe is a legitimate answer, not a transport error: the UI
	// renders it as a validation message next to the form.
	if probeErr := s.prober.Probe(ctx, req.RegistryUrl, username, password); probeErr != nil {
		return &idpv1.TestRegistryConnectionResponse{
			Success:      false,
			Message:      probeErr.Error(),
			RegistryHost: host,
		}, nil
	}

	return &idpv1.TestRegistryConnectionResponse{
		Success:      true,
		Message:      fmt.Sprintf("Authenticated with %s as %s", host, username),
		RegistryHost: host,
	}, nil
}

func (s *Service) auditFailure(ctx context.Context, action, projectSlug, name string, err error) {
	s.audit.RecordFromUser(ctx, action, "", name, "registry_credential", "failure", map[string]any{
		"project": projectSlug,
		"error":   err.Error(),
	})
}

func credentialToProto(projectSlug string, row *db.RegistryCredential, host string, namespaces []string) *idpv1.RegistryCredential {
	if namespaces == nil {
		namespaces = []string{}
	}
	return &idpv1.RegistryCredential{
		ProjectSlug:  projectSlug,
		Name:         row.Name,
		RegistryUrl:  row.RegistryUrl,
		RegistryHost: host,
		Username:     row.Username,
		Email:        derefString(row.Email),
		SecretName:   kubernetes.RegistrySecretName(row.Name),
		Namespaces:   namespaces,
		CreatedAt:    timestampString(row.CreatedAt),
		UpdatedAt:    timestampString(row.UpdatedAt),
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timestampString(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
}
