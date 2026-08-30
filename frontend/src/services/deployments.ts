import { apiFetch } from './client';

export interface EnvVar {
  key: string;
  value: string;
}

/**
 * A sensitive variable as the UI holds it. `value` is only ever populated for
 * entries the user just typed — the server never sends secret values back, so
 * an existing key arrives with `value: ''` and `isExisting: true`.
 */
export interface SecretVar {
  key: string;
  value: string;
  isExisting: boolean;
}

/**
 * An HTTP health check. `path` empty means the probe is not configured — the
 * backend omits it from the pod spec entirely rather than sending an empty one.
 */
export interface Probe {
  path: string;
  port: number;
  initialDelaySeconds: number;
  timeoutSeconds: number;
  periodSeconds: number;
  failureThreshold: number;
}

export function emptyProbe(): Probe {
  return { path: '', port: 0, initialDelaySeconds: 0, timeoutSeconds: 0, periodSeconds: 0, failureThreshold: 0 };
}

/** Drops a probe the user left blank so no empty message reaches the API. */
export function probeOrUndefined(p: Probe): Probe | undefined {
  return p.path.trim() === '' ? undefined : p;
}

function toProbe(raw: any): Probe | null {
  if (!raw || !raw.path) return null;
  return {
    path: raw.path,
    port: raw.port ?? 0,
    initialDelaySeconds: raw.initialDelaySeconds ?? 0,
    timeoutSeconds: raw.timeoutSeconds ?? 0,
    periodSeconds: raw.periodSeconds ?? 0,
    failureThreshold: raw.failureThreshold ?? 0,
  };
}

/** Kubernetes quantity strings, e.g. "250m" / "512Mi". Empty means unset. */
export interface ResourceLimits {
  cpuRequest: string;
  cpuLimit: string;
  memoryRequest: string;
  memoryLimit: string;
}

export function emptyResources(): ResourceLimits {
  return { cpuRequest: '', cpuLimit: '', memoryRequest: '', memoryLimit: '' };
}

export interface Autoscaling {
  minReplicas: number;
  maxReplicas: number;
  cpuAverageUtilization: number;
  memoryAverageUtilization: number;
}

export function emptyAutoscaling(): Autoscaling {
  return { minReplicas: 1, maxReplicas: 0, cpuAverageUtilization: 70, memoryAverageUtilization: 0 };
}

function toAutoscaling(a: any): Autoscaling | null {
  if (!a || !a.maxReplicas) return null;
  return {
    minReplicas: a.minReplicas ?? 1,
    maxReplicas: a.maxReplicas ?? 0,
    cpuAverageUtilization: a.cpuAverageUtilization ?? 70,
    memoryAverageUtilization: a.memoryAverageUtilization ?? 0,
  };
}

/** A golden path: reviewed defaults for a known stack. */
export interface DeploymentTemplate {
  id: string;
  name: string;
  description: string;
  category: string;
  replicas: number;
  port: number;
  resources: ResourceLimits;
  readinessProbe: Probe | null;
  livenessProbe: Probe | null;
  configVars: EnvVar[];
  /** Secret names the stack usually needs. Never carries values. */
  suggestedSecretKeys: string[];
  exampleImage: string;
  rationale: string;
  autoscaling: Autoscaling | null;
}

export async function listDeploymentTemplates(): Promise<DeploymentTemplate[]> {
  const response = await apiFetch('/idp.v1.DeploymentService/ListDeploymentTemplates', {});
  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to load templates: ${response.statusText}`);
  }
  const data = await response.json();
  return (data.templates || []).map((t: any) => ({
    id: t.id || '',
    name: t.name || '',
    description: t.description || '',
    category: t.category || '',
    replicas: t.replicas ?? 1,
    port: t.port ?? 80,
    resources: {
      cpuRequest: t.resources?.cpuRequest || '',
      cpuLimit: t.resources?.cpuLimit || '',
      memoryRequest: t.resources?.memoryRequest || '',
      memoryLimit: t.resources?.memoryLimit || '',
    },
    readinessProbe: toProbe(t.readinessProbe),
    livenessProbe: toProbe(t.livenessProbe),
    configVars: t.configVars || [],
    suggestedSecretKeys: t.suggestedSecretKeys || [],
    exampleImage: t.exampleImage || '',
    rationale: t.rationale || '',
    autoscaling: toAutoscaling(t.autoscaling),
  }));
}

/** One revision of a Deployment, reconstructed from its ReplicaSets. */
export interface Rollout {
  revision: number;
  createdAt: string;
  image: string;
  replicas: number;
  readyReplicas: number;
  /** Active for the serving revision, Superseded otherwise. */
  status: string;
  changeCause: string;
  current: boolean;
}

export async function listRollouts(namespace: string, name: string): Promise<Rollout[]> {
  const response = await apiFetch('/idp.v1.DeploymentService/ListRollouts', { namespace, name });
  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to load history: ${response.statusText}`);
  }
  const data = await response.json();
  return (data.rollouts || []).map((r: any) => ({
    // Proto int64 arrives as a JSON string, so it must be parsed rather than
    // used directly — numeric comparisons would silently fail otherwise.
    revision: Number(r.revision ?? 0),
    createdAt: r.createdAt || '',
    image: r.image || '',
    replicas: r.replicas ?? 0,
    readyReplicas: r.readyReplicas ?? 0,
    status: r.status || '',
    changeCause: r.changeCause || '',
    current: r.current ?? false,
  }));
}

