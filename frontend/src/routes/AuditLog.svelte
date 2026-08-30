<script lang="ts">
  import PageHeader from '$components/ui/PageHeader.svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';
  import EmptyState from '$components/ui/EmptyState.svelte';
  import DataTable from '$components/ui/DataTable.svelte';
  import Pagination from '$components/ui/Pagination.svelte';
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import { auth } from '$stores/auth';
  import { listAuditLogs, type AuditLog } from '$services/audit';
  import { createQuery } from '@tanstack/svelte-query';
  import { ScrollText, RefreshCw, Eye, Search, X, ShieldAlert } from '@lucide/svelte';
  import Modal from '$components/ui/Modal.svelte';

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
    refetchInterval: 20000,
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

<div class="page-stack">
  <PageHeader
    title="Audit Logs"
    description="Immutable timeline of user actions, API calls, resource alterations, and operation outcomes."
  >
    {#if isAdmin}
      <button
        onclick={() => auditLogsQuery.refetch()}
        class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="mr-2 h-4 w-4" />
        Refresh
      </button>
    {/if}
  </PageHeader>

  {#if !isAdmin}
    <EmptyState
      icon={ShieldAlert}
      title="Access Denied"
      description="Only platform administrators are authorized to inspect the system audit trails and compliance records."
    />
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
      <Skeleton variant="table" rows={8} />
    {:else if auditLogsQuery.error}
      <Card class="border-destructive bg-destructive/5">
        <CardContent class="py-6">
          <p class="text-sm text-destructive font-medium">
            Error loading audit logs: {auditLogsQuery.error.message}
          </p>
        </CardContent>
      </Card>
    {:else if !auditLogsQuery.data || auditLogsQuery.data.logs.length === 0}
      <EmptyState
        icon={ScrollText}
        title="No audit records"
        description="Perform deployments or configure namespaces to populate the audit timeline."
      />
    {:else}
      <div class="overflow-hidden rounded-xl border border-border bg-card">
        <DataTable minWidth="52rem" class="rounded-none border-0">
          <thead>
            <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              <th class="px-4 py-3">Timestamp</th>
              <th class="px-4 py-3">User</th>
              <th class="px-4 py-3">Action</th>
              <th class="px-4 py-3">Namespace</th>
              <th class="px-4 py-3">Resource</th>
              <th class="px-4 py-3">Result</th>
              <th class="px-4 py-3 text-right">Metadata</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border text-sm">
            {#each auditLogsQuery.data.logs as log}
              <tr class="hover:bg-accent/20 transition-colors">
                <td class="px-4 py-3 text-xs text-muted-foreground">
                  {new Date(log.createdAt).toLocaleString()}
                </td>
                <td class="px-4 py-3 font-medium text-foreground">
                  {log.userEmail}
                </td>
                <td class="px-4 py-3 font-mono text-xs font-semibold text-foreground">
                  {log.action}
                </td>
                <td class="px-4 py-3 font-mono text-xs text-muted-foreground">
                  {log.namespace || '—'}
                </td>
                <td class="px-4 py-3 text-xs text-muted-foreground">
                  {log.resourceType ? `${log.resourceType}/` : ''}{log.resource || '—'}
                </td>
                <td class="px-4 py-3">
                  <span
                    class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize
                    {log.result === 'success' ? 'bg-emerald-500/10 text-emerald-500' : 'bg-rose-500/10 text-rose-500'}"
                  >
                    {log.result}
                  </span>
                </td>
                <td class="px-4 py-3 text-right">
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
        </DataTable>

        {#if auditLogsQuery.data.pageInfo}
          <Pagination
            page={page}
            totalPages={auditLogsQuery.data.pageInfo.totalPages}
            totalCount={auditLogsQuery.data.pageInfo.totalCount}
            onprev={handlePrevPage}
            onnext={handleNextPage}
          />
        {/if}
      </div>
    {/if}
  {/if}
</div>

<!-- Details Modal -->
<Modal
  open={!!activeLogDetails}
  title="Audit Event Payload"
  description="Raw metadata for this console action."
  size="xl"
  onclose={() => (activeLogDetails = null)}
>
  {#if activeLogDetails}
    <div class="space-y-3.5">
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
        <pre class="log-surface log-console select-all overflow-auto rounded-lg border p-4 font-mono text-[11px] leading-normal">{formatDetails(activeLogDetails.details)}</pre>
      </div>
    </div>
  {/if}

  {#snippet footer()}
    <button
      type="button"
      onclick={() => (activeLogDetails = null)}
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Dismiss
    </button>
  {/snippet}
</Modal>
