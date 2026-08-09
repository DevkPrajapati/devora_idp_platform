import { apiFetch } from './client';

export interface StorageOverview {
  connected: boolean;
  pvcCount: number;
  boundPvcCount: number;
  pendingPvcCount: number;
  pvCount: number;
  availablePvCount: number;
  storageClassCount: number;
  totalRequested: string;
  totalCapacity: string;
  totalRequestedBytes: number;
  totalCapacityBytes: number;
}

export interface PersistentVolumeClaimInfo {
  name: string;
  namespace: string;
  phase: string;
  volumeName: string;
  requested: string;
  capacity: string;
  requestedBytes: number;
  capacityBytes: number;
  storageClass: string;
  accessModes: string[];
  volumeMode: string;
  createdAt: string;
  usedBy: string[];
}

export interface PersistentVolumeInfo {
  name: string;
  phase: string;
  capacity: string;
  capacityBytes: number;
  storageClass: string;
  accessModes: string[];
  reclaimPolicy: string;
  claim: string;
  driver: string;
  createdAt: string;
}

export interface StorageClassInfo {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBindingMode: string;
  allowVolumeExpansion: boolean;
  isDefault: boolean;
  createdAt: string;
}

export interface NodeStorageInfo {
  name: string;
  containerRuntime: string;
  runtimeName: string;
  runtimeVersion: string;
  kubeletVersion: string;
  osImage: string;
  kernelVersion: string;
  architecture: string;
  operatingSystem: string;
  ephemeralStorageCapacity: string;
  ephemeralStorageAllocatable: string;
  ephemeralStorageCapacityBytes: number;
  ephemeralStorageAllocatableBytes: number;
  imageCount: number;
  imageBytes: number;
  imageSize: string;
  diskPressure: boolean;
  ready: boolean;
}

/**
 * protojson serialises 64-bit integers as JSON *strings* to avoid the precision
 * loss of an IEEE-754 double. Every byte count below therefore arrives as
 * "10737418240" rather than a number, and arithmetic on it would concatenate
 * instead of adding.
 */
function toNumber(value: unknown): number {
  if (typeof value === 'number') return value;
  if (typeof value === 'string' && value !== '') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

async function post(procedure: string, body: Record<string, unknown> = {}): Promise<any> {
  const response = await apiFetch(`/idp.v1.StorageService/${procedure}`, body);
  if (!response.ok) {
    // Connect returns a JSON body carrying the human-readable reason; the bare
    // status text alone ("Bad Request") tells the user nothing actionable.
    const detail = await response.json().catch(() => null);
    throw new Error(detail?.message || `${procedure} failed: ${response.statusText}`);
  }
  return response.json();
}

export async function getStorageOverview(): Promise<StorageOverview> {
  const data = await post('GetStorageOverview');
  return {
    connected: data.connected ?? false,
    pvcCount: data.pvcCount ?? 0,
    boundPvcCount: data.boundPvcCount ?? 0,
    pendingPvcCount: data.pendingPvcCount ?? 0,
    pvCount: data.pvCount ?? 0,
    availablePvCount: data.availablePvCount ?? 0,
    storageClassCount: data.storageClassCount ?? 0,
    totalRequested: data.totalRequested || '0',
    totalCapacity: data.totalCapacity || '0',
    totalRequestedBytes: toNumber(data.totalRequestedBytes),
    totalCapacityBytes: toNumber(data.totalCapacityBytes),
  };
}

export async function listPersistentVolumeClaims(
  namespace = '',
): Promise<PersistentVolumeClaimInfo[]> {
  const data = await post('ListPersistentVolumeClaims', { namespace });
  return (data.claims ?? []).map((c: any) => ({
    name: c.name ?? '',
    namespace: c.namespace ?? '',
    phase: c.phase || 'Unknown',
    volumeName: c.volumeName ?? '',
    requested: c.requested ?? '',
    capacity: c.capacity ?? '',
    requestedBytes: toNumber(c.requestedBytes),
    capacityBytes: toNumber(c.capacityBytes),
    storageClass: c.storageClass ?? '',
    accessModes: c.accessModes ?? [],
    volumeMode: c.volumeMode ?? '',
    createdAt: c.createdAt ?? '',
    usedBy: c.usedBy ?? [],
  }));
}

export async function listPersistentVolumes(): Promise<PersistentVolumeInfo[]> {
  const data = await post('ListPersistentVolumes');
  return (data.volumes ?? []).map((v: any) => ({
    name: v.name ?? '',
    phase: v.phase || 'Unknown',
    capacity: v.capacity ?? '',
    capacityBytes: toNumber(v.capacityBytes),
    storageClass: v.storageClass ?? '',
    accessModes: v.accessModes ?? [],
    reclaimPolicy: v.reclaimPolicy ?? '',
    claim: v.claim ?? '',
    driver: v.driver ?? '',
    createdAt: v.createdAt ?? '',
  }));
}

export async function listStorageClasses(): Promise<StorageClassInfo[]> {
  const data = await post('ListStorageClasses');
  return (data.storageClasses ?? []).map((s: any) => ({
    name: s.name ?? '',
    provisioner: s.provisioner ?? '',
    reclaimPolicy: s.reclaimPolicy ?? '',
    volumeBindingMode: s.volumeBindingMode ?? '',
    allowVolumeExpansion: s.allowVolumeExpansion ?? false,
    isDefault: s.isDefault ?? false,
    createdAt: s.createdAt ?? '',
  }));
}

export async function listNodeStorage(): Promise<NodeStorageInfo[]> {
  const data = await post('ListNodeStorage');
  return (data.nodes ?? []).map((n: any) => ({
    name: n.name ?? '',
    containerRuntime: n.containerRuntime ?? '',
    runtimeName: n.runtimeName ?? '',
    runtimeVersion: n.runtimeVersion ?? '',
    kubeletVersion: n.kubeletVersion ?? '',
    osImage: n.osImage ?? '',
    kernelVersion: n.kernelVersion ?? '',
    architecture: n.architecture ?? '',
    operatingSystem: n.operatingSystem ?? '',
    ephemeralStorageCapacity: n.ephemeralStorageCapacity ?? '',
    ephemeralStorageAllocatable: n.ephemeralStorageAllocatable ?? '',
    ephemeralStorageCapacityBytes: toNumber(n.ephemeralStorageCapacityBytes),
    ephemeralStorageAllocatableBytes: toNumber(n.ephemeralStorageAllocatableBytes),
    imageCount: n.imageCount ?? 0,
    imageBytes: toNumber(n.imageBytes),
    imageSize: n.imageSize || '0',
    diskPressure: n.diskPressure ?? false,
    ready: n.ready ?? false,
  }));
}
