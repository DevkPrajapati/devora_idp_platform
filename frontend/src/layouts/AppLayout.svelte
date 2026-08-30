<script lang="ts">
  import Header from '$components/Header.svelte';
  import Sidebar from '$components/Sidebar.svelte';
  import Toaster from '$components/ui/Toaster.svelte';
  import ClusterStatusBanner from '$components/ClusterStatusBanner.svelte';
  import { router } from '$stores/router';
  import { sidebarOpen } from '$stores/ui';
  import type { Snippet } from 'svelte';

  interface Props {
    children?: Snippet;
  }

  let { children }: Props = $props();

  function handleResize() {
    if (window.innerWidth >= 1024) {
      sidebarOpen.close();
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      sidebarOpen.close();
    }
  }

  $effect(() => {
    void $router;
    sidebarOpen.close();
  });

  $effect(() => {
    document.body.style.overflow = $sidebarOpen ? 'hidden' : '';
    return () => {
      document.body.style.overflow = '';
    };
  });
</script>

<svelte:window onresize={handleResize} onkeydown={handleKeydown} />

<div class="flex h-dvh overflow-hidden bg-background">
  {#if $sidebarOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm lg:hidden"
      onclick={() => sidebarOpen.close()}
    ></div>
  {/if}

  <Sidebar />

  <div class="flex min-w-0 flex-1 flex-col overflow-hidden">
    <Header />
    <main class="flex-1 overflow-y-auto">
      <div class="page-enter mx-auto w-full max-w-[1400px] px-4 py-4 sm:px-5 sm:py-5 lg:px-6 lg:py-6">
        <ClusterStatusBanner />
        {@render children?.()}
      </div>
    </main>
  </div>
</div>

<Toaster />
