<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '$components/ui/Modal.svelte';
  import PageHeader from '$components/ui/PageHeader.svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';
  import EmptyState from '$components/ui/EmptyState.svelte';
  import DataTable from '$components/ui/DataTable.svelte';
  import { auth } from '$stores/auth';
  import { toasts, toastError } from '$stores/toast';
  import {
    listProjects,
    createProject,
    deleteProject,
    addMember,
    removeMember,
    listMembers,
    type Project,
    type ProjectMember,
  } from '$services/projects';

  let projects = $state<Project[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);

  let showCreate = $state(false);
  let newSlug = $state('');
  let newName = $state('');
  let newDesc = $state('');

  let membersFor = $state<Project | null>(null);
  let members = $state<ProjectMember[]>([]);
  let addEmail = $state('');
  let addUsername = $state('');
  let addPassword = $state('');
  let addRole = $state<'developer' | 'viewer'>('developer');
  let memberNotice = $state<string | null>(null);

  let deleteTarget = $state<Project | null>(null);
  let deleting = $state(false);

  const isAdmin = $derived($auth.user?.roles.includes('admin') ?? false);

  async function refresh() {
    loading = true;
    error = null;
    try {
      const res = await listProjects(1, 50, false);
      projects = res.projects ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(refresh);

  async function submitCreate(e: Event) {
    e.preventDefault();
    try {
      await createProject(newSlug, newName || newSlug, newDesc);
      showCreate = false;
      newSlug = '';
      newName = '';
      newDesc = '';
      await refresh();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function remove(p: Project) {
    deleteTarget = p;
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    error = null;
    try {
      await deleteProject(deleteTarget.slug);
      toasts.success(`Project “${deleteTarget.slug}” deleted`);
      deleteTarget = null;
      await refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to delete project';
      error = msg;
      toastError(e, 'Failed to delete project');
    } finally {
      deleting = false;
    }
  }

  async function openMembers(p: Project) {
    membersFor = p;
    members = [];
    memberNotice = null;
    addEmail = '';
    addUsername = '';
    addPassword = '';
    try {
      const res = await listMembers(p.slug);
      members = res.members ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function submitAddMember(e: Event) {
    e.preventDefault();
    if (!membersFor) return;
    memberNotice = null;
    try {
      const result = await addMember(membersFor.slug, addEmail, addRole, {
        username: addUsername.trim() || undefined,
        password: addPassword,
      });
      const login = result.loginUsername || addUsername.trim() || addEmail;
      memberNotice = result.keycloakUserCreated
        ? `Keycloak login created. They can sign in as “${login}” with the password you set.`
        : `Member added. Existing Keycloak user “${login}” can sign in (realm role “${addRole}” ensured).`;
      addEmail = '';
      addUsername = '';
      addPassword = '';
      const res = await listMembers(membersFor.slug);
      members = res.members ?? [];
      await refresh();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function removeMemberClick(m: ProjectMember) {
    if (!membersFor) return;
    if (!confirm(`Remove ${m.userEmail} from ${membersFor.slug}?`)) return;
    try {
      await removeMember(membersFor.slug, m.userEmail);
      const res = await listMembers(membersFor.slug);
      members = res.members ?? [];
      await refresh();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
</script>

<!-- No page padding here: AppLayout's <main> already applies it, and the
     duplicate inset pushed the table past the viewport on narrow screens. -->
<div class="page-stack">
  <PageHeader title="Projects" description="Multi-tenant workspaces that own namespaces and members.">
    {#if isAdmin}
      <button
        class="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        onclick={() => (showCreate = true)}
      >
        New Project
      </button>
    {/if}
  </PageHeader>

  {#if error}
    <div class="rounded-lg border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
      {error}
    </div>
  {/if}

  {#if loading}
    <Skeleton variant="table" rows={6} />
  {:else if projects.length === 0}
    <EmptyState
      title="No projects yet"
      description={isAdmin ? 'Click New Project to create one.' : 'Ask an admin to add you to a project.'}
    />
  {:else}
    <DataTable minWidth="52rem">
        <thead class="bg-muted/50 text-left text-xs uppercase text-muted-foreground">
          <tr>
            <th class="px-4 py-2">Slug</th>
            <th class="px-4 py-2">Name</th>
            <th class="px-4 py-2">Owner</th>
            <th class="px-4 py-2">Members</th>
            <th class="px-4 py-2">Namespaces</th>
            <th class="px-4 py-2">Created</th>
            <th class="px-4 py-2 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each projects as p (p.id)}
            <tr class="border-t border-border hover:bg-accent/40">
              <td class="px-4 py-2 font-mono text-xs">{p.slug}</td>
              <td class="px-4 py-2">{p.name}</td>
              <td class="px-4 py-2 text-muted-foreground">{p.ownerEmail}</td>
              <td class="px-4 py-2">{p.memberCount}</td>
              <td class="px-4 py-2">{p.namespaceCount}</td>
              <td class="px-4 py-2 text-muted-foreground">{p.createdAt?.slice(0, 10)}</td>
              <td class="px-4 py-2 text-right space-x-2">
                <button class="text-xs text-primary hover:underline" onclick={() => openMembers(p)}>Members</button>
                {#if isAdmin}
                  <button class="text-xs text-destructive hover:underline" onclick={() => remove(p)}>Delete</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
    </DataTable>
  {/if}
</div>

<Modal open={showCreate} title="New Project" onclose={() => (showCreate = false)}>
  <form id="create-project-form" onsubmit={submitCreate} class="space-y-3">
    <label class="block text-sm">
      <span class="text-muted-foreground">Slug (lowercase, hyphens)</span>
      <input
        class="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
        required
        pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
        bind:value={newSlug}
      />
    </label>
    <label class="block text-sm">
      <span class="text-muted-foreground">Display name</span>
      <input class="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm" bind:value={newName} />
    </label>
    <label class="block text-sm">
      <span class="text-muted-foreground">Description</span>
      <textarea class="mt-1 w-full rounded-md border border-border bg-background px-3 py-2 text-sm" rows="3" bind:value={newDesc}></textarea>
    </label>
  </form>

  {#snippet footer()}
    <button type="button" class="rounded-md border border-border px-3 py-1.5 text-sm" onclick={() => (showCreate = false)}>Cancel</button>
    <button type="submit" form="create-project-form" class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground">Create</button>
  {/snippet}
</Modal>

<Modal
  open={!!membersFor}
  title={membersFor ? `Members — ${membersFor.slug}` : 'Members'}
  size="lg"
  onclose={() => (membersFor = null)}
>
  <div class="max-h-64 overflow-y-auto rounded-md border border-border">
    {#if members.length === 0}
      <p class="p-3 text-sm text-muted-foreground">No members yet.</p>
    {:else}
      <table class="min-w-full text-sm">
        <tbody>
          {#each members as m (m.userEmail)}
            <tr class="border-t border-border first:border-t-0">
              <td class="px-3 py-2">{m.userEmail}</td>
              <td class="px-3 py-2 text-xs text-muted-foreground">{m.role}</td>
              <td class="px-3 py-2 text-right">
                {#if isAdmin}
                  <button class="text-xs text-destructive hover:underline" onclick={() => removeMemberClick(m)}>Remove</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  {#if memberNotice}
    <p class="mt-3 rounded-md border border-emerald-500/30 bg-emerald-500/10 p-2 text-xs text-emerald-700 dark:text-emerald-400">
      {memberNotice}
    </p>
  {/if}

  {#if isAdmin}
    <form onsubmit={submitAddMember} class="mt-4 space-y-2">
      <p class="text-xs text-muted-foreground">
        Creates a Keycloak login (if needed) with the selected realm role, then adds them to this project.
        New users need a password so they can sign in to the IDP console.
      </p>
      <div class="flex flex-wrap gap-2">
        <input
          class="min-w-[12rem] flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm"
          type="email"
          placeholder="user@example.com"
          required
          bind:value={addEmail}
        />
        <input
          class="w-36 rounded-md border border-border bg-background px-3 py-2 text-sm"
          type="text"
          placeholder="username (optional)"
          autocomplete="off"
          bind:value={addUsername}
        />
        <select class="rounded-md border border-border bg-background px-3 py-2 text-sm" bind:value={addRole}>
          <option value="developer">developer</option>
          <option value="viewer">viewer</option>
        </select>
      </div>
      <div class="flex flex-wrap gap-2">
        <input
          class="min-w-[12rem] flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm"
          type="password"
          placeholder="Login password (required for new users)"
          autocomplete="new-password"
          bind:value={addPassword}
        />
        <button type="submit" class="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground">Add member</button>
      </div>
    </form>
  {/if}

  {#snippet footer()}
    <button
      type="button"
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
      onclick={() => (membersFor = null)}
    >
      Close
    </button>
  {/snippet}
</Modal>

<Modal
  open={!!deleteTarget}
  title="Delete project"
  description={deleteTarget
    ? `Remove “${deleteTarget.name}” (${deleteTarget.slug}) from the platform.`
    : ''}
  onclose={() => { if (!deleting) deleteTarget = null; }}
>
  {#if deleteTarget}
    <p class="text-sm text-muted-foreground">
      This also removes
      {deleteTarget.namespaceCount === 1 ? '1 namespace' : `${deleteTarget.namespaceCount} namespaces`}
      and
      {deleteTarget.memberCount === 1 ? '1 member' : `${deleteTarget.memberCount} members`}
      from the platform.
      {#if deleteTarget.namespaceCount > 0}
        Kubernetes namespaces are deleted when a cluster is connected; if the cluster is disconnected they are only unregistered here.
      {/if}
    </p>
  {/if}
  {#snippet footer()}
    <button
      type="button"
      onclick={() => (deleteTarget = null)}
      disabled={deleting}
      class="inline-flex h-9 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent disabled:opacity-50"
    >
      Cancel
    </button>
    <button
      type="button"
      onclick={confirmDelete}
      disabled={deleting || !deleteTarget}
      class="inline-flex h-9 items-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground disabled:opacity-50"
    >
      {deleting ? 'Deleting…' : 'Delete'}
    </button>
  {/snippet}
</Modal>

