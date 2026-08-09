<script lang="ts">
  import ThemeToggle from '$components/ThemeToggle.svelte';
  import { auth } from '$stores/auth';
  import { router } from '$stores/router';
  import { sidebarOpen } from '$stores/ui';
  import { Bell, Menu, Search, LogOut } from '@lucide/svelte';

  const user = $derived($auth.user);

  // Falls back rather than indexing directly: a token whose
  // `preferred_username` claim is absent leaves this an empty string, and
  // `''[0]` is undefined, which renders as the literal text "undefined".
  const initial = $derived(user?.username?.charAt(0) || '?');

  function handleLogout() {
    auth.logout();
    router.navigate('/');
  }
</script>

<header
  class="flex h-14 shrink-0 items-center justify-between gap-2 border-b border-border bg-card/50 px-3 backdrop-blur-sm sm:px-6"
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

    <!-- Hidden rather than shrunk on the narrowest screens: at that width the
         field is too small to show a resource name, and the row needs the
         space for the account controls. -->
    <div class="relative hidden sm:block">
      <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <input
        type="search"
        placeholder="Search resources..."
        aria-label="Search resources"
        class="h-9 w-40 rounded-lg border border-border bg-background pl-9 pr-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary md:w-56 lg:w-64"
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

    {#if user}
      <div
        class="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-2 py-1.5 sm:ml-1 sm:px-3"
        title={user.username}
      >
        <div
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-semibold uppercase text-primary-foreground"
        >
          {initial}
        </div>
        <div class="hidden min-w-0 md:block">
          <span class="block max-w-[10rem] truncate text-sm font-medium text-foreground">
            {user.username}
          </span>
          {#if user.roles.length > 0}
            <span class="block truncate text-[10px] uppercase tracking-wide text-muted-foreground">
              {user.roles.join(', ')}
            </span>
          {/if}
        </div>
      </div>

      <button
        onclick={handleLogout}
        type="button"
        class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
        aria-label="Sign out"
        title="Sign Out"
      >
        <LogOut class="h-4 w-4" />
      </button>
    {/if}
  </div>
</header>
