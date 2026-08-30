<script lang="ts">
  import ThemeToggle from '$components/ThemeToggle.svelte';
  import AccountMenu from '$components/AccountMenu.svelte';
  import Logo from '$components/Logo.svelte';
  import { router } from '$stores/router';
  import { sidebarOpen } from '$stores/ui';
  import { Bell, Menu, Search } from '@lucide/svelte';
</script>

<header
  class="relative z-20 flex h-14 shrink-0 items-center justify-between gap-2 border-b border-border bg-card/80 px-3 backdrop-blur-sm sm:px-5 lg:px-6"
>
  <div class="flex min-w-0 items-center gap-2 sm:gap-3">
    <button
      type="button"
      onclick={() => sidebarOpen.toggle()}
      aria-label="Open navigation"
      aria-controls="app-sidebar"
      aria-expanded={$sidebarOpen}
      class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground lg:hidden"
    >
      <Menu class="h-5 w-5" />
    </button>

    <a
      href="/"
      onclick={(e) => {
        e.preventDefault();
        router.navigate('/');
      }}
      aria-label="DEVORA home"
      class="min-w-0 text-foreground lg:hidden"
    >
      <Logo variant="lockup" size="sm" />
    </a>

    <!-- Hidden rather than shrunk on the narrowest screens: at that width the
         field is too small to show a resource name, and the row needs the
         space for the account controls. -->
    <div class="relative hidden sm:block">
      <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <input
        type="search"
        placeholder="Search resources..."
        aria-label="Search resources"
        class="h-9 w-40 rounded-lg border border-border bg-background pl-9 pr-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus:border-foreground focus:ring-1 focus:ring-foreground md:w-52 lg:w-64"
      />
    </div>
  </div>

  <div class="flex shrink-0 items-center gap-1 sm:gap-2">
    <button
      type="button"
      class="hidden h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground sm:inline-flex"
      aria-label="Notifications"
    >
      <Bell class="h-4 w-4" />
    </button>
    <ThemeToggle />
    <AccountMenu />
  </div>
</header>
