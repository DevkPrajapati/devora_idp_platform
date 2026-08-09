# Features 7 & 8 — Deployment Templates and Git-Based Build & Deploy

---

# Feature 7 — Deployment Templates (Golden Paths)

## 1. Architecture

Templates are **static server-side data** in `internal/deployment/templates.go`,
not rows in a table. A template is a platform-team decision, not tenant data —
storing them per project would let each team drift into its own defaults, which
is precisely the problem golden paths exist to solve. Changing one is a code
review.

Selecting a template prefills the deploy form; the developer supplies image,
name and version. `template_id` is recorded as a label on the Deployment
(`idp.platform/template`) for traceability.

This feature also added **resource limits**, which the platform previously left
empty on every container.

## 2. Backend

`internal/deployment/templates.go` — the catalogue and `Templates()`.
`internal/kubernetes/resources.go` — `ResourceSpec`, validation, `BuildResourceRequirements`.

`Templates()` rebuilds every message per call. Handing out pointers into
package-level state would let one request's mutation corrupt the catalogue for
every later one — `TestTemplatesReturnsIndependentCopies` covers it.

## 3. Database schema

**No changes.**

## 4. API

`ListDeploymentTemplates` (authenticated, no project scope — templates are the
same for everyone). `CreateDeploymentRequest` gained `resources` and
`template_id`; `Deployment` gained `resources`.

## 5. Frontend

A template picker at the top of the deploy form, with the rationale, example
image and required secret names shown for the selection. Applying a template
opens the Routing & Health Checks section rather than hiding what it changed.

Name and image are deliberately **not** overwritten — clobbering a half-typed
name while someone browses templates would be hostile.

## 6. Kubernetes resources

**Modified:** `containers[].resources.requests/limits`, and the
`idp.platform/template` label.

Unset resource fields are left **absent**, not zero: a zero CPU limit means
"unlimited" to Kubernetes, and a zero memory request schedules the pod anywhere
regardless of need. Absent lets a namespace LimitRange apply.

## 7. Error handling

| Situation | Behaviour |
|---|---|
| Malformed quantity (`512 MB`, `lots`) | `InvalidArgument` naming the field |
| Negative quantity | `InvalidArgument` |
| Request above limit | `InvalidArgument`. Kubernetes rejects this only at admission, without saying which resource. |
| No resources given | Valid — nothing is set on the container |

## 8. Security

Templates prefill secret **names** and never values. A default credential
shipped in a template would be deployed unchanged by every team that used it;
`TestTemplatesSuggestSecretNamesWithoutValues` guards against one being added.

## 9. Testing

12 tests. Every template is validated as if it were user input — quantities
parse, probes validate, config keys are C_IDENTIFIERs. Plus: the five required
stacks are present, copies are independent, and memory request equals limit
across the catalogue.

## 10. The catalogue

| Template | Port | CPU (req/lim) | Memory | Notable default |
|---|---|---|---|---|
| Node.js API | 3000 | 100m / 500m | 256Mi | Single-threaded — replicas, not cores |
| React App | 80 | 50m / 200m | 128Mi | **No liveness probe** — nginx serving static files has no state a restart fixes |
| Go API | 8080 | 100m / 1000m | 128Mi | Short probe delays; binaries start in ms |
| Python FastAPI | 8000 | 200m / 1000m | 512Mi | `PYTHONUNBUFFERED=1` — CPython buffers stdout to a pipe, making logs arrive in bursts |
| Spring Boot | 8080 | 500m / 2000m | 1Gi | `MaxRAMPercentage=75` — otherwise the JVM sizes its heap from *node* memory and gets OOM-killed |

Two rules run through all of them: **memory request equals limit** (memory is
incompressible, so a lower request only buys scheduling onto a node that cannot
honour it), and **CPU limits are loose** (CPU is compressible; a tight limit
costs startup latency for no safety gain).

---

# Feature 8 — Git-Based Build & Deploy

## 1. Architecture

```
  push ──► POST /webhooks/git/{id}          "Build now" (Connect RPC)
              │ HMAC verify                        │
              ▼                                    ▼
           builds row (pending) ◄──────────────────┘
              │
              ▼
     Kaniko Job in idp-builds
       ├── git context, token from a Secret
       ├── /kaniko/.docker/config.json  ← copied registry credential (F1)
       └── --destination=<repo>:<branch>-<sha7>
              │
        reconciler (10s poll)
              │  succeeded?
              ▼
     SetDeploymentImage(target) ──► rolling update
```