/** Rolls back to `revision`, or to the previous revision when it is 0. */
export async function rollbackDeployment(
  namespace: string,
  name: string,
  revision = 0,
): Promise<{ revision: number }> {
  const response = await apiFetch('/idp.v1.DeploymentService/RollbackDeployment', {
    namespace,
    name,
    // Connect JSON encodes proto int64 as a string; sending a number can be
    // rejected or truncated depending on the codec.
    revision: String(revision),
  });
  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to roll back: ${response.statusText}`);
  }
  const data = await response.json();
  return { revision: Number(data.revision ?? 0) };
}

export interface DeploymentConfig {
  configVars: EnvVar[];
  secretKeys: string[];
  configMapName: string;
  secretName: string;
}

export interface Deployment {
  name: string;
  namespace: string;
  image: string;
  replicas: number;
  readyReplicas: number;
  availableReplicas: number;
  status: string;
  statusReason: string;
  port: number;
  serviceType: string;
  nodePort: number;
  /** Kubernetes Service ClusterIP — same value as `kubectl get svc`. */
  clusterIp: string;
  clusterAddress: string;
  /** Non-sensitive configuration, read back from the workload's ConfigMap. */
  envVars: EnvVar[];
  configMapName: string;
  secretName: string;
  /** Names of sensitive variables. No API returns their values. */
  secretKeys: string[];
  /** Browser-reachable URL from the Ingress, empty when none exists. */
  url: string;
  ingressHost: string;
  /** null when the workload has no such probe configured. */
  readinessProbe: Probe | null;
  livenessProbe: Probe | null;
  resources?: ResourceLimits;
  autoscaling: Autoscaling | null;
  createdAt: string;
}

export interface ListDeploymentsResponse {
  deployments: Deployment[];
  pageInfo?: {
    page: number;
    pageSize: number;
    totalCount: string;
    totalPages: number;
  };
}

export async function listDeployments(namespace: string, page = 1, pageSize = 20): Promise<ListDeploymentsResponse> {
  const response = await apiFetch('/idp.v1.DeploymentService/ListDeployments', {
    namespace,
    page: { page, pageSize },
  });

  if (!response.ok) {
    throw new Error(`Failed to list deployments: ${response.statusText}`);
  }

  const data = await response.json();
  return {
    deployments: (data.deployments || []).map((d: any) => ({
      name: d.name,
      namespace: d.namespace,
      image: d.image,
      replicas: d.replicas ?? 0,
      readyReplicas: d.readyReplicas ?? 0,
      availableReplicas: d.availableReplicas ?? 0,
      status: d.status || 'Unknown',
      statusReason: d.statusReason || '',
      port: d.port ?? 0,
      serviceType: d.serviceType || '',
      nodePort: d.nodePort ?? 0,
      clusterIp: d.clusterIp || '',
      clusterAddress: d.clusterAddress || '',
      envVars: d.envVars || [],
      configMapName: d.configMapName || '',
      secretName: d.secretName || '',
      secretKeys: d.secretKeys || [],
      url: d.url || '',
      ingressHost: d.ingressHost || '',
      readinessProbe: toProbe(d.readinessProbe),
      livenessProbe: toProbe(d.livenessProbe),
      resources: d.resources
        ? {
            cpuRequest: d.resources.cpuRequest || '',
            cpuLimit: d.resources.cpuLimit || '',
            memoryRequest: d.resources.memoryRequest || '',
            memoryLimit: d.resources.memoryLimit || '',
          }
        : emptyResources(),
      autoscaling: toAutoscaling(d.autoscaling),
      createdAt: d.createdAt || '',
    })),
    pageInfo: data.pageInfo,
  };
}

export interface CreateDeploymentInput {
  namespace: string;
  name: string;
  image: string;
  replicas?: number;
  port?: number;
  serviceType?: string;
  labels?: Record<string, string>;
  /** Non-sensitive values → ConfigMap. */
  configVars?: EnvVar[];
  /** Sensitive values → Secret. Never echoed back by any read. */
  secretVars?: EnvVar[];
  /** Omit to leave the probe unconfigured. */
  readinessProbe?: Probe;
  livenessProbe?: Probe;
  /** Override the generated <app>.<project>.<domain> hostname. */
  hostname?: string;
  /** Skip Ingress creation for workloads that should stay internal. */
  ingressDisabled?: boolean;
  resources?: ResourceLimits;
  /** Recorded as a label for traceability. */
  templateId?: string;
  /** Attach a PVC. Database images enable this automatically server-side. */
  persistent?: boolean;
  /** Skip automatic PVC for database images. */
  disablePersistence?: boolean;
  /** PVC size, e.g. "5Gi". */
  storageSize?: string;
  autoscaling?: Autoscaling | null;
}

export async function createDeployment(input: CreateDeploymentInput): Promise<{ deployment: Deployment }> {
  const response = await apiFetch('/idp.v1.DeploymentService/CreateDeployment', {
    namespace: input.namespace,
    name: input.name,
    image: input.image,
    replicas: input.replicas ?? 1,
    labels: input.labels ?? {},
    port: input.port ?? 80,
    serviceType: input.serviceType ?? 'NodePort',
    configVars: input.configVars ?? [],
    secretVars: input.secretVars ?? [],
    readinessProbe: input.readinessProbe ?? null,
    livenessProbe: input.livenessProbe ?? null,
    hostname: input.hostname ?? '',
    ingressDisabled: input.ingressDisabled ?? false,
    resources: input.resources ?? null,
    templateId: input.templateId ?? '',
    persistent: input.persistent ?? false,
    disablePersistence: input.disablePersistence ?? false,
    storageSize: input.storageSize ?? '',
    autoscaling: input.autoscaling ?? null,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to create deployment: ${response.statusText}`);
  }

  return response.json();
}

