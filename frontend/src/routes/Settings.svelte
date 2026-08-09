<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import ThemeToggle from '$components/ThemeToggle.svelte';
  import { theme } from '$stores/theme';
  import { getPlatformInfo } from '$services/platform';
  import { createQuery } from '@tanstack/svelte-query';
  import { Settings, Info, Cloud, Laptop } from '@lucide/svelte';

  // Previously five hardcoded literals, two of which were wrong: the app
  // version read 1.0.0 while the build reported 0.1.0, and the Kubernetes
  // version was a constant no cluster had ever confirmed.
  const platformQuery = createQuery(() => ({
    queryKey: ['platform-info'],
    queryFn: getPlatformInfo,
  }));

  const platform = $derived(platformQuery.data);

  /** Backend URL is genuinely a client-side build constant, so it stays local. */
  const backendUrl = import.meta.env.VITE_API_URL ?? 'http://localhost:8090';

  /** Anything the backend could not determine is shown as unknown, not guessed. */
  function orUnknown(value: string | undefined | null): string {
    return value && value.length > 0 ? value : 'Unknown';
  }
</script>

<div class="space-y-6">
  <div>
    <h1 class="text-2xl font-semibold tracking-tight">Settings</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      Configure platform interface parameters and view connection configs.
    </p>
  </div>

  <div class="grid gap-4 md:grid-cols-2">
    <!-- UI Settings -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Laptop class="h-4 w-4 text-muted-foreground" />
          <CardTitle>Interface Settings</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <div class="flex items-center justify-between border-b border-border pb-3">
          <div>
            <p class="text-sm font-medium">Dark Mode / Color Theme</p>
            <p class="text-xs text-muted-foreground">Select preferred interface appearance</p>
          </div>
          <ThemeToggle />
        </div>
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium">Active Theme Store State</p>
            <p class="text-xs text-muted-foreground">Resolved from localStorage</p>
          </div>
          <span class="text-xs font-semibold capitalize bg-secondary px-2.5 py-1 rounded">
            {$theme}
          </span>
        </div>
      </CardContent>
    </Card>

    <!-- Platform Telemetry Context -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Cloud class="h-4 w-4 text-muted-foreground" />
          <CardTitle>Platform Configuration</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-3.5 text-sm">
        {#if platformQuery.isPending}
          <p class="py-2 text-xs text-muted-foreground">Loading platform configuration…</p>
        {:else if platformQuery.isError}
          <p class="py-2 text-xs text-destructive">
            Could not reach the platform API. Values below are unavailable rather than assumed.
          </p>
        {:else}
          <div class="flex justify-between border-b border-border pb-2.5">
            <span class="text-muted-foreground">API Server URL</span>
            <span class="font-mono text-xs font-medium">{backendUrl}</span>
          </div>
          <div class="flex justify-between gap-4 border-b border-border pb-2.5">
            <span class="shrink-0 text-muted-foreground">Identity Issuer</span>
            <span class="truncate font-mono text-xs font-medium">{orUnknown(platform?.authIssuer)}</span>
          </div>
          <div class="flex justify-between border-b border-border pb-2.5">
            <span class="text-muted-foreground">Cluster Orchestration</span>
            <span class="font-medium">
              {#if !platform?.clusterConnected}
                <span class="text-amber-500">Not connected</span>
              {:else}
                Kubernetes {orUnknown(platform?.kubernetesVersion)}
              {/if}
            </span>
          </div>
          <div class="flex justify-between border-b border-border pb-2.5">
            <span class="text-muted-foreground">Authentication</span>
            <span class="font-medium">
              {#if platform?.authEnabled}
                <span class="text-success">Enforced</span>
              {:else}
                <!-- Worth calling out loudly: with auth off every request is
                     served as an administrator. -->
                <span class="text-destructive">Disabled (development only)</span>
              {/if}
            </span>
          </div>
          <div class="flex justify-between border-b border-border pb-2.5">
            <span class="text-muted-foreground">Git builds</span>
            <span class="font-medium">{platform?.buildsEnabled ? 'Enabled' : 'Disabled'}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-muted-foreground">Active Environment</span>
            <span class="font-medium capitalize text-primary">{orUnknown(platform?.environment)}</span>
          </div>
        {/if}
      </CardContent>
    </Card>
  </div>

  <!-- Version details -->
  <Card>
    <CardHeader>
      <div class="flex items-center gap-2">
        <Info class="h-4 w-4 text-muted-foreground" />
        <CardTitle>System Information</CardTitle>
      </div>
    </CardHeader>
    <CardContent class="space-y-2.5">
      <p class="text-sm">
        IDP Platform is an internal self-service portal built on top of standard Connect RPC API and client-go primitives.
      </p>
      <p class="text-xs text-muted-foreground">
        Version: {orUnknown(platform?.version)} &middot; reported by the running backend.
      </p>
    </CardContent>
  </Card>
</div>
