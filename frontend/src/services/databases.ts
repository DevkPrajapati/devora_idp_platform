import { apiFetch } from './client';

export interface DatabaseInstance {
  namespace: string;
  name: string;
  engine: string;
  engineName: string;
  image: string;
  podName: string;
  container: string;
  port: number;
  ready: boolean;
  serviceName: string;
  persistentVolumeClaims: string[];
  credentialsResolved: boolean;
  credentialsHint: string;
}

export interface DatabaseTable {
  schema: string;
  name: string;
  rowEstimate: number;
  sizeBytes: number;
}

export interface DatabaseOverview {
  engine: string;
  database: string;
  version: string;
  tableCount: number;
  schemaCount: number;
  sizeBytes: number;
  activeConnections: number;
  tables: DatabaseTable[];
  tablesTruncated: boolean;
  inspectedAt: string;
}

export interface QueryDocumentsResult {
  documents: string[];
  returned: number;
  limit: number;
  skip: number;
  truncated: boolean;
}

function toNumber(value: unknown): number {
  if (typeof value === 'number') return value;
  if (typeof value === 'string' && value !== '') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

async function post(procedure: string, body: Record<string, unknown> = {}): Promise<any> {
  const response = await apiFetch(`/idp.v1.DatabaseService/${procedure}`, body);
  if (!response.ok) {
    const detail = await response.json().catch(() => null);
    throw new Error(detail?.message || `${procedure} failed: ${response.statusText}`);
  }
  return response.json();
}

export async function listDatabases(namespace = ''): Promise<{
  connected: boolean;
  instances: DatabaseInstance[];
}> {
  const data = await post('ListDatabases', { namespace });
  return {
    connected: data.connected ?? false,
    instances: (data.instances ?? []).map((i: any) => ({
      namespace: i.namespace ?? '',
      name: i.name ?? '',
      engine: i.engine ?? '',
      engineName: i.engineName ?? i.engine ?? '',
      image: i.image ?? '',
      podName: i.podName ?? '',
      container: i.container ?? '',
      port: toNumber(i.port),
      ready: i.ready ?? false,
      serviceName: i.serviceName ?? '',
      persistentVolumeClaims: i.persistentVolumeClaims ?? [],
      credentialsResolved: i.credentialsResolved ?? false,
      credentialsHint: i.credentialsHint ?? '',
    })),
  };
}

export async function inspectDatabase(
  namespace: string,
  name: string,
): Promise<DatabaseOverview> {
  const data = await post('InspectDatabase', { namespace, name });
  return {
    engine: data.engine ?? '',
    database: data.database ?? '',
    version: data.version ?? '',
    tableCount: toNumber(data.tableCount),
    schemaCount: toNumber(data.schemaCount),
    sizeBytes: toNumber(data.sizeBytes),
    activeConnections: toNumber(data.activeConnections),
    tables: (data.tables ?? []).map((t: any) => ({
      schema: t.schema ?? '',
      name: t.name ?? '',
      rowEstimate: toNumber(t.rowEstimate),
      sizeBytes: toNumber(t.sizeBytes),
    })),
    tablesTruncated: data.tablesTruncated ?? false,
    inspectedAt: data.inspectedAt ?? '',
  };
}

export async function queryDocuments(params: {
  namespace: string;
  name: string;
  schema: string;
  table: string;
  limit?: number;
  skip?: number;
}): Promise<QueryDocumentsResult> {
  const data = await post('QueryDocuments', {
    namespace: params.namespace,
    name: params.name,
    schema: params.schema,
    table: params.table,
    limit: params.limit ?? 50,
    skip: params.skip ?? 0,
  });
  return {
    documents: data.documents ?? [],
    returned: toNumber(data.returned),
    limit: toNumber(data.limit),
    skip: toNumber(data.skip),
    truncated: data.truncated ?? false,
  };
}

export interface ExportResult {
  filename: string;
  contentType: string;
  sizeBytes: number;
  blob: Blob;
}

export interface PersistenceResult {
  pvcName: string;
  mountPath: string;
  storageSize: string;
  patched: boolean;
  message: string;
}

/** protojson encodes bytes fields as base64 strings. */
function base64ToUint8Array(value: string): Uint8Array {
  const binary = atob(value);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

function uint8ArrayToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]!);
  }
  return btoa(binary);
}

export async function exportDatabase(namespace: string, name: string): Promise<ExportResult> {
  const data = await post('ExportDatabase', { namespace, name });
  const raw = typeof data.archive === 'string' ? base64ToUint8Array(data.archive) : new Uint8Array();
  const contentType = data.contentType || 'application/octet-stream';
  return {
    filename: data.filename || `${name}-export.bin`,
    contentType,
    sizeBytes: toNumber(data.sizeBytes) || raw.byteLength,
    blob: new Blob([raw.buffer.slice(raw.byteOffset, raw.byteOffset + raw.byteLength)], {
      type: contentType,
    }),
  };
}

export async function importDatabase(
  namespace: string,
  name: string,
  file: File,
): Promise<{ message: string; sizeBytes: number }> {
  const buffer = new Uint8Array(await file.arrayBuffer());
  const data = await post('ImportDatabase', {
    namespace,
    name,
    archive: uint8ArrayToBase64(buffer),
  });
  return {
    message: data.message || 'import completed',
    sizeBytes: toNumber(data.sizeBytes),
  };
}

export async function ensureDatabasePersistence(
  namespace: string,
  name: string,
  storageSize = '5Gi',
): Promise<PersistenceResult> {
  const data = await post('EnsureDatabasePersistence', {
    namespace,
    name,
    storageSize,
  });
  return {
    pvcName: data.pvcName ?? '',
    mountPath: data.mountPath ?? '',
    storageSize: data.storageSize ?? storageSize,
    patched: data.patched ?? false,
    message: data.message ?? '',
  };
}

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

/** True when a deployment image looks like a known database engine. */
export function isDatabaseImage(image: string): boolean {
  const lower = image.toLowerCase();
  return (
    lower.includes('mongo') ||
    lower.includes('postgres') ||
    lower.includes('postgresql') ||
    lower.includes('mysql') ||
    lower.includes('mariadb') ||
    lower.includes('percona') ||
    lower.includes('timescaledb')
  );
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}
