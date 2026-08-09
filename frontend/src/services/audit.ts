import { apiFetch } from './client';

export interface AuditLog {
  id: string;
  userId: string;
  userEmail: string;
  action: string;
  namespace: string;
  resource: string;
  resourceType: string;
  result: string;
  details: string;
  createdAt: string;
}

export interface ListAuditLogsResponse {
  logs: AuditLog[];
  pageInfo?: {
    page: number;
    pageSize: number;
    totalCount: string;
    totalPages: number;
  };
}

export async function listAuditLogs(
  page = 1,
  pageSize = 20,
  namespace = '',
  userId = '',
  action = ''
): Promise<ListAuditLogsResponse> {
  const response = await apiFetch('/idp.v1.AuditService/ListAuditLogs', {
    page: { page, pageSize },
    namespace,
    userId,
    action,
  });

  if (!response.ok) {
    throw new Error(`Failed to list audit logs: ${response.statusText}`);
  }

  const data = await response.json();
  return {
    logs: (data.logs || []).map((l: any) => ({
      id: l.id,
      userId: l.userId || '',
      userEmail: l.userEmail || '',
      action: l.action || '',
      namespace: l.namespace || '',
      resource: l.resource || '',
      resourceType: l.resourceType || '',
      result: l.result || '',
      details: l.details || '',
      createdAt: l.createdAt || '',
    })),
    pageInfo: data.pageInfo,
  };
}
