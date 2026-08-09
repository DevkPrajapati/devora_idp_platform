# Feature 1 — Registry Credentials

Deploy private images from Docker Hub, GHCR, GitLab, ECR, ACR, GCR or a
self-hosted registry without touching `kubectl`.

## 1. Architecture

A credential lives in three places, and the platform keeps them in step:

```
        Registry page (Svelte)
                 │  SaveRegistryCredential
                 ▼
        registry.Service ──── secretbox (AES-256-GCM)
                 │                    │
                 │                    ▼
                 │        registry_credentials  (Postgres, ciphertext)
                 │
                 ├── EnsureRegistrySecret ──► Secret  kubernetes.io/dockerconfigjson
                 │                             (one per credential, per namespace)
                 └── SyncImagePullSecrets ──► Deployment.spec.template.spec.imagePullSecrets
```

Scoping: credentials belong to a **project**; Secrets are materialised into
**every namespace that project owns**. `namespaces.project_id` existed since
migration 002 but was never populated — this feature wires it up, because
without it a deployment has no path back to its project's credentials.

Resolution on deploy: `namespace → project_id → credentials → ensure Secrets →
attach imagePullSecrets`. A namespace with no project, or a project with no
credentials, deploys exactly as before — public images keep working with
nothing configured.

## 2. Backend

| File | Responsibility |
|------|---------------|
| `internal/pkg/secretbox/` | AES-256-GCM sealing of passwords before storage |
| `internal/repository/registry.go` | CRUD over `registry_credentials` |
| `internal/kubernetes/registry_secret.go` | dockerconfigjson rendering, Secret ensure/delete, `imagePullSecrets` merge + reconcile |
| `internal/registry/service.go` | Authorisation, validation, reconciliation, `EnsureNamespacePullSecrets` |
| `internal/registry/prober.go` | Docker Registry v2 auth handshake for Test Connection |
| `internal/registry/handler.go` | Connect RPC surface |

`registry.Service` satisfies `deployment.PullSecretResolver` and
`namespace.PullSecretResolver`, so those packages depend on an interface rather
than on credential storage.

## 3. Database schema

Migration `003_registry_credentials.sql`:

```sql
CREATE TABLE registry_credentials (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name               TEXT NOT NULL CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
                                            AND length(name) <= 48),
    registry_url       TEXT NOT NULL,
    username           TEXT NOT NULL,
    password_encrypted BYTEA NOT NULL,   -- 0x01 || nonce(12) || ciphertext+tag
    email              TEXT,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);
```

Plus a partial index on `namespaces(project_id) WHERE status != 'deleted'` for
the per-deployment lookup. `ON DELETE CASCADE` means deleting a project drops
its credentials; the Kubernetes Secrets are removed by the delete RPC.

Run: `make migrate-up && make sqlc-gen`.

## 4. API

`idp.v1.RegistryService`:

| RPC | Auth | Notes |
|-----|------|-------|
| `SaveRegistryCredential` | project owner / admin / member with `developer` | Upsert. Reconciles Secrets and existing Deployments. Returns synced namespaces + count of updated Deployments. |
| `ListRegistryCredentials` | any project member | Metadata only. Reports where each Secret **actually** exists. |
| `DeleteRegistryCredential` | write | Deletes Secrets first, then the row. |
| `TestRegistryConnection` | write | Validates without persisting. Empty `password` + a `name` re-tests a saved credential. |

`idp.v1.NamespaceService.SetNamespaceProject` (admin) attaches an existing
namespace to a project and immediately materialises its Secrets.

`Namespace` and `CreateNamespaceRequest` gained `project_slug`.
`Deployment` gained `image_pull_secrets` (names only).

**`RegistryCredential` has no password field.** No RPC returns a stored
password, and passwords are never written to logs or audit details.

## 5. Frontend

- `src/routes/Registry.svelte` — project selector, credential table (with a
  per-credential "which namespaces is this actually in?" badge), add/edit
  modal, Test Connection, delete confirmation.
- `src/services/registry.ts` — typed client + presets for the seven supported
  registry kinds, each with the credential form that registry actually wants
  (e.g. GCR wants `_json_key` + the service-account JSON).
- Route `/registry`, sidebar entry, `App.svelte` branch.

Editing an existing credential leaves the password field blank; blank means
"keep the stored one", and Test Connection still works because the backend
falls back to the stored password server-side.

## 6. Kubernetes resources

**Created:** one `Secret` of type `kubernetes.io/dockerconfigjson` per
credential per namespace, named `idp-registry-<credential-name>`, labelled:

```yaml
idp.platform/managed: "true"
idp.platform/registry-credential: <name>
idp.platform/project: <slug>
```

