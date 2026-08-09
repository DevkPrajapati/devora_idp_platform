# IDP — Verification & Missing Features Report

**Date:** 2026-08-05
**Commit:** none — the repository has no commits yet (all files untracked)
**Verified on:** Windows 11, Go toolchain + Node 20, no cluster and no database connected

Everything below was produced by actually running the project's own build, vet,
test, typecheck and lint commands, plus reading the code paths involved. Claims
that could not be measured on this machine are marked as such.

---

## 1. Verification results

| Check | Command | Result |
|---|---|---|
| Backend compile | `go build ./...` | **PASS** (exit 0) |
| Backend static analysis | `go vet ./...` | **PASS** (no findings) |
| Backend tests | `go test ./...` | **PASS** — 82 test functions, all green |
| Backend coverage | `go test -cover ./...` | **NOT MEASURABLE HERE** — see note below |
| Frontend production build | `npm run build` | **PASS** — 5.30s, 349.75 kB JS (92.23 kB gzip) |
| Frontend typecheck | `npm run check` | **FAIL — 10 errors, 3 warnings** |
| Frontend tests | `npm run test` | **FAIL — script does not exist** |
| Whole-project test | `make test` | **FAIL — dies at the frontend step** |
| Whole-project lint | `make lint` | **FAIL — no lint script, no linter config** |

**Coverage note.** `go test -cover` could not complete on this machine: Windows
Application Control blocked `covdata.exe`, which also turned three passing
packages into spurious FAILs under the `-cover` flag only. Without the flag all
tests pass. The one figure that got through was `internal/deployment` at
**5.6 %**. Coverage should be re-measured on a machine without that policy.

### Test distribution

82 test functions in 11 files, but they cover only **4 of 24 packages**:

| Tested (4) | Test funcs |
|---|---|
| `internal/kubernetes` | 42 |
| `internal/build` | 19 |
| `internal/pkg/secretbox` | 9 |
| `internal/deployment` | 5 |

**Untested (20)** — including every package where an authorization or data
mistake would be most costly:

`internal/auth` · `internal/config` · `internal/middleware` · `internal/repository` ·
`internal/project` · `internal/namespace` · `internal/registry` · `internal/storage` ·
`internal/audit` · `internal/health` · `internal/cluster` · `internal/server` ·
`internal/logging` · `internal/pkg/convert` · `internal/pkg/pagination` ·
`internal/database` · `cmd/server` · plus generated packages

`internal/auth` having zero tests is the sharpest gap: JWT signature checking,
issuer/audience validation, expiry, and role extraction from Keycloak claims are
all unverified.

---

## 2. Critical — security

### C1. `/apps/{namespace}/{name}` is completely unauthenticated

**`backend/internal/server/server.go:156-158`** registers the app-access handler
directly on the mux, outside `connect.WithInterceptors(authInterceptor)`.
**`backend/internal/kubernetes/appaccess.go:51-80`** then performs no
authentication or authorization of its own.

Consequence: anyone who can reach port 8090 can issue
`GET /apps/<any-namespace>/<any-service>` and the platform will open a
port-forward into that pod and redirect them to it. Namespace and service names
can be brute-forced because the error strings distinguish "service not found",
"service has no ports", and "no running pod found". This bypasses every
namespace boundary the IDP is supposed to enforce, and it works even when
`AUTH_ENABLED=true`.

The webhook endpoint is deliberately unauthenticated too, but that one is
correct — it verifies an HMAC over the raw body in constant time and refuses to
run without a configured secret (`build/webhook.go:63-125`). The app-access
handler has no equivalent.

**Fix:** put the handler behind the same token check as the RPC surface (or
issue short-lived signed URLs from an authenticated RPC), and authorize the
caller against the target namespace.

### C2. Shipped configuration disables authentication entirely

`AUTH_ENABLED` defaults to `false` in **`config/config.go:239`** *and* is set to
`false` in the checked-in **`backend/.env:17`**. When disabled,
**`auth/interceptor.go:20-23`** injects `DevUser()` — a full **admin** — into
every request without looking at any header.

The README lists this as a known gap, but two things make it worse than it
reads. First, it is the default in both places, so the out-of-the-box state is
open. Second, `frontend/.env` sets `VITE_AUTH_ENABLED=true`, so the UI presents
a Keycloak login screen while the API behind it accepts unauthenticated calls —
the deployment *looks* protected and is not.

**Fix:** default `auth.enabled` to `true`; require an explicit opt-out. Refuse
to start when `APP_ENV != development` and auth is off.

### C3. No role enforcement anywhere

`auth.RequireRole` (**`auth/interceptor.go:42-59`**) is defined and never
called — a grep across the whole backend returns only its own definition. Roles
are parsed out of the JWT and stored on the context, then never consulted by any
interceptor.

The result is that the three-tier model the product advertises does not exist at
the transport layer: a `viewer` token is accepted for `DeleteNamespace`,
`DeleteDeployment`, and `SaveRegistryCredential`. `frontend/src/routes/Rbac.svelte`
renders a privilege matrix describing restrictions that nothing enforces, and
`auth.canWrite()` in the frontend store is UI-only — trivially bypassed with curl.

