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
