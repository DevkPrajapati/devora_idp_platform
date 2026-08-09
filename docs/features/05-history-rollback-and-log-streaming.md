# Features 5 & 6 — Deployment History/Rollback and Live Log Streaming

---

# Feature 5 — Deployment History & Rollback

## 1. Architecture

```
ListRollouts ──► Deployment ──► ownerReferences ──► ReplicaSets
                                                        │
                          deployment.kubernetes.io/revision
                                                        ▼
                                            Rollout[] (newest first)

RollbackDeployment ──► pick target RS ──► copy spec.template
                                          strip pod-template-hash
                                          stamp kubernetes.io/change-cause
                                                        ▼
                                          Deployment.spec.template
                                                        ▼
                                    controller creates a NEW revision
```

There is no history object in Kubernetes — `kubectl rollout history` reads
exactly these ReplicaSets, and so does this. Nothing is stored platform-side.

## 2. Backend

`internal/kubernetes/rollout.go` — `ListRollouts`, `RollbackDeployment`,
`ownedReplicaSets`, `selectRollbackTarget`.
`internal/deployment/service.go` — authorisation, validation, audit.

**Three details that make the difference between working and subtly broken:**

- **Ownership via `ownerReferences`, not the label selector.** A selector can
  legitimately match ReplicaSets from another Deployment in the same namespace;
  rolling back onto one would swap in a completely unrelated pod template.
- **`pod-template-hash` is stripped** from the restored template. The Deployment
  controller computes that label itself — writing it back stale makes its hash
  disagree with the label, and it churns creating ReplicaSets.
- **ReplicaSets with no revision annotation are skipped.** Treating them as
  revision 0 would let one win the "previous revision" comparison.

## 3. Database schema

**No changes.** Mirroring revision history into Postgres would create a second
copy that drifts from what the cluster will actually roll back to.

## 4. API

| RPC | Auth | Notes |
|---|---|---|
| `ListRollouts` | read | Newest first. Revision, timestamp, image, replicas, status, change cause. |
| `RollbackDeployment` | write | `revision: 0` = undo to previous, matching `kubectl rollout undo`. |

`Rollout.revision` is `int64`, so it arrives as a **JSON string** — the frontend
parses it with `Number()`. Comparing it to a number without that would silently
fail.

Rollback failures map to `FailedPrecondition`, not `Internal`: "revision not
found", "no previous revision" and "already active" are all the caller asking
for something impossible, not server faults.

## 5. Frontend

A **History** button per deployment opens a table of Revision / Timestamp /
Image / Replicas / Status, with **Rollback** on every non-current row. The
change cause renders under the image, so a prior rollback explains itself.

After a rollback the list is refetched rather than patched — the rollback
creates a new revision, so the displayed list is already stale.

## 6. Kubernetes resources

**Created:** none. **Modified:** `Deployment.spec.template` and the
`kubernetes.io/change-cause` annotation. ReplicaSets are read-only here.

## 7. Error handling

| Situation | Behaviour |
|---|---|
| Deployment missing | `NotFound` |
| Revision does not exist | `FailedPrecondition`, naming the revision |
| Rollback with no earlier revision | `FailedPrecondition` |
| Target revision already active | `FailedPrecondition` — a no-op rollout would otherwise look like success |
| Concurrent update during rollback | `RetryOnConflict` |
| No history yet | Empty list plus an explanation, not an error |

## 8. Security

Rollback is a write, gated by the same namespace authorisation as scale and
delete. Audit records the requested and resolved revision plus the resulting
image, so "who reverted production and to what" is answerable.

History exposes image references and replica counts — no configuration values,
because a ReplicaSet's pod template holds `envFrom` references, not values
(see Feature 2).

## 9. Testing

8 tests, passing:

- Explicit revision selection.
- Undo picks the previous revision, from a **deliberately unordered** slice —
  picking the numerically highest, or the first in list order, both fail here.
- Revisions above the current one are skipped (they exist after a prior
  rollback; "rolling back" to one would roll forward).
- Nothing selected when the revision is absent, when there is no earlier
  revision, and when history is empty.
- Unannotated ReplicaSets ignored.
- `pod-template-hash` stripped, `app` label kept, source object not mutated.
- `revisionOf` across valid/missing/nil/garbage/empty.
- `withChangeCause` on nil and populated maps.

Not covered: `ListRollouts`/`RollbackDeployment` against a live API server.

## 10. Example

```yaml
# ReplicaSet the history is derived from
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: backend-api-7d9f4b8c5
  namespace: team-a
  annotations:
    deployment.kubernetes.io/revision: "2"
    kubernetes.io/change-cause: "rollback to revision 1"
  ownerReferences:
    - apiVersion: apps/v1
      kind: Deployment
      name: backend-api
      uid: 3f1c...
```