**Fix:** wire `RequireRole` per-procedure (or add a procedure→roles map applied
in the interceptor chain), and add tests asserting a viewer token is rejected on
each mutating RPC.

---

## 3. High

### H1. Port-forward sessions are never reclaimed

`AppAccess.active` (**`appaccess.go:31`**) grows without bound. Entries are
deleted only when `ForwardPorts()` returns (`appaccess.go:114-118`); there is no
idle timeout, no TTL, no maximum, and no cleanup on server shutdown. Every
distinct namespace/service ever opened holds an open SPDY connection and a
listening local port for the process lifetime. Combined with C1 this is
remotely triggerable file-descriptor exhaustion.

### H2. `make test` and `make lint` both fail, and nothing else checks the build

`frontend/package.json` defines only `dev`, `build`, `preview`, and `check`.
`Makefile:68` calls `npm run test` and `Makefile:72` calls `npm run lint`;
neither script exists. `golangci-lint` is invoked with no config file present in
the repo. There is no CI of any kind — no `.github/`, no `.gitlab-ci.yml`, no
`Jenkinsfile` — so the broken targets have nothing to surface them.

### H3. Frontend typecheck fails — 10 errors

All 10 come from `frontend/src/routes/Audit.svelte`, which uses TanStack Query
v6 results with Svelte v4 store syntax (`$auditQuery`). The file is dead code —
`App.svelte:22` imports `AuditLog.svelte` instead — but it still fails
`npm run check`, and `vite build` passes only because it never typechecks. Two
a11y warnings (unassociated `<label>`s at lines 48 and 57) are in the same file.
Deleting it clears all 10 errors.

### H4. No frontend tests and no test tooling

Zero test files under `frontend/src/`, and no vitest, testing-library,
playwright, or cypress in `package.json`. The entire UI — auth store, token
persistence, router, all 14 route pages — is unverified.

---

## 4. Medium — missing features

### M1. No plain-HTTP health endpoint

The only health check is the Connect RPC `/idp.v1.HealthService/Check`, which is
a POST. Kubernetes `httpGet` liveness and readiness probes issue GET, so the
platform cannot be probed by the very orchestrator it manages. Add `GET /healthz`
and `GET /readyz` alongside the RPC.

### M2. No metrics endpoint

No `/metrics`, no Prometheus registry, no OpenTelemetry. Nothing exports request
rates, RPC latency, build durations, or reconciler health.

### M3. Monitoring page charts are hardcoded

`frontend/src/routes/Monitoring.svelte:17-19`:

```js
const cpuHistory     = [22, 28, 35, 41, 39, 45, 52, 48, 44, 47];
const memoryHistory  = [31, 31, 32, 32, 32, 32, 33, 33, 32, 32];
const storageHistory = [18, 18, 18, 18, 19, 19, 19, 19, 19, 19];
```

These literals are rendered under the heading "Real-time telemetry, resource
trends" (line 27). The current-value tiles on the same page *are* live via
`GetOverview`, but every trend line is fabricated. There is no time-series store
behind them, which is really what M2 is blocking.

### M4. Settings page is entirely static

`frontend/src/routes/Settings.svelte:10-16` hardcodes `k8sVersion: 'v1.36.1'`,
`version: '1.0.0'`, and the Keycloak URL. Two of those are wrong or unverifiable:
`APP_VERSION` is `0.1.0`, and the cluster version is never queried. The page
writes nothing — the only functional control is the theme toggle. There is no
backend settings API.

### M5. RBAC is display-only

No RBAC service exists in any `.proto`. `Rbac.svelte` lists "Assign RBAC
policies" as an Admin capability, but there is no endpoint to assign anything.
The page can only echo the claims already in the current token. Related: the
README notes users must be created in the Keycloak admin console — there is no
user-management surface in the IDP at all.

### M6. The platform cannot be deployed

No `Dockerfile` for backend or frontend, no Helm chart, no Kubernetes manifests.
`deploy/` contains only `docker-compose.yml` (Postgres + Keycloak for local dev)
and a Keycloak realm export. `make build-backend` produces a bare host binary.
An IDP that deploys workloads to Kubernetes has no way to deploy itself.

### M7. CORS is hardcoded and unsafe for production

`middleware/cors.go:10` pins `Access-Control-Allow-Origin` to
`http://localhost:5173` with no configuration hook, so any real frontend origin
is blocked. It also sets `Access-Control-Allow-Credentials: true` (line 13),
which makes the fixed origin a correctness requirement, not just an
inconvenience — an allowlist read from config is the right shape here.

### M8. Click-to-open only works on a developer laptop

`appaccess.go:77-78` redirects the browser to `http://127.0.0.1:<port>`, but the
port-forward is bound to the *server's* loopback (`appaccess.go:221`). These are
the same host only when the browser and the backend run on one machine. Once the
backend is deployed anywhere else the feature silently sends users to a dead
port on their own laptop.

