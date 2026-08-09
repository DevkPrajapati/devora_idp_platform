# Features 3 & 4 — Ingress URLs and Health Probes

Delivered together because both extend the same `CreateDeployment` path and the
same pod template.

---

# Feature 3 — Ingress & Real URLs

## 1. Architecture

```
CreateDeployment
      │
      ├── resolveIngressHost ──► custom hostname?  → validate verbatim
      │                          otherwise         → <app>.<scope>.<domain>
      │                                               scope = project slug,
      │                                               else namespace name
      ▼
  EnsureIngress ──► networking.k8s.io/v1 Ingress (class: nginx, path: / Prefix)
      │
      └── Deployment.url = <scheme>://<host>   shown as a link in the UI
```

`scope` falls back to the namespace name when the namespace has no project.
Without that fallback the hostname would contain an empty label
(`api..idp.local`) and the API server would reject the Ingress — an unattached
namespace still deserves a working URL.

## 2. Backend

`internal/kubernetes/ingress.go` — `BuildIngressHost`, `ValidateHostname`,
`EnsureIngress`, `GetIngressHost`, `DeleteIngress`, `IngressConfig`.
`internal/deployment/service.go` — `resolveIngressHost`, creation, deletion.
`IngressConfig` hangs off `kubernetes.Client` because every read that reports a
URL needs the domain and scheme.

## 3. Database schema

**No changes.** The Ingress is the source of truth for a workload's hostname.

## 4. API

`CreateDeploymentRequest` gained `hostname` (override) and `ingress_disabled`.
`Deployment` gained `url` and `ingress_host`.

New configuration:

| Env var | Default | Purpose |
|---|---|---|
| `IDP_INGRESS_ENABLED` | `true` | Off on clusters with no ingress controller |
| `IDP_INGRESS_DOMAIN` | `idp.local` | Hostname suffix |
| `IDP_INGRESS_CLASS` | `nginx` | IngressClass name |
| `IDP_INGRESS_TLS_SECRET` | *(empty)* | Attaches TLS and switches URLs to `https` |

## 5. Frontend

The **Access** column now shows the hostname as a clickable link when an
Ingress exists, falling back to NodePort, then ClusterIP. The deploy form's
**Routing & Health Checks** section takes a custom hostname and an
"internal only" checkbox.

## 6. Kubernetes resources

**Created:** one `Ingress` per workload, named after it, labelled
`app=<name>`, `idp.platform/managed=true`, with a single `/` `Prefix` rule
pointing at the workload's Service. TLS is attached only when a secret is
configured.

**Deleted** with the workload — a surviving Ingress advertises a hostname that
routes to nothing, which reads as a broken app rather than a deleted one.

## 7. Error handling

| Situation | Behaviour |
|---|---|
| Invalid custom hostname | `InvalidArgument` **before** anything is created |
| Hostname with scheme/port/path (`https://x`, `x:8080`) | Rejected with a message naming the problem; the API server's own error never mentions the extra characters |
| App or project name that is not a DNS label | `InvalidArgument` from `BuildIngressHost` |
| Ingress creation fails (no controller, admission webhook) | **Deployment still succeeds.** The failure is audited and the URL is absent. A workload already reachable by ClusterIP/NodePort should not be lost to an ingress problem. |
| Ingress disabled globally or per-deployment | No Ingress, no URL, no error |
| Namespace with no project | Hostname scoped by namespace instead |

## 8. Security

- Hostnames are validated against RFC 1123 before reaching the API server, with
  per-label and 253-character total limits enforced.
- `http` is the honest default: a `.local` domain has no certificate authority
  behind it, so advertising `https` would send every user to a browser warning.
  Setting `IDP_INGRESS_TLS_SECRET` switches both the Ingress and the published
  URL together, so they can never disagree.
- An Ingress publishes a workload to anything that can reach the controller.
  `ingress_disabled` exists so internal services can opt out.

## 9. Testing

7 tests, passing. Hostname construction across all documented shapes; the
no-project fallback; rejection of underscores, dots, leading hyphens and empty
labels; 12 malformed hostnames including the four realistic paste accidents;
label and total length limits; config normalisation and scheme selection.

Not covered: `EnsureIngress` against a live API server (needs `client-go` fake).

