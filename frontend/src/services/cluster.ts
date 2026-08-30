import { apiFetch } from './client';

export interface ClusterOverview {
  clusterName: string;
  connected: boolean;
  namespaceCount: number;
  deploymentCount: number;
  serviceCount: number;
  podCount: number;
  runningPods: number;
  nodeCount: number;
  readyNodes: number;
}

export interface ClusterEvent {
  type: string;
  reason: string;
  message: string;
  namespace: string;
  object: string;
  timestamp: string;
}

export interface ContainerInfo {
  name: string;
  image: string;
  /** Whether the readiness probe passes, and so whether Services route here. */
  ready: boolean;
  /** Running, Waiting or Terminated. */
  state: string;
  /** CrashLoopBackOff, ImagePullBackOff, OOMKilled, Completed. Empty when healthy. */
  reason: string;
  message: string;
  restartCount: number;
  cpuRequest: string;
  memoryRequest: string;
  cpuLimit: string;
  memoryLimit: string;
  hasLivenessProbe: boolean;
  hasReadinessProbe: boolean;
  hasStartupProbe: boolean;
  startedAt: string;
  lastExitCode: number;
  lastTerminationReason: string;
}

export interface PodInfo {
  name: string;
  namespace: string;
  /** Display status, which folds container failures in — see `phase` for the raw value. */
  status: string;
  ip: string;
  node: string;
  restartCount: number;
  createdAt: string;
  containers: ContainerInfo[];
  phase: string;
  reason: string;
  message: string;
  /** True only when every container passes readiness. */
  ready: boolean;
  qosClass: string;
  /** The scheduler's reason a pod has no node, e.g. "Insufficient cpu". */
  schedulingMessage: string;
}

export interface ServiceInfo {
  name: string;
  namespace: string;
  type: string;
  clusterIp: string;
  externalIp: string;
  ports: number[];
  createdAt: string;
}

export async function getOverview(): Promise<ClusterOverview> {
  const response = await apiFetch('/idp.v1.ClusterService/GetOverview', {});

  if (!response.ok) {
    throw new Error(`Failed to get cluster overview: ${response.statusText}`);
  }

  const data = await response.json();
  return {
    clusterName: data.clusterName || 'disconnected',
    connected: data.connected ?? false,
    namespaceCount: data.namespaceCount ?? 0,
    deploymentCount: data.deploymentCount ?? 0,
    serviceCount: data.serviceCount ?? 0,
    podCount: data.podCount ?? 0,
    runningPods: data.runningPods ?? 0,
    nodeCount: data.nodeCount ?? 0,
    readyNodes: data.readyNodes ?? 0,
  };
}

export async function listEvents(namespace = '', limit = 50): Promise<ClusterEvent[]> {
  const response = await apiFetch('/idp.v1.ClusterService/ListEvents', {
    namespace,
    limit,
  });

  if (!response.ok) {
    throw new Error(`Failed to list cluster events: ${response.statusText}`);
  }

  const data = await response.json();
  return data.events || [];
}

export async function listPods(namespace = '', app = ''): Promise<PodInfo[]> {
  const response = await apiFetch('/idp.v1.ClusterService/ListPods', { namespace, app });

  if (!response.ok) {
    throw new Error(`Failed to list pods: ${response.statusText}`);
  }

  const data = await response.json();
  return (data.pods || []).map(toPod);
}

function toContainer(c: any): ContainerInfo {
  return {
    name: c.name || '',
    image: c.image || '',
    ready: c.ready ?? false,
    state: c.state || 'Unknown',
    reason: c.reason || '',
    message: c.message || '',
    restartCount: c.restartCount ?? 0,
    cpuRequest: c.cpuRequest || '',
    memoryRequest: c.memoryRequest || '',
    cpuLimit: c.cpuLimit || '',
    memoryLimit: c.memoryLimit || '',
    hasLivenessProbe: c.hasLivenessProbe ?? false,
    hasReadinessProbe: c.hasReadinessProbe ?? false,
    hasStartupProbe: c.hasStartupProbe ?? false,
    startedAt: c.startedAt || '',
    lastExitCode: c.lastExitCode ?? 0,
    lastTerminationReason: c.lastTerminationReason || '',
  };
}

function toPod(p: any): PodInfo {
  return {
    name: p.name || '',
    namespace: p.namespace || '',
    status: p.status || 'Unknown',
    ip: p.ip || '',
    node: p.node || '',
    restartCount: p.restartCount ?? 0,
    createdAt: p.createdAt || '',
    containers: (p.containers || []).map(toContainer),
    phase: p.phase || '',
    reason: p.reason || '',
    message: p.message || '',
    ready: p.ready ?? false,
    qosClass: p.qosClass || '',
    schedulingMessage: p.schedulingMessage || '',
  };
}

export async function getPodLogs(namespace: string, podName: string, tailLines = 200): Promise<string> {
  const response = await apiFetch('/idp.v1.ClusterService/GetPodLogs', {
    namespace,
    podName,
    tailLines,
  });
  if (!response.ok) {
    throw new Error(`Failed to fetch pod logs: ${response.statusText}`);
  }
  const data = await response.json();
  return data.logs || '';
}

