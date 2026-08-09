# Internal Developer Platform

A production-grade, self-service multi-tenant Kubernetes Internal Developer Platform (IDP).

## Architecture

```
┌─────────────┐     Connect RPC      ┌─────────────┐     client-go     ┌─────────────┐
│   Svelte    │ ◄──────────────────► │  Go Backend │ ◄───────────────► │ Kubernetes  │
│  Frontend   │      (JWT Auth)      │  (Clean Arch)│                   │   Cluster   │
└─────────────┘                      └──────┬──────┘                   └─────────────┘
                                            │
                                     ┌──────▼──────┐
                                     │ PostgreSQL  │
                                     │  (Metadata) │
                                     └─────────────┘
```

### Backend (Clean Architecture)

| Layer | Responsibility |
|-------|---------------|
| `handler/` | Connect RPC request/response mapping |
| `service/` | Business logic, orchestration |
| `repository/` | Database access via sqlc |
| `kubernetes/` | client-go resource operations |
| `middleware/` | Logging, recovery, CORS, auth |
| `auth/` | JWT validation, RBAC context |

### Frontend

| Directory | Responsibility |
|-----------|---------------|
| `components/` | Reusable UI components |
| `layouts/` | Page layout shells |
| `routes/` | Feature pages |
| `services/` | Connect RPC clients |
| `stores/` | Svelte stores (theme, auth) |
| `types/` | TypeScript type definitions |

## Prerequisites

