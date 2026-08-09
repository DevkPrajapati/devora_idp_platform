<script lang="ts">
  import Header from '$components/Header.svelte';
  import Sidebar from '$components/Sidebar.svelte';
  import Toaster from '$components/ui/Toaster.svelte';
  import { router } from '$stores/router';
  import { sidebarOpen } from '$stores/ui';
  import type { Snippet } from 'svelte';

  interface Props {
    children?: Snippet;
  }

  let { children }: Props = $props();

  // The drawer is only presented below `lg`. Resizing past that breakpoint
  // turns the sidebar back into a static column, so a drawer left open would
  // otherwise keep the scroll lock and backdrop active over a desktop layout.
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

  // Navigating from inside the drawer should dismiss it, otherwise the new
  // page renders behind a backdrop the user has to close by hand.
  $effect(() => {
    void $router;
    sidebarOpen.close();
  });

  // Without this the page behind the backdrop scrolls under touch gestures.
  $effect(() => {
    document.body.style.overflow = $sidebarOpen ? 'hidden' : '';
    return () => {
      document.body.style.overflow = '';
    };
  });
</script>

<svelte:window onresize={handleResize} onkeydown={handleKeydown} />

<!-- `h-dvh` tracks the visual viewport so mobile browser chrome cannot clip
     the bottom of the scroll container the way a fixed `h-screen` does. -->
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

  <!-- `min-w-0` lets the column shrink below its content width, which is what
       allows the scroll containers around wide tables to actually scroll
       instead of stretching the whole page. -->
  <div class="flex min-w-0 flex-1 flex-col overflow-hidden">
    <Header />
    <main class="flex-1 overflow-y-auto p-4 sm:p-6">
      {@render children?.()}
    </main>
  </div>
</div>

<!-- Mounted outside <main> so a route change does not tear down the live
     region, and so a toast raised by the outgoing page still gets announced. -->
<Toaster />
