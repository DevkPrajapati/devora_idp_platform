<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { auth } from '$stores/auth';
  import { listAuditLogs, type AuditLog } from '$services/audit';
  import { createQuery } from '@tanstack/svelte-query';
  import { ScrollText, RefreshCw, ChevronLeft, ChevronRight, Eye, Search, X, ShieldAlert } from '@lucide/svelte';

  // Filters state
  let page = $state(1);
  let searchAction = $state('');
  let searchNamespace = $state('');
  let activeLogDetails = $state<AuditLog | null>(null);

  // Reactive role check
  const isAdmin = $derived($auth.user?.roles.includes('admin') ?? false);

  // Queries (only enabled if admin)
  const auditLogsQuery = createQuery(() => ({
    queryKey: ['audit-logs', page, searchAction, searchNamespace],
    queryFn: () => listAuditLogs(page, 15, searchNamespace, '', searchAction),
    enabled: isAdmin,
    refetchInterval: 10000,
  }));

  function handlePrevPage() {
    if (page > 1) page--;
  }

  function handleNextPage() {
    if (auditLogsQuery.data?.pageInfo && page < auditLogsQuery.data.pageInfo.totalPages) {
      page++;
    }
  }

  function formatDetails(detailsStr: string): string {
    try {
      const parsed = JSON.parse(detailsStr);
      return JSON.stringify(parsed, null, 2);
    } catch {
      return detailsStr || '{}';
    }
  }
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Audit Logs</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Immutable timeline of user actions, API calls, resource alterations, and operation outcomes.
      </p>
    </div>
    {#if isAdmin}
      <button
        onclick={() => auditLogsQuery.refetch()}
        class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="mr-2 h-4 w-4" />
        Refresh
      </button>
    {/if}
  </div>

  {#if !isAdmin}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-destructive/30 bg-destructive/5 p-16 text-center">
      <ShieldAlert class="mb-4 h-12 w-12 text-rose-500" />
      <h3 class="text-lg font-semibold text-foreground">Access Denied</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Only platform administrators are authorized to inspect the system audit trails and compliance records.
      </p>
    </div>
  {:else}
    <!-- Filters Panel -->
    <Card class="bg-card">
      <CardContent class="py-4 flex flex-wrap gap-4 items-center">
        <div class="flex items-center gap-2 border border-input rounded-md px-3 bg-background flex-1 max-w-sm">
          <Search class="h-4 w-4 text-muted-foreground" />
          <input
            type="text"
            placeholder="Filter by action... (e.g. namespace.create)"
            bind:value={searchAction}
            class="h-9 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>

        <div class="flex items-center gap-2 border border-input rounded-md px-3 bg-background flex-1 max-w-xs">
          <Search class="h-4 w-4 text-muted-foreground" />
          <input
            type="text"
            placeholder="Filter by namespace..."
            bind:value={searchNamespace}
            class="h-9 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>
      </CardContent>
    </Card>

    {#if auditLogsQuery.isPending}
      <div class="space-y-4">
        <div class="h-12 w-full animate-pulse rounded bg-muted"></div>
        <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
      </div>
    {:else if auditLogsQuery.error}
      <Card class="border-destructive bg-destructive/5">
        <CardContent class="py-6">
          <p class="text-sm text-destructive font-medium">
            Error loading audit logs: {auditLogsQuery.error.message}
          </p>
        </CardContent>
      </Card>
    {:else if !auditLogsQuery.data || auditLogsQuery.data.logs.length === 0}
      <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
        <ScrollText class="mb-4 h-12 w-12 text-muted-foreground/40" />
        <h3 class="text-lg font-semibold">No audit records</h3>
        <p class="mt-2 text-sm text-muted-foreground max-w-sm">
          Perform deployments or configure namespaces to populate the audit timeline.
        </p>
      </div>
    {:else}
      <div class="border border-border rounded-lg bg-card overflow-x-auto">
        <table class="w-full min-w-[52rem] text-left border-collapse">
          <thead>
            <tr class="border-b border-border bg-muted/40 text-xs font-semibold text-muted-foreground uppercase">
              <th class="px-5 py-3">Timestamp</th>
              <th class="px-5 py-3">User</th>
              <th class="px-5 py-3">Action</th>
              <th class="px-5 py-3">Namespace</th>
              <th class="px-5 py-3">Resource</th>
              <th class="px-5 py-3">Result</th>
              <th class="px-5 py-3 text-right">Metadata</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border text-sm">
            {#each auditLogsQuery.data.logs as log}
              <tr class="hover:bg-accent/20 transition-colors">
                <td class="px-5 py-3.5 text-xs text-muted-foreground">
                  {new Date(log.createdAt).toLocaleString()}
                </td>
                <td class="px-5 py-3.5 font-medium text-foreground">
                  {log.userEmail}
                </td>
                <td class="px-5 py-3.5 font-mono text-xs text-indigo-500 font-semibold">
                  {log.action}
                </td>
                <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">
                  {log.namespace || '—'}
                </td>
                <td class="px-5 py-3.5 text-xs text-muted-foreground">
                  {log.resourceType ? `${log.resourceType}/` : ''}{log.resource || '—'}
                </td>
                <td class="px-5 py-3.5">
                  <span
                    class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize
                    {log.result === 'success' ? 'bg-emerald-500/10 text-emerald-500' : 'bg-rose-500/10 text-rose-500'}"
                  >
                    {log.result}
                  </span>
                </td>
                <td class="px-5 py-3.5 text-right">
                  <button
                    onclick={() => activeLogDetails = log}
                    class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground"
                    title="View Detail JSON"
                  >
                    <Eye class="h-4 w-4" />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>

        <!-- Pagination footer -->
        {#if auditLogsQuery.data.pageInfo}
          <div class="flex items-center justify-between border-t border-border px-5 py-3 text-xs bg-muted/20">
            <p class="text-muted-foreground">
              Total records: <span class="font-medium text-foreground">{auditLogsQuery.data.pageInfo.totalCount}</span>
            </p>
            <div class="flex items-center gap-4">
              <span class="text-muted-foreground">
                Page <span class="font-medium text-foreground">{page}</span> of <span class="font-medium text-foreground">{auditLogsQuery.data.pageInfo.totalPages}</span>
              </span>
              <div class="flex items-center gap-1">
                <button
                  disabled={page <= 1}
                  onclick={handlePrevPage}
                  class="inline-flex h-7 w-7 items-center justify-center rounded border border-input bg-background hover:bg-accent disabled:opacity-50 text-muted-foreground"
                >
                  <ChevronLeft class="h-4 w-4" />
                </button>
                <button
                  disabled={page >= (auditLogsQuery.data.pageInfo?.totalPages ?? 1)}
                  onclick={handleNextPage}
                  class="inline-flex h-7 w-7 items-center justify-center rounded border border-input bg-background hover:bg-accent disabled:opacity-50 text-muted-foreground"
                >
                  <ChevronRight class="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<!-- Details Modal -->
{#if activeLogDetails}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
    <div class="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-lg sm:p-6">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <h2 class="text-base font-semibold">Audit Event Payload</h2>
        <button
          onclick={() => activeLogDetails = null}
          class="rounded-md p-1 hover:bg-accent text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      <div class="mt-4 space-y-3.5">
        <div class="grid grid-cols-2 gap-2 text-xs">
          <div>
            <p class="text-muted-foreground font-medium">Event ID</p>
            <p class="font-mono text-foreground">{activeLogDetails.id}</p>
          </div>
          <div>
            <p class="text-muted-foreground font-medium">Operator ID</p>
            <p class="font-mono text-foreground">{activeLogDetails.userId}</p>
          </div>
        </div>

        <div class="space-y-1.5">
          <p class="text-xs text-muted-foreground font-medium">Operation Payload (Metadata)</p>
          <pre class="rounded-lg bg-zinc-950 p-4 font-mono text-[11px] text-zinc-300 overflow-x-auto border border-zinc-800 leading-normal max-h-[40vh] select-all">{formatDetails(activeLogDetails.details)}</pre>
        </div>
      </div>

      <div class="mt-6 flex justify-end">
        <button
          type="button"
          onclick={() => activeLogDetails = null}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
        >
          Dismiss
        </button>
      </div>
    </div>
  </div>
{/if}