**Modified:** `Deployment.spec.template.spec.imagePullSecrets` on every
Deployment carrying `idp.platform/managed=true`.

The `idp-registry-` prefix is what makes reconciliation safe: entries with that
prefix are the platform's to add and remove, anything else a user attached by
hand is preserved untouched.

## 7. Error handling

| Situation | Behaviour |
|-----------|-----------|
| No `IDP_ENCRYPTION_KEY` | `FailedPrecondition` with an actionable message. Never stores plaintext. |
| Invalid registry URL | `InvalidArgument` **before** anything is written |
| Project not visible to caller | `NotFound` (not `PermissionDenied`) so the error does not confirm the slug exists |
| Wrong registry password | Test Connection returns `success:false` with HTTP 200 — a rejected login is an answer, not a transport failure |
| Secret name squatted by a non-dockerconfigjson Secret | Deleted and recreated (`type` is immutable) |
| Concurrent credential edits | `RetryOnConflict` around Secret and Deployment updates |
| Cluster unreachable during `List` | Degrades to empty namespace badges; the read still succeeds |
| Credential undecryptable at deploy time | Deployment **fails** with a clear message rather than shipping a workload that will `ImagePullBackOff` |

## 8. Security

- **At rest:** AES-256-GCM, random nonce per write, version-tagged envelope for
  future key rotation. Authenticated, so tampering is detected on read.
- **In transit / on the API:** passwords are write-only; no response, log line,
  or audit `details` blob contains one.
- **SSRF:** Test Connection resolves the target and refuses loopback,
  link-local (blocks the `169.254.169.254` metadata endpoint) and unspecified
  addresses; redirects are re-checked per hop and capped at 3; 10 s timeout;
  response body read is capped at 32 KiB. RFC 1918 is allowed on purpose —
  self-hosted registries live there.
- **Input:** credential names are constrained to RFC 1123 labels in both the
  service and a database `CHECK`, so a name can never escape into an unexpected
  Kubernetes object name.
- **Authorisation:** project viewers can list credential metadata but cannot
  change what the cluster pulls with.

## 9. Testing

`go test ./internal/kubernetes/... ./internal/pkg/secretbox/...` — passing.

- `registry_secret_test.go`: host normalisation across all seven registry
  kinds (including every Docker Hub alias to the legacy v1 key), rejection of
  embedded credentials in URLs, dockerconfigjson shape and `auth` field,
  and `MergeImagePullSecrets` (adds, preserves hand-attached, drops removed,
  idempotent, clears to empty).
- `secretbox_test.go`: round trip, ciphertext never contains plaintext,
  non-determinism across writes, tamper and wrong-key detection, malformed
  envelopes, and the nil-box refusal that prevents plaintext fallback.

Not covered by unit tests: the live cluster paths (`EnsureRegistrySecret`,
`SyncImagePullSecrets`) and the HTTP prober. These need
`k8s.io/client-go/kubernetes/fake` and an `httptest` server respectively —
worth adding next.

## 10. Example generated YAML

Secret written into each namespace of the project:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: idp-registry-dockerhub
  namespace: team-a
  labels:
    idp.platform/managed: "true"
    idp.platform/registry-credential: dockerhub
    idp.platform/project: acme
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: <base64 of the JSON below>
```

```json
{
  "auths": {
    "https://index.docker.io/v1/": {
      "username": "ci-bot",
      "password": "<token>",
      "email": "ci@example.com",
      "auth": "Y2ktYm90Ojx0b2tlbj4="
    }
  }
}
```

Resulting Deployment (abridged):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-api
  namespace: team-a
  labels:
    app: backend-api
    idp.platform/managed: "true"
spec:
  replicas: 2
  selector:
    matchLabels:
      app: backend-api
  template:
    metadata:
      labels:
        app: backend-api
        idp.platform/managed: "true"
    spec:
      imagePullSecrets:
        - name: idp-registry-dockerhub
      containers:
        - name: backend-api
          image: acme/backend-api:1.4.2
          ports:
            - containerPort: 8080
              protocol: TCP
```

## Setup

```bash
# 1. Generate and set the encryption key
openssl rand -base64 32          # paste into backend/.env as IDP_ENCRYPTION_KEY

# 2. Apply the migration and regenerate
make migrate-up
make sqlc-gen
make proto-gen

# 3. Attach namespaces to a project (once, for pre-existing namespaces)
#    via NamespaceService.SetNamespaceProject, or create new namespaces with
#    project_slug set.
```

Without step 3 a credential saves successfully but syncs to zero namespaces —
the UI says so explicitly rather than reporting a misleading success.
