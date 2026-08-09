import { apiFetch } from './client';

/**
 * A stored registry credential. There is deliberately no `password` field:
 * the backend never returns stored passwords, so the UI cannot leak one it
 * was not given.
 */
export interface RegistryCredential {
  projectSlug: string;
  name: string;
  /** Registry as the user typed it, e.g. "ghcr.io". */
  registryUrl: string;
  /** Auth key written into .dockerconfigjson; differs for Docker Hub. */
  registryHost: string;
  username: string;
  email: string;
  /** Kubernetes Secret rendered from this credential. */
  secretName: string;
  /** Namespaces where the Secret currently exists. */
  namespaces: string[];
  createdAt: string;
  updatedAt: string;
}

export interface SaveRegistryCredentialResult {
  credential: RegistryCredential;
  syncedNamespaces: string[];
  updatedDeployments: number;
}

export interface TestConnectionResult {
  success: boolean;
  message: string;
  registryHost: string;
}

/**
 * Connect returns errors as `{code, message}` with a non-2xx status. Surfacing
 * `message` keeps backend validation text (bad registry host, rejected
 * password) in front of the user instead of a generic failure.
 */
async function readError(response: Response, fallback: string): Promise<string> {
  const data = await response.json().catch(() => ({}) as any);
  return data?.message || `${fallback}: ${response.statusText}`;
}

function toCredential(raw: any): RegistryCredential {
  return {
    projectSlug: raw.projectSlug || '',
    name: raw.name || '',
    registryUrl: raw.registryUrl || '',
    registryHost: raw.registryHost || '',
    username: raw.username || '',
    email: raw.email || '',
    secretName: raw.secretName || '',
    namespaces: raw.namespaces || [],
    createdAt: raw.createdAt || '',
    updatedAt: raw.updatedAt || '',
  };
}

export async function listRegistryCredentials(projectSlug: string): Promise<RegistryCredential[]> {
  const response = await apiFetch('/idp.v1.RegistryService/ListRegistryCredentials', { projectSlug });
  if (!response.ok) {
    throw new Error(await readError(response, 'Failed to list registry credentials'));
  }
  const data = await response.json();
  return (data.credentials || []).map(toCredential);
}

export async function saveRegistryCredential(input: {
  projectSlug: string;
  name: string;
  registryUrl: string;
  username: string;
  password: string;
  email?: string;
}): Promise<SaveRegistryCredentialResult> {
  const response = await apiFetch('/idp.v1.RegistryService/SaveRegistryCredential', {
    projectSlug: input.projectSlug,
    name: input.name,
    registryUrl: input.registryUrl,
    username: input.username,
    password: input.password,
    email: input.email ?? '',
  });
  if (!response.ok) {
    throw new Error(await readError(response, 'Failed to save registry credential'));
  }
  const data = await response.json();
  return {
    credential: toCredential(data.credential || {}),
    syncedNamespaces: data.syncedNamespaces || [],
    updatedDeployments: data.updatedDeployments ?? 0,
  };
}

export async function deleteRegistryCredential(projectSlug: string, name: string): Promise<void> {
  const response = await apiFetch('/idp.v1.RegistryService/DeleteRegistryCredential', { projectSlug, name });
  if (!response.ok) {
    throw new Error(await readError(response, 'Failed to delete registry credential'));
  }
}

/**
 * Validates credentials against the registry without saving. Leaving
 * `password` empty tests an already-saved credential by `name`, since the UI
 * never holds the stored password.
 *
 * A rejected login comes back as `success: false` with a 200, not an error:
 * "wrong password" is an answer, not a transport failure.
 */
export async function testRegistryConnection(input: {
  projectSlug: string;
  registryUrl: string;
  username: string;
  password?: string;
  name?: string;
}): Promise<TestConnectionResult> {
  const response = await apiFetch('/idp.v1.RegistryService/TestRegistryConnection', {
    projectSlug: input.projectSlug,
    registryUrl: input.registryUrl,
    username: input.username,
    password: input.password ?? '',
    name: input.name ?? '',
  });
  if (!response.ok) {
    throw new Error(await readError(response, 'Failed to test registry connection'));
  }
  const data = await response.json();
  return {
    success: data.success ?? false,
    message: data.message || '',
    registryHost: data.registryHost || '',
  };
}

/** Presets for the registries the platform is expected to talk to. */
export const REGISTRY_PRESETS: ReadonlyArray<{ label: string; url: string; hint: string }> = [
  { label: 'Docker Hub', url: 'docker.io', hint: 'Use an access token, not your account password' },
  { label: 'GitHub Container Registry', url: 'ghcr.io', hint: 'Username is your GitHub handle; password is a PAT with read:packages' },
  { label: 'GitLab Registry', url: 'registry.gitlab.com', hint: 'Use a deploy token or PAT with read_registry' },
  { label: 'Amazon ECR', url: '', hint: 'Host is <account>.dkr.ecr.<region>.amazonaws.com; username AWS, password a 12-hour ECR token' },
  { label: 'Azure ACR', url: '', hint: 'Host is <registry>.azurecr.io; use an ACR token or service principal' },
  { label: 'Google GCR', url: 'gcr.io', hint: 'Username is _json_key; password is the service-account JSON' },
  { label: 'Other / self-hosted', url: '', hint: 'host or host:port, e.g. harbor.corp.example:5000' },
];