export interface ResourceMetrics {
  cpuRequests: string;
  cpuCapacity: string;
  cpuUsagePercent: number;
  memoryRequests: string;
  memoryCapacity: string;
  memoryUsagePercent: number;
}

export interface NodeInfo {
  name: string;
  status: string;
  /** The kubelet's explanation when the node is not Ready. */
  statusMessage: string;
  role: string;
  cpuCapacity: string;
  memoryCapacity: string;
  cpuAllocatable: string;
  memoryAllocatable: string;
  /** The node's pod ceiling. Hitting it makes further pods unschedulable. */
  podCapacity: number;
  podCount: number;
  cpuRequests: string;
  memoryRequests: string;
  cpuRequestsPercent: number;
  memoryRequestsPercent: number;
  podsPercent: number;
  kubeletVersion: string;
  unschedulable: boolean;
  /** MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable. */
  pressureConditions: string[];
  createdAt: string;
}

export async function getResourceMetrics(): Promise<ResourceMetrics> {
  const response = await apiFetch('/idp.v1.ClusterService/GetResourceMetrics', {});

  if (!response.ok) {
    throw new Error(`Failed to get resource metrics: ${response.statusText}`);
  }

  const data = await response.json();
  return {
    cpuRequests: data.cpuRequests || '0',
    cpuCapacity: data.cpuCapacity || '0',
    cpuUsagePercent: data.cpuUsagePercent ?? 0,
    memoryRequests: data.memoryRequests || '0',
    memoryCapacity: data.memoryCapacity || '0',
    memoryUsagePercent: data.memoryUsagePercent ?? 0,
  };
}

export async function listNodes(): Promise<NodeInfo[]> {
  const response = await apiFetch('/idp.v1.ClusterService/ListNodes', {});

  if (!response.ok) {
    throw new Error(`Failed to list nodes: ${response.statusText}`);
  }

  const data = await response.json();
  return (data.nodes || []).map((n: any) => ({
    name: n.name || '',
    status: n.status || 'Unknown',
    statusMessage: n.statusMessage || '',
    role: n.role || '',
    cpuCapacity: n.cpuCapacity || '',
    memoryCapacity: n.memoryCapacity || '',
    cpuAllocatable: n.cpuAllocatable || '',
    memoryAllocatable: n.memoryAllocatable || '',
    podCapacity: n.podCapacity ?? 0,
    podCount: n.podCount ?? 0,
    cpuRequests: n.cpuRequests || '0',
    memoryRequests: n.memoryRequests || '0',
    cpuRequestsPercent: n.cpuRequestsPercent ?? 0,
    memoryRequestsPercent: n.memoryRequestsPercent ?? 0,
    podsPercent: n.podsPercent ?? 0,
    kubeletVersion: n.kubeletVersion || '',
    unschedulable: n.unschedulable ?? false,
    pressureConditions: n.pressureConditions || [],
    createdAt: n.createdAt || '',
  }));
}

export async function listServices(namespace = ''): Promise<ServiceInfo[]> {
  const response = await apiFetch('/idp.v1.ClusterService/ListServices', { namespace });

  if (!response.ok) {
    throw new Error(`Failed to list services: ${response.statusText}`);
  }

  const data = await response.json();
  return (data.services || []).map((s: any) => ({
    name: s.name,
    namespace: s.namespace,
    type: s.type,
    clusterIp: s.clusterIp || '',
    externalIp: s.externalIp || '',
    ports: s.ports || [],
    createdAt: s.createdAt || '',
  }));
}

export type ClusterNamespaceKind = 'tenant' | 'system' | 'cluster';

export interface ClusterNamespace {
  name: string;
  phase: string;
  createdAt: string;
  labels: Record<string, string>;
  managed: boolean;
  kind: ClusterNamespaceKind;
  displayName: string;
}

export interface NamespaceResource {
  kind: string;
  name: string;
  status: string;
  detail: string;
  createdAt: string;
}

export interface ResourceGroup {
  name: string;
  items: NamespaceResource[];
}

export interface NamespaceInventory {
  namespace: ClusterNamespace;
  groups: ResourceGroup[];
  totalResources: number;
}

export async function listClusterNamespaces(): Promise<ClusterNamespace[]> {
  const response = await apiFetch('/idp.v1.ClusterService/ListClusterNamespaces', {});

  if (!response.ok) {
    throw new Error(`Failed to list cluster namespaces: ${response.statusText}`);
  }

  const data = await response.json();
  return (data.namespaces || []).map((ns: any): ClusterNamespace => ({
    name: ns.name || '',
    phase: ns.phase || 'Unknown',
    createdAt: ns.createdAt || '',
    labels: ns.labels || {},
    managed: Boolean(ns.managed),
    kind: (ns.kind || 'cluster') as ClusterNamespaceKind,
    displayName: ns.displayName || ns.name || '',
  }));
}

