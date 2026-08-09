<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { auth } from '$stores/auth';
  import { Shield, CheckCircle2, User, Key, Lock, ShieldAlert } from '@lucide/svelte';

  // Reactively track the actual logged-in user from the auth store
  const user = $derived($auth.user);
  const token = $derived($auth.token);

  // Check if active roles map to matrix items
  function hasRole(roleName: string): boolean {
    if (!user) return false;
    return user.roles.some(r => r.toLowerCase() === roleName.toLowerCase());
  }

  const rolesPrivilegeMatrix = $derived([
    {
      role: 'Admin',
      description: 'Full control of all cluster namespaces, configurations, platform services, and audit parameters.',
      privileges: ['Create/Delete Namespaces', 'Create/Update/Scale/Restart/Delete Deployments', 'Read Audit Logs', 'Assign RBAC policies'],
      active: hasRole('admin')
    },
    {
      role: 'Developer',
      description: 'Deploy, restart, and scale workloads inside assigned tenant namespaces.',
      privileges: ['Scale/Restart Deployments', 'Create/Update Deployments', 'View Services & Workloads'],
      active: hasRole('developer')
    },
    {
      role: 'Viewer',
      description: 'Read-only access to cluster services, deployment configurations, and general metrics.',
      privileges: ['View Workloads', 'View Metrics', 'View Services'],
      active: hasRole('viewer')
    }
  ]);
</script>

<div class="space-y-6">
  <div>
    <h1 class="text-2xl font-semibold tracking-tight">Access Control (RBAC)</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      View identity policies, verify active token claims, and check role permissions.
    </p>
  </div>

  <!-- Active Identity Details -->
  {#if user}
    <Card class="border-primary/20 bg-primary/5">
      <CardHeader>
        <div class="flex items-center gap-2.5">
          <Shield class="h-5 w-5 text-primary" />
          <CardTitle class="text-base font-semibold">Active User Identity Claims</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div class="space-y-1">
          <span class="text-xs text-muted-foreground flex items-center gap-1">
            <User class="h-3.5 w-3.5" /> Full Name / Username
          </span>
          <p class="text-sm font-semibold text-foreground">{user.username}</p>
        </div>
        <div class="space-y-1">
          <span class="text-xs text-muted-foreground flex items-center gap-1">
            <Key class="h-3.5 w-3.5" /> Email Scope
          </span>
          <p class="text-sm font-semibold text-foreground">{user.email}</p>
        </div>
        <div class="space-y-1">
          <span class="text-xs text-muted-foreground flex items-center gap-1">
            <Lock class="h-3.5 w-3.5" /> Auth Provider
          </span>
          <p class="text-sm font-semibold text-foreground">
            {token ? 'Keycloak OIDC' : 'Developer Mock Mode'}
          </p>
        </div>
        <div class="space-y-1">
          <span class="text-xs text-muted-foreground flex items-center gap-1">
            <Shield class="h-3.5 w-3.5" /> Active Roles
          </span>
          <div class="flex flex-wrap gap-1.5 mt-0.5">
            {#each user.roles as r}
              <span class="inline-flex items-center rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary capitalize">
                {r}
              </span>
            {/each}
          </div>
        </div>
      </CardContent>
    </Card>
  {:else}
    <Card class="border-amber-500/20 bg-amber-500/5">
      <CardContent class="py-6 flex items-center gap-3">
        <ShieldAlert class="h-6 w-6 text-amber-500 shrink-0" />
        <div>
          <h3 class="text-sm font-semibold">Session Disconnected</h3>
          <p class="text-xs text-muted-foreground mt-0.5">No active identity claims could be retrieved.</p>
        </div>
      </CardContent>
    </Card>
  {/if}

  <!-- Platform Roles Matrix -->
  <div class="space-y-4">
    <h2 class="text-lg font-semibold">Privilege Matrix</h2>
    <div class="grid gap-4 md:grid-cols-3">
      {#each rolesPrivilegeMatrix as entry}
        <Card class={entry.active ? 'border-primary ring-1 ring-primary/20 bg-primary/5' : 'opacity-60 bg-muted/20 border-dashed border-border'}>
          <CardHeader>
            <div class="flex items-center justify-between">
              <CardTitle class="text-base font-semibold">{entry.role}</CardTitle>
              {#if entry.active}
                <span class="inline-flex h-2 w-2 rounded-full bg-emerald-500 ring-4 ring-emerald-500/20 animate-pulse"></span>
              {:else}
                <span class="inline-flex h-2 w-2 rounded-full bg-muted-foreground/45"></span>
              {/if}
            </div>
            <p class="text-xs text-muted-foreground mt-1.5 leading-normal">
              {entry.description}
            </p>
          </CardHeader>
          <CardContent class="border-t border-border pt-4">
            <p class="text-xs font-semibold text-foreground uppercase tracking-wider mb-2">Capabilities</p>
            <ul class="space-y-1.5">
              {#each entry.privileges as priv}
                <li class="flex items-start gap-2 text-xs text-muted-foreground">
                  <CheckCircle2 class="h-3.5 w-3.5 text-emerald-500 shrink-0 mt-0.5" />
                  <span>{priv}</span>
                </li>
              {/each}
            </ul>
          </CardContent>
        </Card>
      {/each}
    </div>
  </div>
</div>