### M9. Database port drift

`config/config.go:232` defaults to port **5433**; `Makefile:17` and
`backend/.env:9` both use **5434**. Anyone running the backend without a `.env`
present connects to the wrong port.

### M10. `VITE_AUTH_ENABLED` missing from `frontend/.env.example`

`frontend/.env` sets it, the example file does not, and `auth.ts:20` treats any
value other than the literal `'false'` as enabled. A fresh clone copying the
example gets a login wall with no documented way to turn it off.

### M11. Implemented RPCs never wired to the UI

Present in proto, implemented in the backend, called from nowhere in the
frontend:

- `NamespaceService/SetNamespaceProject` — namespaces cannot be assigned to a
  project from the UI, which appears to leave a hole in the tenancy model
- `NamespaceService/GetNamespace`
- `DeploymentService/GetDeployment`
- `ClusterService/GetPodLogs` (acceptable — `StreamPodLogs` is used instead)

### M12. Fresh clone cannot build without extra tooling

`.gitignore:15,17,58` excludes `backend/internal/gen/`, `*.pb.go`, and
`backend/internal/database/sqlc/`. Generated code is therefore not in the repo,
so a clone must run `buf generate` and `sqlc generate` before `go build`
succeeds. The README does list both as prerequisites, so this is a documented
cost rather than a defect — but with no CI, nothing ever proves the generation
step still works.

---

## 5. Low

- **No `LICENSE` file**, though the README declares "Private — Internal use only".
- **No `CONTRIBUTING.md`**, no API reference doc, no ADRs.
- **No rate limiting** on the webhook endpoint — HMAC-authenticated but unthrottled.
- **No request-ID or trace correlation** in `middleware/logging.go`; logs cannot be
  tied to a single request across services.
- **`tsconfig.app.json:13`** uses `baseUrl`, deprecated and removed in TypeScript 7.
- **Duplicate route files** — `Audit.svelte` (dead) and `AuditLog.svelte` (live).

---

## 6. Secrets check

`backend/.env` contains a live 32-byte `IDP_ENCRYPTION_KEY`, a personal ngrok
hostname, and a macOS kubeconfig path. `frontend/.env` contains no secrets.

**Verified with `git check-ignore`: both `.env` files, `bin/idp-server`, and all
generated code are correctly ignored, and the repository has no commits — so
nothing has leaked.** Still worth rotating that key before this repo is pushed
or shared, since it exists in plaintext in the working tree and any credential
already sealed with it becomes undecryptable once it changes.

One point in the design's favour: when the key is absent the platform disables
registry credential storage outright (`server.go:64-70`) rather than silently
falling back to plaintext. That is the correct behaviour.

---

## 7. What is genuinely solid

Worth stating plainly, because the list above is one-sided:

- Backend compiles clean and passes `go vet` with no findings.
- Webhook signature verification is done properly — mandatory secret,
  constant-time comparison, per-provider handling for GitHub / GitLab /
  Bitbucket (`build/webhook.go:63-125`).
- Clean-architecture layering is consistent across all nine services, with no
  `TODO`, `FIXME`, or stub handler anywhere in the backend.
- `internal/kubernetes` is the best-tested package (42 tests) and covers the
  riskiest translation logic: ingress, probes, rollout, resources, workload config.
- Encryption-key handling degrades safely instead of silently.
- The build reconciler is correctly modelled as a background loop and is
  cancelled before the DB pool closes on shutdown (`server.go:210-220`).
- Config loading has a real bug-fix comment explaining the viper/dotenv bridge
  (`config.go:101-110`) — the kind of context that survives a handover.
- The feature docs in `docs/features/` are unusually good and cover features 1–8
  (3 & 4 are combined in `03-`, 5 & 6 in `05-`, 7 & 8 in `07-`) — no doc gap.

---

## 8. Suggested order of work

**Before this is exposed to anyone else**

1. C1 — authenticate `/apps/*` (highest severity, not previously tracked)
2. C2 — flip `AUTH_ENABLED` to default true; refuse to boot open outside dev
3. C3 — wire `RequireRole` across mutating RPCs, with tests
4. H1 — TTL and cap on port-forward sessions

**To stop regressions**

5. H2 — add vitest + a `test` and `lint` script; add `.golangci.yml`; add CI running
   `go build`, `go vet`, `go test`, `npm run check`, `npm run build`
6. H3 — delete `Audit.svelte` (clears all 10 typecheck errors)
7. Tests for `internal/auth` and `internal/config`

**To make it deployable**

8. M1, M2 — `/healthz`, `/readyz`, `/metrics`
9. M6 — Dockerfiles + Helm chart or manifests
10. M7 — configurable CORS allowlist
11. M9, M10 — fix port drift and `.env.example`

**Product gaps**

12. M3, M4 — real metrics history; make Settings live or remove the fake fields
13. M5 — decide whether RBAC management is in scope, or relabel the page
14. M11 — wire `SetNamespaceProject` into the UI
15. M8 — rethink click-to-open for non-local deployments