export async function getNamespaceResources(name: string): Promise<NamespaceInventory> {
  const response = await apiFetch('/idp.v1.ClusterService/GetNamespaceResources', { name });

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}));
    throw new Error(errData.message || `Failed to load namespace ${name}: ${response.statusText}`);
  }

  const data = await response.json();
  const ns = data.namespace || {};
  return {
    namespace: {
      name: ns.name || name,
      phase: ns.phase || 'Unknown',
      createdAt: ns.createdAt || '',
      labels: ns.labels || {},
      managed: Boolean(ns.managed),
      kind: (ns.kind || 'cluster') as ClusterNamespaceKind,
      displayName: ns.displayName || ns.name || name,
    },
    groups: (data.groups || []).map((g: any): ResourceGroup => ({
      name: g.name || '',
      items: (g.items || []).map((item: any): NamespaceResource => ({
        kind: item.kind || '',
        name: item.name || '',
        status: item.status || '',
        detail: item.detail || '',
        createdAt: item.createdAt || '',
      })),
    })),
    totalResources: data.totalResources ?? 0,
  };
}

export type ClusterProvider = 'kind' | 'minikube' | 'imported';
export type ClusterStatus = 'provisioning' | 'starting' | 'running' | 'stopping' | 'stopped' | 'error' | 'deleting';

export interface ManagedCluster {
  id: string;
  name: string;
  displayName: string;
  provider: ClusterProvider;
  status: ClusterStatus;
  active: boolean;
  connected: boolean;
  serverUrl: string;
  kubernetesVersion: string;
  nodeCount: number;
  lastError: string;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface ClusterFleet {
  clusters: ManagedCluster[];
  kindAvailable: boolean;
  minikubeAvailable: boolean;
}

function mapCluster(c: any): ManagedCluster {
  return {
    id: c.id || '',
    name: c.name || '',
    displayName: c.displayName || c.name || '',
    provider: (c.provider || 'imported') as ClusterProvider,
    status: (c.status || 'error') as ClusterStatus,
    active: Boolean(c.active),
    connected: Boolean(c.connected),
    serverUrl: c.serverUrl || '',
    kubernetesVersion: c.kubernetesVersion || '',
    nodeCount: c.nodeCount ?? 0,
    lastError: c.lastError || '',
    createdBy: c.createdBy || '',
    createdAt: c.createdAt || '',
    updatedAt: c.updatedAt || '',
  };
}

async function readError(response: Response, fallback: string): Promise<string> {
  const data = await response.json().catch(() => ({}));
  return data.message || fallback;
}

export async function listClusters(): Promise<ClusterFleet> {
  const response = await apiFetch('/idp.v1.ClusterService/ListClusters', {});
  if (!response.ok) {
    throw new Error(await readError(response, `Failed to list clusters: ${response.statusText}`));
  }
  const data = await response.json();
  return {
    clusters: (data.clusters || []).map(mapCluster),
    kindAvailable: Boolean(data.kindAvailable),
    minikubeAvailable: Boolean(data.minikubeAvailable),
  };
}

export interface CreateClusterInput {
  name: string;
  displayName?: string;
  provider: ClusterProvider;
  kubeconfig?: string;
  kubernetesVersion?: string;
  workerCount?: number;
  activate?: boolean;
}

export async function createCluster(input: CreateClusterInput): Promise<ManagedCluster> {
  const response = await apiFetch('/idp.v1.ClusterService/CreateCluster', {
    name: input.name,
    displayName: input.displayName || '',
    provider: input.provider,
    kubeconfig: input.kubeconfig || '',
    kubernetesVersion: input.kubernetesVersion || '',
    workerCount: input.workerCount ?? 0,
    activate: input.activate ?? true,
  });
  if (!response.ok) {
    throw new Error(await readError(response, `Failed to create cluster: ${response.statusText}`));
  }
  return mapCluster(await response.json());
}

export async function activateCluster(id: string): Promise<ManagedCluster> {
  const response = await apiFetch('/idp.v1.ClusterService/ActivateCluster', { id });
  if (!response.ok) {
    throw new Error(await readError(response, `Failed to activate cluster: ${response.statusText}`));
  }
  return mapCluster(await response.json());
}

export async function stopCluster(id: string): Promise<ManagedCluster> {
  const response = await apiFetch('/idp.v1.ClusterService/StopCluster', { id });
  if (!response.ok) {
    throw new Error(await readError(response, `Failed to stop cluster: ${response.statusText}`));
  }
  return mapCluster(await response.json());
}

export async function restartCluster(id: string): Promise<ManagedCluster> {
  const response = await apiFetch('/idp.v1.ClusterService/RestartCluster', { id });
  if (!response.ok) {
    throw new Error(await readError(response, `Failed to restart cluster: ${response.statusText}`));
  }
  return mapCluster(await response.json());
}

export async function deleteCluster(id: string): Promise<void> {
  const response = await apiFetch('/idp.v1.ClusterService/DeleteCluster', { id });
  if (!response.ok) {
    throw new Error(await readError(response, `Failed to delete cluster: ${response.statusText}`));
  }
}