**Kaniko, not docker-in-docker.** DinD needs a privileged container, which is a
far larger blast radius for a build that executes arbitrary repository code.

**Polling, not watching.** A build takes minutes; a short poll is simpler than a
watch that must be re-established after every disconnect, and it recovers by
itself after a backend restart — the Jobs keep running and are picked up again.

**The platform holds no build state beyond the `builds` table.** Job status is
the source of truth.

## 2. Backend

| File | Responsibility |
|---|---|
| `internal/build/service.go` | Repository CRUD, trigger, retry, reconcile, deploy |
| `internal/build/webhook.go` | Provider signature verification and payload parsing |
| `internal/build/handler.go` | Connect RPCs + the plain HTTP webhook route |
| `internal/kubernetes/buildjob.go` | Kaniko Job spec, status, `SetDeploymentImage` |
| `internal/repository/build.go` | Persistence |

## 3. Database schema

Migration `004_git_builds.sql`:

- **`git_repositories`** — project-scoped, with `token_encrypted` and
  `webhook_secret_encrypted` (AES-256-GCM via `secretbox`, reusing F1's key).
  A `CHECK` enforces that `auto_deploy` has a target, since auto-deploy without
  one would silently do nothing.
- **`builds`** — one row per attempt. `number` is assigned **inside the insert**
  (`COALESCE(MAX(number),0)+1`) so two concurrent triggers cannot claim the
  same number. `retry_of` links a retry to its original.

`registry_credential` is deliberately **not** a foreign key: a credential can be
renamed or recreated without orphaning the repository, and the build reports a
clear error if it is missing at build time.

## 4. API

`idp.v1.BuildService`: `SaveGitRepository`, `ListGitRepositories`,
`DeleteGitRepository`, `TriggerBuild`, `RetryBuild`, `ListBuilds`.

**Webhooks are not a Connect RPC.** Git providers post their own payload shapes
with their own headers and cannot speak Connect, so they are served by
`POST /webhooks/git/{repository_id}` — mounted *outside* the auth interceptor,
which is exactly why HMAC verification is mandatory.

New configuration: `IDP_BUILD_ENABLED`, `IDP_BUILD_NAMESPACE`,
`IDP_BUILD_KANIKO_IMAGE`, `IDP_BUILD_POLL_INTERVAL`, `IDP_PUBLIC_URL`.

## 5. Frontend

`routes/Builds.svelte` — repository connection form, webhook URL with copy
button, "Build now", and a build table showing number, branch/commit, image tag,
status, trigger, duration, with **Logs** and **Retry** per row.

Build logs reuse F6's `LogViewer` against the build namespace — the same
streaming component, pointed at the Job's pod.

The list polls only while a build is active, then stops.

## 6. Kubernetes resources

**Created:** the `idp-builds` namespace; a `Job` per build
(`backoffLimit: 0`, `ttlSecondsAfterFinished: 3600`,
`activeDeadlineSeconds: 1800`); a short-lived `Secret` holding the clone URL
with its token; a copy of the registry credential Secret.

**Modified:** the target `Deployment`'s image on auto-deploy.

## 7. Error handling

| Situation | Behaviour |
|---|---|
| Invalid webhook signature | 401, generic message. Logged with the remote address. |
| Unknown repository id | 404, same shape as a signature failure, so probing cannot enumerate ids |
| Non-push event, tag push, branch deletion, or a non-default branch | **200 "ignored"**. A non-2xx would make the provider retry and eventually disable the webhook. |
| Registry credential missing | Build fails immediately with a message naming the credential |
| Undecryptable git token | Build fails with "re-save the repository" |
| Job creation fails | Build marked failed rather than left pending forever |
| Job garbage-collected before the reconciler sees it | Marked failed with "build job no longer exists" |
| Build times out | `activeDeadlineSeconds` fails the Job; the reconciler records it |
| Auto-deploy fails | Build stays **succeeded** — the image was built and pushed, which is what the build promised. The deploy failure is audited separately. |
| Backend restarts mid-build | Jobs keep running; the reconciler resumes on the next tick |

## 8. Security

This is the largest attack surface in the platform, because a build executes
repository code.

- **Webhook authentication is mandatory.** A repository with no configured
  secret rejects every delivery. Treating "no secret" as "no verification" would
  make the endpoint remote code execution for anyone who learns a repository id.
- **Constant-time comparison** via `hmac.Equal` for both the HMAC path and
  GitLab's plain token, so neither leaks a byte at a time through timing.
- **Signature is verified before parsing.** An unauthenticated caller never
  reaches any payload-parsing code.
- **The git token never appears in the Job spec.** It is written to a Secret and
  injected as `$(CLONE_URL)`, expanded by the kubelet — `kubectl get job -o yaml`
  shows the variable reference, not the token. The Secret is deleted with the Job.
- **Clone URLs with embedded credentials are rejected** at save time; they would
  otherwise be stored in the clear.
- **Builds are namespace-isolated** in `idp-builds`, so a build pod cannot read
  application Secrets.
- **Build resources are bounded** (500m–2 CPU, 1–4Gi). A build compiles
  untrusted code; unbounded, one repository can starve the node.
- **Only the configured branch auto-builds.** Building every branch would let
  any pushed branch deploy to the target environment.
- **Bodies are capped at 1 MiB.**

Known gaps worth stating: a repository's Dockerfile runs with the build Job's
service account, so cluster RBAC for that namespace should be minimal; and
Kaniko's `--cache=true` shares a cache layer across builds of the same
repository, which is fine within a project but should not be pointed at a cache
repository shared across tenants.

## 9. Testing

19 tests across `internal/build`, all passing.

**Webhook security** (8 forgery cases): missing signature, wrong secret,
malformed hex, a valid signature over a *different* body, wrong GitLab token,
and — separately — that every provider refuses a repository with no secret
configured.

**Payload parsing:** GitHub/GitLab/Bitbucket push shapes; branch deletion in all
four forms providers use; ping and tag-push ignored; tag refs yielding no branch.

**Tag and name generation:** 11 branch shapes (slashes, unicode, leading
separators, 340 characters, empty) all produce valid Docker tags; job names stay
within the 63-character DNS label limit and remain unique after truncation.

**Clone URL handling:** token injection, and rejection of SSH URLs and embedded
credentials.

Not covered: the live Job lifecycle (`CreateBuildJob`, `GetBuildJobStatus`,
`Reconcile`) and `SetDeploymentImage`. These need `client-go/kubernetes/fake`
and are the largest remaining test gap in the platform.

## 10. Example generated YAML

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: build-api-7
  namespace: idp-builds
  labels:
    idp.platform/managed: "true"
    idp.platform/build-id: 3f1c8e2a-...
    app: idp-build
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 3600
  activeDeadlineSeconds: 1800
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: kaniko
          image: gcr.io/kaniko-project/executor:v1.23.2
          args:
            - --context=git://$(CLONE_URL)#refs/heads/main
            - --dockerfile=Dockerfile
            - --destination=ghcr.io/acme/api:main-abc1234
            - --cache=true
            - --verbosity=info
          env:
            - name: CLONE_URL
              valueFrom:
                secretKeyRef:
                  name: build-api-7-git      # token lives here, not in the spec
                  key: clone-url
          volumeMounts:
            - name: docker-config
              mountPath: /kaniko/.docker
              readOnly: true
          resources:
            requests: { cpu: 500m, memory: 1Gi }
            limits:   { cpu: "2",  memory: 4Gi }
      volumes:
        - name: docker-config
          secret:
            secretName: idp-registry-ghcr
            items:
              - key: .dockerconfigjson       # kaniko reads config.json
                path: config.json
```

## Setup

```bash
# 1. A registry credential must exist first (Feature 1) — it is the push
#    credential, and the build fails without it.
# 2. Set IDP_PUBLIC_URL so the webhook URL renders correctly.
# 3. make migrate-up
# 4. Connect a repository on the Builds page, with a webhook secret.
# 5. Paste the shown webhook URL into the provider:
#      GitHub:    Settings > Webhooks > application/json + secret
#      GitLab:    Settings > Webhooks > Secret token
#      Bitbucket: Repository settings > Webhooks
```

Without a webhook secret the repository still works for manual builds; the UI
says so explicitly rather than letting pushes fail silently.
