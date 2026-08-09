<script lang="ts">
  import { onMount } from 'svelte';
  import { auth } from '$stores/auth';
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
    if (!confirm(`Delete project "${p.slug}"? This cannot be undone.`)) return;
    try {
      await deleteProject(p.slug);
      await refresh();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
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
<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold">Projects</h1>
      <p class="text-sm text-muted-foreground">Multi-tenant workspaces that own namespaces and members.</p>
    </div>
    {#if isAdmin}
      <button
        class="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        onclick={() => (showCreate = true)}
      >
        New Project
      </button>
    {/if}
  </div>

  {#if error}
    <div class="rounded-lg border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
      {error}
    </div>
  {/if}

  {#if loading}
    <p class="text-sm text-muted-foreground">Loading…</p>
  {:else if projects.length === 0}
    <div class="rounded-lg border border-border p-8 text-center text-sm text-muted-foreground">
      No projects yet.{#if isAdmin} Click <strong>New Project</strong> to create one.{/if}
    </div>
  {:else}
    <div class="overflow-x-auto rounded-lg border border-border">
      <table class="min-w-[56rem] w-full text-sm">
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
      </table>
    </div>
  {/if}
</div>

{#if showCreate}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
    <div class="w-full max-w-md rounded-lg border border-border bg-card p-6">
      <h2 class="text-lg font-semibold">New Project</h2>
      <form onsubmit={submitCreate} class="mt-4 space-y-3">
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
        <div class="flex justify-end gap-2 pt-2">
          <button type="button" class="rounded-md border border-border px-3 py-1.5 text-sm" onclick={() => (showCreate = false)}>Cancel</button>
          <button type="submit" class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground">Create</button>
        </div>
      </form>
    </div>
  </div>
{/if}

{#if membersFor}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
    <div class="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg border border-border bg-card p-4 sm:p-6">
      <div class="flex items-center justify-between">
        <h2 class="text-lg font-semibold">Members — {membersFor.slug}</h2>
        <button class="text-sm text-muted-foreground" onclick={() => (membersFor = null)}>Close</button>
      </div>

      <div class="mt-4 max-h-64 overflow-y-auto rounded-md border border-border">
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
    </div>
  </div>
{/if}
