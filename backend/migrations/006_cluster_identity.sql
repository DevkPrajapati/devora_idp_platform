-- +goose Up
-- +goose StatementBegin

-- Pin every fleet row to the identity of the cluster it actually describes.
--
-- Before this column the only durable handle on a cluster was its name and
-- server URL, and neither survives a local cluster's lifecycle: `minikube
-- delete && minikube start` reuses the profile name and hands out a fresh host
-- port, so the platform could not tell "the same cluster came back" from "a
-- brand new empty cluster now answers on this name". It chose the first, and
-- kept serving namespace and workload records belonging to a cluster that no
-- longer existed.
--
-- cluster_uid holds the UID of the cluster's kube-system namespace, which the
-- control plane creates once at bootstrap and never recreates. A mismatch on
-- reconnect is proof the cluster was rebuilt.
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS cluster_uid TEXT;

-- Namespaces are the platform's largest body of cluster-derived state, so they
-- carry the identity of the cluster they were provisioned into. Rows whose
-- cluster_uid no longer matches the live cluster describe something that was
-- destroyed with the old cluster and must not be presented as live.
--
-- Existing rows stay NULL: their provenance is genuinely unknown, and NULL is
-- reconciled against the live cluster on the next read rather than guessed at
-- here, where a wrong guess would either hide real namespaces or resurrect
-- dead ones.
ALTER TABLE namespaces ADD COLUMN IF NOT EXISTS cluster_uid TEXT;

CREATE INDEX IF NOT EXISTS idx_namespaces_cluster_uid ON namespaces (cluster_uid);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_namespaces_cluster_uid;
ALTER TABLE namespaces DROP COLUMN IF EXISTS cluster_uid;
ALTER TABLE clusters DROP COLUMN IF EXISTS cluster_uid;
-- +goose StatementEnd