## 10. Example generated YAML

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: backend-api
  namespace: team-a
  labels:
    app: backend-api
    idp.platform/managed: "true"
spec:
  ingressClassName: nginx
  rules:
    - host: backend-api.acme.idp.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: backend-api
                port:
                  number: 8080
```

### Local DNS

`.local` hostnames do not resolve on their own. On minikube:

```bash
minikube addons enable ingress
echo "$(minikube ip) backend-api.acme.idp.local" | sudo tee -a /etc/hosts
```

On Windows the hosts file is `C:\Windows\System32\drivers\etc\hosts`. A
wildcard DNS entry or dnsmasq avoids editing it per app.

---

# Feature 4 — Health Probes

## 1. Architecture

Probes are **opt-in**, keyed entirely off whether `path` is set. `BuildProbe`
returns `nil` for an unconfigured probe, which omits the field from the pod spec
rather than sending an empty one.

That default is deliberate: a liveness probe pointed at a path the app does not
serve restarts a healthy container forever. No probe is strictly better than a
wrong probe.

## 2. Backend

`internal/kubernetes/probes.go` — `ProbeSpec`, `Configured`, `Validate`,
`BuildProbe`, `probeToSpec`. Wired into the container in `deployment.go` and
validated in `deployment/service.go`.

## 3. Database schema

**No changes.**

## 4. API

New `Probe` message: `path`, `port`, `initial_delay_seconds`,
`timeout_seconds`, `period_seconds`, `failure_threshold` — exactly the six
requested fields. Added to `CreateDeploymentRequest` and `Deployment`
(as `readiness_probe` / `liveness_probe`).

A workload with no probe returns `null`, not a zeroed message, so the UI can
tell "not configured" from "configured with empty settings".

## 5. Frontend

Both probes appear in the collapsible **Routing & Health Checks** section, each
with the six fields. Placeholders show the default that applies when a box is
left blank (`Timeout (1s)`, `Period (10s)`, `Failure threshold (3)`). The
deployments table shows a `readiness + liveness` badge for workloads that have
them.

`probeOrUndefined` drops a blank probe client-side, so an empty message never
reaches the API.

## 6. Kubernetes resources

**Modified:** `containers[0].readinessProbe` and `.livenessProbe`. Nothing is
created.

## 7. Error handling

| Situation | Behaviour |
|---|---|
| All fields blank | Valid. No probe emitted. |
| Path without a leading `/` | `InvalidArgument` |
| Port outside 1–65535, or any negative value | `InvalidArgument` |
| Timeout longer than period | `InvalidArgument`. Kubernetes rejects this at admission with a message that does not name the probe. |
| Probe port unset | Inherits the container port rather than defaulting to 0 |
| Timings unset | Kubernetes' own defaults (1s / 10s / 3), so the platform adds no hidden behaviour |
| Non-HTTP probe attached by hand | Reported as unconfigured rather than misrepresented as a broken HTTP probe |

Every validation error names which probe failed — with two on one form, an
error that does not say which makes the user guess.

## 8. Security

Minor surface. Probes are unauthenticated GETs from the kubelet to the
container's own port, so a health path must not expose sensitive data — a
documentation concern, not something the platform can enforce. Only
`URISchemeHTTP` is emitted; there is no way to point a probe at an external
host.

## 9. Testing

7 tests, passing. Blank-in/nothing-out across three shapes of "unconfigured";
defaults matching Kubernetes exactly; explicit values carried through; port
inheritance; five invalid inputs including timeout-exceeds-period; the error
naming the probe; round-trip fidelity; and non-HTTP probes not being
misreported.

## 10. Example generated YAML

```yaml
    spec:
      containers:
        - name: backend-api
          image: acme/backend-api:1.4.2
          ports:
            - containerPort: 8080
              protocol: TCP
          envFrom:
            - configMapRef: { name: backend-api-config, optional: true }
            - secretRef:    { name: backend-api-secrets, optional: true }
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
              scheme: HTTP
            initialDelaySeconds: 5
            timeoutSeconds: 1
            periodSeconds: 10
            failureThreshold: 3
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
              scheme: HTTP
            initialDelaySeconds: 15
            timeoutSeconds: 1
            periodSeconds: 10
            failureThreshold: 3
```

With both probes left blank, neither key appears in the pod spec at all.
