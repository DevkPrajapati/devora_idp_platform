import { apiFetch } from './client';

export interface Project {
  id: string;
  slug: string;
  name: string;
  description: string;
  ownerId: string;
  ownerEmail: string;
  labels: Record<string, string>;
  status: string;
  memberCount: number;
  namespaceCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectMember {
  projectId: string;
  userId: string;
  userEmail: string;
  role: string;
  addedAt: string;
}

export interface ListProjectsResponse {
  projects: Project[];
  pageInfo?: { page: number; pageSize: number; totalCount: string; totalPages: number };
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await apiFetch(path, body);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.message || `${path} failed: ${res.statusText}`);
  }
  return res.json();
}

export const listProjects = (page = 1, pageSize = 20, mineOnly = false) =>
  post<ListProjectsResponse>('/idp.v1.ProjectService/ListProjects', { page: { page, pageSize }, mineOnly });

export const getProject = (slug: string) =>
  post<{ project: Project }>('/idp.v1.ProjectService/GetProject', { slug });

export const createProject = (slug: string, name: string, description = '', labels: Record<string, string> = {}) =>
  post<{ project: Project }>('/idp.v1.ProjectService/CreateProject', { slug, name, description, labels });

export const updateProject = (slug: string, name: string, description = '', labels: Record<string, string> = {}) =>
  post<{ project: Project }>('/idp.v1.ProjectService/UpdateProject', { slug, name, description, labels });

export const deleteProject = (slug: string) =>
  post<Record<string, never>>('/idp.v1.ProjectService/DeleteProject', { slug });

export const listMembers = (projectSlug: string) =>
  post<{ members: ProjectMember[] }>('/idp.v1.ProjectService/ListMembers', { projectSlug });

export interface AddMemberResult {
  member: ProjectMember;
  keycloakUserCreated?: boolean;
  loginUsername?: string;
}

export const addMember = (
  projectSlug: string,
  userEmail: string,
  role: 'developer' | 'viewer',
  opts: { username?: string; password?: string; temporaryPassword?: boolean } = {},
) =>
  post<AddMemberResult>('/idp.v1.ProjectService/AddMember', {
    projectSlug,
    userEmail,
    role,
    username: opts.username ?? '',
    password: opts.password ?? '',
    temporaryPassword: opts.temporaryPassword ?? false,
  });

export const removeMember = (projectSlug: string, userEmail: string) =>
  post<Record<string, never>>('/idp.v1.ProjectService/RemoveMember', { projectSlug, userEmail });