```bash
# equivalent CLI
kubectl rollout history deployment/backend-api -n team-a
kubectl rollout undo deployment/backend-api -n team-a --to-revision=1
```

---

# Feature 6 — Live Log Streaming

## 1. Architecture

```
kubelet ──► client-go GetLogs(Follow, Timestamps) ──► bufio.Scanner
                                                          │ per line
                                            ParseLogLine  ▼
                              connect.ServerStream.Send(LogLine)
                                                          │
                        [1 flag byte][4-byte BE length][JSON]
                                                          ▼
                                  fetch + ReadableStream reader
                                                          ▼
                                        LogViewer.svelte (async iterator)
```

Lines are sent one at a time, never batched — batching would reintroduce
exactly the delay this replaces.

## 2. Backend

`internal/kubernetes/logstream.go` — `StreamPodLogs`, `ParseLogLine`.
`internal/cluster/{service,handler}.go` — the streaming RPC.

`emit` writes straight to the transport, so a slow client applies backpressure
to the read from Kubernetes rather than growing an unbounded backend buffer.

## 3. Database schema

**No changes.**

## 4. API

`StreamPodLogs(StreamPodLogsRequest) returns (stream LogLine)`.
`GetPodLogs` is kept, marked deprecated, so existing callers keep working.

`LogLine` carries `pod_name` on **every** line, so a client tailing several pods
can attribute lines without tracking which stream each arrived on.

## 5. Frontend

`services/logstream.ts` implements the Connect streaming envelope by hand —
`[1 flag byte][4-byte big-endian length][payload]`, flag `0x02` marking
end-of-stream. This is hand-rolled because the rest of the app talks to Connect
with plain `fetch` JSON POSTs; adding the generated client for one screen would
leave two transports in the codebase.

`components/LogViewer.svelte` delivers all six requirements:

| Requirement | Implementation |
|---|---|
| Real time | async iterator over the stream, one line at a time |
| Auto-scroll | pins to bottom; **releases** when the user scrolls up, with a "Jump to latest" affordance |
| Pause/Resume | stream **stays open**; lines buffer and flush on resume, with a pending count |
| Reconnect | 1s → 2s → 5s → 10s backoff, reset by any successful line |
| Filter by pod | in-viewer pod selector, plus a text filter across lines |
| Timestamps | kubelet-supplied, rendered as local `HH:MM:SS.mmm` |

Pausing deliberately does **not** close the stream — the paused window is
usually the exact thing the user paused to read.

`Workloads.svelte`'s hard-coded `simulatedLogs` array is gone, replaced by the
real viewer.

## 6. Kubernetes resources

None. Read-only against the pod log endpoint.

## 7. Error handling

| Situation | Behaviour |
|---|---|
| Line over 256 KiB | Named error identifying the pod. Unbounded, one container could exhaust backend memory. |
| `tail_lines` 0 or over 5000 | Clamped to 200 / 5000. Zero would request a long-running pod's entire history. |
| Client disconnects | Context cancellation ends the stream quietly — not an error |
| Container exits | Stream closes cleanly; UI shows "container exited" and **stops retrying** |
| Pod not found | `not_found` → UI stops retrying rather than hammering the API |
| Transient drop | Exponential backoff reconnect |
| Line without a timestamp | Passed through whole rather than having its first word eaten |

The UI also caps retained lines at 5000; a container logging hundreds of lines
a second would otherwise grow the DOM until the tab locks up.

## 8. Security

Streaming inherits the cluster service's authorisation. Two resource limits
matter because a stream is long-lived: the per-line cap and the tail clamp.
Log content is rendered as text, never HTML — Svelte escapes interpolations, so
a container echoing `<script>` cannot inject into the console.

Worth stating: **logs frequently contain secrets** that applications print
themselves. This feature moves them from `kubectl` to the browser; it does not
make them safe. Read access should be scoped accordingly.

## 9. Testing

6 tests, passing: timestamp splitting; full message preservation including a
message that itself looks like a timestamp; six shapes of untimestamped line
passed through unchanged; CRLF trimming; `looksLikeTimestamp` across 4 valid
and 5 invalid tokens; and the tail/line-size invariants.

Not covered: the live `StreamPodLogs` loop and the browser-side envelope
parser. The parser is the piece most worth an integration test — its framing
handles split and coalesced chunks, which these unit tests do not exercise.

## 10. Example

Wire frame (one line):

```
0x00 0x00 0x00 0x00 0x7b {"podName":"api-7d9f","timestamp":"2026-07-28T10:15:00.123456789Z","message":"listening on :8080"}
```

End-of-stream frame:

```
0x02 0x00 0x00 0x00 0x02 {}
```

Rendered:

```
10:15:00.123  listening on :8080
```