- Go 1.22+
- Node.js 20+
- Docker & Docker Compose
- protoc (for code generation)
- goose (`go install github.com/pressly/goose/v3/cmd/goose@latest`)
- sqlc (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`)

## Quick Start

```bash
# 1. Start dependencies (PostgreSQL + Keycloak)
make dev-up

# 2. Run database migrations
make migrate-up

# 3. Start backend
make backend-run

# 4. Start frontend (separate terminal)
make frontend-run
```

- Frontend: http://localhost:5173
- Backend:  http://localhost:8090
- Keycloak: http://localhost:8080 (admin/admin)
- Adminer (Postgres UI): http://localhost:8081

### Browse / edit IDP database (Adminer)

`make dev-up` also starts **Adminer** — a full SQL UI over the Docker Postgres
database (view, insert, update, delete, run SQL).

```bash
make dev-up
make db-ui   # prints login fields
```

Open http://localhost:8081 and sign in with:

| Field | Value |
|-------|-------|
| System | PostgreSQL |
| Server | `postgres` |
| Username | `idp` |
| Password | `idp_dev_password` |
| Database | `idp` |

IDP platform tables: `projects`, `project_members`, `namespaces`,
`registry_credentials`, `git_repositories`, `builds`, `audit_logs`.
Keycloak shares this same database — do not edit its tables unless you know
what you are doing.

### Default Users

| Username | Password | Role |
|----------|----------|------|
| admin | admin | Admin |
| developer | developer | Developer |
| viewer | viewer | Viewer |

## Development Commands

```bash
make help           # Show all commands
make proto-gen      # Generate protobuf code
make sqlc-gen       # Generate sqlc queries
make migrate-up     # Apply migrations
make test           # Run tests
make build-backend  # Build backend binary
make build-frontend # Build frontend
```

## Project Status

**Implemented**

- [x] Clean-architecture backend skeleton, Docker Compose (PostgreSQL + Keycloak)
- [x] Database migrations (audit logs, namespaces, projects, registry credentials, git builds)
- [x] Connect RPC services: health, audit, namespace, deployment, cluster, project, registry, build, storage
- [x] Kubernetes integration via client-go (workloads, ingress, probes, rollout history, log streaming)
- [x] Git build-and-deploy pipeline (Kaniko, webhook-triggered, background reconciler)
- [x] Keycloak JWT validation middleware with JWKS caching
- [x] Frontend shell with dark/light theme, fully responsive down to mobile
- [x] Dashboard with cluster meters, activity trend, and event distribution charts
- [x] PVC Storage page (claims, volumes, storage classes, per-node container runtime)

- [x] Role-based authorization enforced at the transport layer for all 47 RPCs
- [x] Signed short-lived tickets gating browser access to running workloads
- [x] `/healthz` and `/readyz` probes, container images, and Kubernetes manifests
- [x] CI (build, vet, lint, test, secret scan); `golangci-lint` clean

## Deployment

```bash
# Build images (context is the repo root for both)
docker build -f backend/Dockerfile  -t idp-backend:dev  .
docker build -f frontend/Dockerfile -t idp-frontend:dev .

# Edit the placeholder Secret first, then:
kubectl apply -f deploy/k8s/idp.yaml
```

`deploy/k8s/idp.yaml` runs both services non-root on a read-only root
filesystem with a least-privilege ClusterRole. Liveness probes `/healthz`
(process only) and readiness probes `/readyz` (checks dependencies) — using the
dependency check for liveness would restart pods during a database outage
instead of just pulling them from the load balancer.

**Security note.** `IDP_ENCRYPTION_KEY` must be identical across replicas. It
seals registry credentials at rest *and* signs app-access tickets, so mismatched
keys mean a ticket minted by one pod is rejected by another.

**Auth is on by default.** `AUTH_ENABLED` now defaults to `true`, and the server
**refuses to start** if it is `false` while `APP_ENV` is anything other than
`development`.

**Known gaps**

1. User accounts are created in the Keycloak admin console, not from the IDP UI.
   There is no RBAC-assignment API; `Rbac.svelte` only reflects existing token
   claims.
2. `Settings.svelte` is read-only. It now reports live values, but there is no
   API for *changing* platform settings.
3. Resource history is an in-memory ring buffer (~1 hour, lost on restart) and
   covers CPU and memory only — storage is shown as a current-value meter
   because nothing samples it over time. Longer retention belongs in Prometheus.
4. Click-to-open redirects to `127.0.0.1`, so it only works when the browser and
   the backend share a host.
5. Only the Namespaces dialogs use the accessible `Modal` component. Fourteen
   hand-rolled `fixed inset-0` dialogs remain across Builds, Deployments,
   Projects, Registry, Workloads and AuditLog — they lack focus trapping,
   `role="dialog"`, and Escape-to-close. `Namespaces.svelte` is the reference
   for migrating them.
6. Empty and error states are still styled per-page rather than shared.

## UI conventions

- **Notifications** — `toasts.success/error/info` from `$stores/toast`, rendered
  by the single `Toaster` mounted in `AppLayout`. Errors persist until
  dismissed; successes auto-clear after 4s. Use `toastError(err, fallback)` to
  narrow an unknown throw, since `String(err)` on a non-Error yields
  "[object Object]".
- **Dialogs** — `$components/ui/Modal.svelte`. Handles focus trapping, focus
  restore, Escape, scroll lock and `aria-modal`. Destructive actions should
  additionally require a typed confirmation.
- **Colour** — `--primary` (emerald) is brand only. `--success` is a different
  green reserved for healthy status, and `--destructive` stays red. Never
  recolour a destructive control to match the brand.

## Observability

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness — process only, no dependency checks |
| `GET /readyz` | Readiness — reports 503 when a dependency is unhealthy |
| `GET /metrics` | Prometheus scrape (unauthenticated; not routed by the Ingress) |

Every response carries `X-Request-Id`, reusing an upstream proxy's value when
present, and the same ID is stamped on the structured log line. A user reporting
a failure can quote the header and it will `grep` straight to the request.

Metric route labels are deliberately collapsed (`/apps/*`, `/webhooks/git/*`) —
labelling by raw path would mint a new time series per namespace and workload,
which is how a Prometheus server runs out of memory.

## License

Private — Internal use only.
