# Feature 2 — Secrets & ConfigMaps

Environment variables no longer live inside the Deployment. Non-sensitive values
go to a ConfigMap, sensitive ones to a Secret, and both are injected with
`envFrom`.

## 1. Architecture

```
  Deploy form / Config editor (Svelte)
        │ config_vars          │ secret_vars
        ▼                      ▼
   <app>-config           <app>-secrets
   (ConfigMap)            (Secret, Opaque)
        └──────────┬───────────┘
                   │ envFrom
                   ▼
        Deployment.spec.template.spec.containers[0]
                   │
        pod template annotation
        idp.platform/config-checksum: <sha256>   ← forces the rollout
```

Two objects per workload, named after it, sharing its lifecycle: created before
the Deployment, deleted with it.

**The checksum annotation is load-bearing.** Kubernetes does not restart pods
when a ConfigMap or Secret changes, and `envFrom` is read only at container
start. Without stamping a hash of the configuration onto the pod template, a
save would report success while every running container kept serving the old
values. Changing the annotation changes the pod template, which is what actually
triggers a rolling update.

## 2. Backend

| File | Responsibility |
|------|---------------|
| `internal/kubernetes/workload_config.go` | ConfigMap/Secret ensure, partial secret merge, key validation, checksum, `envFrom` wiring |
| `internal/kubernetes/deployment.go` | `envFrom` instead of inline `env`; config written before the Deployment; rollback on failure |
| `internal/deployment/service.go` | Split, validate, `GetConfig`, `UpdateConfig`, cleanup on delete |

## 3. Database schema

**No changes.** Configuration lives in Kubernetes, which is already the source
of truth for it. Mirroring it into Postgres would create a second copy of every
secret to keep in sync and to leak.

## 4. API

| RPC | Notes |
|-----|-------|
| `CreateDeployment` | `config_vars` + `secret_vars` added. `env_vars` is deprecated but still accepted and merged into `config_vars` (treated as non-sensitive). |
| `GetDeploymentConfig` | Returns config values in full and secret **key names only**. |
| `UpdateDeploymentConfig` | `config_vars` replaces wholesale; `secret_vars` + `removed_secret_keys` patch. Returns `restarted`. |

`Deployment` gained `config_map_name`, `secret_name`, `secret_keys`.

### Why config replaces but secrets patch

The asymmetry is deliberate, and it is the central design decision here.

The client is shown every config value, so it can honestly send the full desired
set and let omission mean deletion. It is **never** shown a secret value — so it
cannot echo one back. A full replace would therefore delete every secret the
user did not retype. Instead the client sends only what it changed, plus an
explicit removal list.

## 5. Frontend

`Deployments.svelte` gained:

- Two labelled sections in the deploy form — **Config Variables** and
  **Secret Variables** (amber, `type="password"`, with a warning that values
  are never shown again).
- A **Config** button per row, badged with the secret count, opening an editor
  that supports add / edit / delete on both kinds.
- Existing secret keys render read-only with a `•••••••• (unchanged)`
  placeholder. Typing replaces the value; leaving it blank keeps it; the X
  queues an explicit deletion, previewed before save.
- The save confirmation states whether pods actually rolled.

## 6. Kubernetes resources

**Created per workload:**

- `ConfigMap` `<app>-config`
- `Secret` `<app>-secrets` (type `Opaque`)

Both labelled `app=<name>`, `idp.platform/managed=true`. Both referenced with
`optional: true` — a hand-deleted object then degrades to missing variables
rather than wedging every pod in `CreateContainerConfigError`.

**Modified:** the container uses `envFrom` and no longer sets `env`; the pod
template carries `idp.platform/config-checksum`.

## 7. Error handling

| Situation | Behaviour |
|-----------|-----------|
| Key is not a C_IDENTIFIER (`has-dash`, `1ABC`) | `InvalidArgument`. Kubernetes would otherwise **silently skip** the variable — the pod starts without it and only an event says why. |
| Same name in both config and secrets | `InvalidArgument`. `envFrom` would resolve it to the Secret silently. |
| Key both set and removed in one request | `InvalidArgument` rather than picking an order. |
| Payload over 512 KiB | `InvalidArgument` naming which kind. The API server's own 1 MiB error does not say which object was too big. |
| Deployment create fails after config was written | ConfigMap and Secret are deleted, so no orphans remain. |
| Concurrent edits | `RetryOnConflict` on every ConfigMap/Secret/Deployment update. |
| Deployment deleted | ConfigMap and Secret deleted too — stale credentials must not outlive the workload. |
| Pre-existing deployment with inline `env` | Still read and displayed, so nothing disappears from the UI; it migrates into the ConfigMap on the next config save. |

## 8. Security

- **The stated goal is met:** `kubectl get deployment -o yaml` shows no values.
  `TestContainerCarriesNoInlineEnv` asserts it.
- **No read path for secret values.** `GetDeploymentConfig` returns key names.
  `DeploymentInfo` carries `SecretKeys`, never values. The only function that
  reads values, `readSecretValues`, is unexported and feeds the checksum alone.
- **Audit logs record key names, not values** — logging `DB_PASSWORD`'s value
  would recreate the exact leak this feature removes.
- **The checksum is one-way.** Secret values feed a SHA-256 digest, so the
  annotation changes on rotation while revealing nothing.
- **`StringData` is set with `Data` cleared** on update. Leaving both populated
  has `StringData` win for overlapping keys while stale `Data` survives for the
  rest — a deleted secret that quietly stays live.

Remaining gap, worth stating plainly: Kubernetes Secrets are base64, not
encrypted, so anyone with `get secrets` in the namespace can still read them.
Closing that needs etcd encryption-at-rest or an external secrets store, which
is a cluster-level decision rather than an application one.

## 9. Testing

`go test ./internal/kubernetes/...` — passing.

- Key validation accepts identifiers and rejects the six shapes that `envFrom`
  silently drops.
- Checksum stability across 50 calls (Go randomises map order — an
  order-sensitive hash would roll pods on every save).
- Checksum changes for config edits, secret edits, and removals; and differs
  when a variable moves between config and secret.
- `envFrom` references the right objects and both are optional.
- Size-limit rejection names the offending kind.

Not covered: live cluster round trips (`EnsureWorkloadConfig`,
`MergeWorkloadSecret`, `ApplyConfigChecksum`). These need
`k8s.io/client-go/kubernetes/fake`; the merge semantics in particular deserve a
test before this ships to a shared cluster.

## 10. Example generated YAML

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: backend-api-config
  namespace: team-a
  labels:
    app: backend-api
    idp.platform/managed: "true"
data:
  NODE_ENV: production
  LOG_LEVEL: info
  API_URL: https://api.example.com
---
apiVersion: v1
kind: Secret
metadata:
  name: backend-api-secrets
  namespace: team-a
  labels:
    app: backend-api
    idp.platform/managed: "true"
type: Opaque
data:
  DB_PASSWORD: <base64>
  JWT_SECRET: <base64>
---
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
      annotations:
        idp.platform/config-checksum: 9f2c1e...
    spec:
      imagePullSecrets:
        - name: idp-registry-dockerhub
      containers:
        - name: backend-api
          image: acme/backend-api:1.4.2
          ports:
            - containerPort: 8080
              protocol: TCP
          envFrom:
            - configMapRef:
                name: backend-api-config
                optional: true
            - secretRef:
                name: backend-api-secrets
                optional: true
```

Note what is **absent** from the Deployment: any `env:` block, and any value
from either object.
