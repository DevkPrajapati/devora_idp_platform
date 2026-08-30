import { apiFetch } from './client';

export interface Namespace {
  id: string;
  name: string;
  displayName: string;
  description: string;
  ownerId: string;
  ownerEmail: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  status: string;
  createdAt: string;
  updatedAt: string;
  /** Slug of the owning project; empty or absent when unattached. */
  projectSlug?: string;
  /**
   * Whether the namespace still exists in the connected cluster.
   *
   * Only meaningful when `clusterChecked` is true. A registered namespace can
   * disappear from the cluster without the platform being told — deleted with
   * kubectl, or lost with a cluster that was rebuilt — and every drill-down on
   * such a row can only fail, so it is marked rather than presented as healthy.
   */
  existsInCluster?: boolean;
  /** Whether presence could be determined at all. False when disconnected. */
  clusterChecked?: boolean;
}

/**
 * Reports a namespace whose platform record no longer matches the cluster.
 * Distinct from "unknown": with no cluster connected nothing can be verified,
 * and claiming the namespace is missing would be wrong.
 */
export function isMissingFromCluster(ns: Namespace): boolean {
  return ns.clusterChecked === true && ns.existsInCluster !== true;
}

export interface ListNamespacesResponse {
  namespaces: Namespace[];
  pageInfo?: {
    page: number;
    pageSize: number;
    totalCount: string;
    totalPages: number;
  };
}

export async function listNamespaces(page = 1, pageSize = 20, status = ''): Promise<ListNamespacesResponse> {
  const response = await apiFetch('/idp.v1.NamespaceService/ListNamespaces', {
    page: { page, pageSize },
    status,
  });

  if (!response.ok) {
    throw new Error(`Failed to list namespaces: ${response.statusText}`);
  }

  return response.json();
}

export async function createNamespace(
  name: string,
  displayName: string,
  description: string,
  labels: Record<string, string> = {},
  annotations: Record<string, string> = {}
): Promise<{ namespace: Namespace }> {
  const response = await apiFetch('/idp.v1.NamespaceService/CreateNamespace', {
    name,
    displayName,
    description,
    labels,
    annotations,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to create namespace: ${response.statusText}`);
  }

  return response.json();
}

/**
 * Assigns a namespace to a project, or detaches it when projectSlug is empty.
 *
 * Moving a namespace re-syncs the target project's registry credentials into
 * it, which is why the response reports which Secrets were written — the caller
 * should surface that rather than treat this as a silent metadata update.
 */
export async function setNamespaceProject(
  name: string,
  projectSlug: string,
): Promise<{ namespace: Namespace; syncedRegistrySecrets?: string[] }> {
  const response = await apiFetch('/idp.v1.NamespaceService/SetNamespaceProject', {
    name,
    projectSlug,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to update namespace project: ${response.statusText}`);
  }

  return response.json();
}

export async function deleteNamespace(name: string): Promise<void> {
  const response = await apiFetch('/idp.v1.NamespaceService/DeleteNamespace', { name });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to delete namespace: ${response.statusText}`);
  }
}
