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

export interface PodInfo {
  name: string;
  namespace: string;
  status: string;
  ip: string;
  node: string;
  restartCount: number;
  createdAt: string;
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

export async function listPods(namespace = ''): Promise<PodInfo[]> {
  const response = await apiFetch('/idp.v1.ClusterService/ListPods', { namespace });

  if (!response.ok) {
    throw new Error(`Failed to list pods: ${response.statusText}`);
  }

  const data = await response.json();
  return (data.pods || []).map((p: any) => ({
    name: p.name,
    namespace: p.namespace,
    status: p.status,
    ip: p.ip || '',
    node: p.node || '',
    restartCount: p.restartCount ?? 0,
    createdAt: p.createdAt || '',
  }));
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
  role: string;
  cpuCapacity: string;
  memoryCapacity: string;
  cpuAllocatable: string;
  memoryAllocatable: string;
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
    role: n.role || '',
    cpuCapacity: n.cpuCapacity || '',
    memoryCapacity: n.memoryCapacity || '',
    cpuAllocatable: n.cpuAllocatable || '',
    memoryAllocatable: n.memoryAllocatable || '',
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
