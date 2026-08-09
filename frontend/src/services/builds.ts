import { apiFetch } from './client';

/**
 * A registered git repository. There is no token or webhook-secret field:
 * both are write-only server-side, so the UI can report *whether* one is set
 * but never receives the value.
 */
export interface GitRepository {
  projectSlug: string;
  name: string;
  provider: string;
  cloneUrl: string;
  defaultBranch: string;
  dockerfilePath: string;
  buildContext: string;
  imageRepository: string;
  registryCredential: string;
  autoDeploy: boolean;
  targetNamespace: string;
  targetDeployment: string;
  hasToken: boolean;
  hasWebhookSecret: boolean;
  /** Endpoint to paste into the git provider's webhook settings. */
  webhookUrl: string;
  createdAt: string;
  updatedAt: string;
}

export interface Build {
  repositoryName: string;
  number: number;
  branch: string;
  commitSha: string;
  imageTag: string;
  /** pending | running | succeeded | failed | cancelled */
  status: string;
  /** manual | webhook | retry */
  triggerType: string;
  triggeredBy: string;
  /** Kubernetes Job; also the pod whose logs the viewer streams. */
  jobName: string;
  errorMessage: string;
  startedAt: string;
  finishedAt: string;
  createdAt: string;
}

export const GIT_PROVIDERS = [
  { value: 'github', label: 'GitHub', secretHint: 'Webhook secret — GitHub signs the body with HMAC-SHA256' },
  { value: 'gitlab', label: 'GitLab', secretHint: 'Secret token — GitLab sends it verbatim in X-Gitlab-Token' },
  { value: 'bitbucket', label: 'Bitbucket', secretHint: 'Webhook secret — signed with HMAC-SHA256' },
  { value: 'generic', label: 'Other', secretHint: 'Shared secret, sent as an HMAC signature or a token header' },
] as const;

async function readError(response: Response, fallback: string): Promise<string> {
  const data = await response.json().catch(() => ({}) as any);
  return data?.message || `${fallback}: ${response.statusText}`;
}

function toRepository(raw: any): GitRepository {
  return {
    projectSlug: raw.projectSlug || '',
    name: raw.name || '',
    provider: raw.provider || 'generic',
    cloneUrl: raw.cloneUrl || '',
    defaultBranch: raw.defaultBranch || 'main',
    dockerfilePath: raw.dockerfilePath || 'Dockerfile',
    buildContext: raw.buildContext || '.',
    imageRepository: raw.imageRepository || '',
    registryCredential: raw.registryCredential || '',
    autoDeploy: raw.autoDeploy ?? false,
    targetNamespace: raw.targetNamespace || '',
    targetDeployment: raw.targetDeployment || '',
    hasToken: raw.hasToken ?? false,
    hasWebhookSecret: raw.hasWebhookSecret ?? false,
    webhookUrl: raw.webhookUrl || '',
    createdAt: raw.createdAt || '',
    updatedAt: raw.updatedAt || '',
  };
}

function toBuild(raw: any): Build {
  return {
    repositoryName: raw.repositoryName || '',
    // Proto int64 arrives as a JSON string; comparing it as-is against a
    // number would silently fail.
    number: Number(raw.number ?? 0),
    branch: raw.branch || '',
    commitSha: raw.commitSha || '',
    imageTag: raw.imageTag || '',
    status: raw.status || 'pending',
    triggerType: raw.triggerType || 'manual',
    triggeredBy: raw.triggeredBy || '',
    jobName: raw.jobName || '',
    errorMessage: raw.errorMessage || '',
    startedAt: raw.startedAt || '',
    finishedAt: raw.finishedAt || '',
    createdAt: raw.createdAt || '',
  };
}

export async function listGitRepositories(projectSlug: string): Promise<GitRepository[]> {
  const response = await apiFetch('/idp.v1.BuildService/ListGitRepositories', { projectSlug });
  if (!response.ok) throw new Error(await readError(response, 'Failed to list repositories'));
  const data = await response.json();
  return (data.repositories || []).map(toRepository);
}

export interface SaveGitRepositoryInput {
  projectSlug: string;
  name: string;
  provider: string;
  cloneUrl: string;
  defaultBranch: string;
  dockerfilePath: string;
  buildContext: string;
  imageRepository: string;
  registryCredential: string;
  /** Blank keeps the stored value. */
  token?: string;
  webhookSecret?: string;
  autoDeploy: boolean;
  targetNamespace?: string;
  targetDeployment?: string;
}

export async function saveGitRepository(input: SaveGitRepositoryInput): Promise<GitRepository> {
  const response = await apiFetch('/idp.v1.BuildService/SaveGitRepository', {
    ...input,
    token: input.token ?? '',
    webhookSecret: input.webhookSecret ?? '',
    targetNamespace: input.targetNamespace ?? '',
    targetDeployment: input.targetDeployment ?? '',
  });
  if (!response.ok) throw new Error(await readError(response, 'Failed to save repository'));
  const data = await response.json();
  return toRepository(data.repository || {});
}

export async function deleteGitRepository(projectSlug: string, name: string): Promise<void> {
  const response = await apiFetch('/idp.v1.BuildService/DeleteGitRepository', { projectSlug, name });
  if (!response.ok) throw new Error(await readError(response, 'Failed to delete repository'));
}

export async function triggerBuild(projectSlug: string, name: string, branch = ''): Promise<Build> {
  const response = await apiFetch('/idp.v1.BuildService/TriggerBuild', { projectSlug, name, branch });
  if (!response.ok) throw new Error(await readError(response, 'Failed to start build'));
  const data = await response.json();
  return toBuild(data.build || {});
}

export async function retryBuild(projectSlug: string, name: string, buildNumber: number): Promise<Build> {
  const response = await apiFetch('/idp.v1.BuildService/RetryBuild', { projectSlug, name, buildNumber });
  if (!response.ok) throw new Error(await readError(response, 'Failed to retry build'));
  const data = await response.json();
  return toBuild(data.build || {});
}

export interface ListBuildsResult {
  builds: Build[];
  totalCount: number;
  /** Namespace build Jobs run in, needed to stream their logs. */
  buildNamespace: string;
}

export async function listBuilds(
  projectSlug: string,
  name: string,
  limit = 30,
  offset = 0,
): Promise<ListBuildsResult> {
  const response = await apiFetch('/idp.v1.BuildService/ListBuilds', { projectSlug, name, limit, offset });
  if (!response.ok) throw new Error(await readError(response, 'Failed to load builds'));
  const data = await response.json();
  return {
    builds: (data.builds || []).map(toBuild),
    totalCount: Number(data.totalCount ?? 0),
    buildNamespace: data.buildNamespace || '',
  };
}

/** True while a build is still moving, so the UI knows to keep polling. */
export function isBuildActive(status: string): boolean {
  return status === 'pending' || status === 'running';
}

/** Kubernetes Job name for a build; pods are named `{job}-{suffix}`. */
export function buildJobName(repositoryName: string, number: number): string {
  const name = `build-${repositoryName}-${number}`;
  if (name.length <= 63) return name;
  const suffix = `-${number}`;
  return name.slice(0, 63 - suffix.length) + suffix;
}