export async function getDeploymentConfig(namespace: string, name: string): Promise<DeploymentConfig> {
  const response = await apiFetch('/idp.v1.DeploymentService/GetDeploymentConfig', { namespace, name });
  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to load configuration: ${response.statusText}`);
  }
  const data = await response.json();
  return {
    configVars: data.configVars || [],
    secretKeys: data.secretKeys || [],
    configMapName: data.configMapName || '',
    secretName: data.secretName || '',
  };
}

/**
 * Config variables are replaced wholesale — the UI was shown every value, so it
 * can send the full desired set. Secrets are patched: only entries the user
 * actually typed are sent, plus an explicit removal list, because the UI never
 * holds the existing values and a full replace would erase them.
 */
export async function updateDeploymentConfig(input: {
  namespace: string;
  name: string;
  configVars: EnvVar[];
  secretVars: EnvVar[];
  removedSecretKeys: string[];
}): Promise<{ configVars: EnvVar[]; secretKeys: string[]; restarted: boolean }> {
  const response = await apiFetch('/idp.v1.DeploymentService/UpdateDeploymentConfig', {
    namespace: input.namespace,
    name: input.name,
    configVars: input.configVars,
    secretVars: input.secretVars,
    removedSecretKeys: input.removedSecretKeys,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to update configuration: ${response.statusText}`);
  }

  const data = await response.json();
  return {
    configVars: data.configVars || [],
    secretKeys: data.secretKeys || [],
    restarted: data.restarted ?? false,
  };
}

export async function scaleDeployment(
  namespace: string,
  name: string,
  replicas: number,
  autoscaling?: Autoscaling | null,
): Promise<{ deployment: Deployment }> {
  const response = await apiFetch('/idp.v1.DeploymentService/ScaleDeployment', {
    namespace,
    name,
    replicas,
    autoscaling: autoscaling ?? null,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to scale deployment: ${response.statusText}`);
  }

  return response.json();
}

export async function restartDeployment(namespace: string, name: string): Promise<{ deployment: Deployment }> {
  const response = await apiFetch('/idp.v1.DeploymentService/RestartDeployment', {
    namespace,
    name,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to restart deployment: ${response.statusText}`);
  }

  return response.json();
}

export async function deleteDeployment(namespace: string, name: string): Promise<void> {
  const response = await apiFetch('/idp.v1.DeploymentService/DeleteDeployment', {
    namespace,
    name,
  });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to delete deployment: ${response.statusText}`);
  }
}
