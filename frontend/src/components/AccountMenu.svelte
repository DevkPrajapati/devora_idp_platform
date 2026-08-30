<script lang="ts">
  import { auth } from '$stores/auth';
  import { router } from '$stores/router';
  import { ChevronDown, LogOut } from '@lucide/svelte';

  let open = $state(false);
  let root = $state<HTMLDivElement | null>(null);

  const user = $derived($auth.user);
  const initial = $derived(user?.username?.charAt(0)?.toUpperCase() || '?');
  const roleLabel = $derived(user?.roles?.[0] ? prettyRole(user.roles[0]) : '');

  function prettyRole(role: string) {
    return role
      .replace(/[_-]+/g, ' ')
      .replace(/\b\w/g, (c) => c.toUpperCase());
  }

  function toggle(e: MouseEvent) {
    e.stopPropagation();
    open = !open;
  }

  function close() {
    open = false;
  }

  function onWindowClick(e: MouseEvent) {
    if (!open || !root) return;
    if (!root.contains(e.target as Node)) close();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close();
  }

  function handleLogout() {
    close();
    auth.logout();
    router.navigate('/');
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKeydown} />

{#if user}
  <div class="account" bind:this={root}>
    <button
      type="button"
      class="account-trigger"
      onclick={toggle}
      aria-haspopup="menu"
      aria-expanded={open}
      aria-label="Account menu"
    >
      <span class="account-avatar" aria-hidden="true">{initial}</span>
      <span class="account-meta">
        <span class="account-name">{user.username}</span>
        {#if roleLabel}
          <span class="account-role">{roleLabel}</span>
        {/if}
      </span>
      <span class="account-caret" class:account-caret-open={open} aria-hidden="true">
        <ChevronDown class="h-3.5 w-3.5" />
      </span>
    </button>

    {#if open}
      <div class="account-menu" role="menu" aria-label="Account">
        <div class="account-menu-head">
          <span class="account-avatar account-avatar-lg" aria-hidden="true">{initial}</span>
          <div class="account-menu-id">
            <p class="account-menu-name">{user.username}</p>
            {#if user.email}
              <p class="account-menu-email">{user.email}</p>
            {/if}
            {#if roleLabel}
              <span class="account-badge">{roleLabel}</span>
            {/if}
          </div>
        </div>

        <div class="account-menu-rule"></div>

        <button
          type="button"
          class="account-signout"
          role="menuitem"
          onclick={handleLogout}
        >
          <LogOut class="h-4 w-4" />
          Sign out
        </button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .account {
    position: relative;
  }

  .account-trigger {
    display: inline-flex;
    max-width: 16rem;
    align-items: center;
    gap: 0.55rem;
    height: 2.25rem;
    padding: 0 0.4rem 0 0.3rem;
    border: 1px solid var(--border);
    border-radius: 9999px;
    background: var(--card);
    color: var(--fg);
    cursor: pointer;
    transition:
      background 0.15s ease,
      border-color 0.15s ease;
  }

  .account-trigger:hover {
    background: var(--accent);
  }

  .account-trigger:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .account-avatar {
    display: inline-flex;
    height: 1.6rem;
    width: 1.6rem;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    border-radius: 9999px;
    background: linear-gradient(135deg, #5eb8f6 0%, #a27dff 100%);
    color: #fff;
    font-size: 0.68rem;
    font-weight: 700;
    letter-spacing: 0.02em;
    line-height: 1;
  }

  .account-avatar-lg {
    height: 2.25rem;
    width: 2.25rem;
    font-size: 0.85rem;
  }

  .account-meta {
    display: none;
    min-width: 0;
    flex-direction: column;
    align-items: flex-start;
    line-height: 1.15;
  }

  .account-name {
    max-width: 8.5rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.8125rem;
    font-weight: 600;
  }

  .account-role {
    font-size: 0.65rem;
    font-weight: 500;
    letter-spacing: 0.02em;
    color: var(--muted-fg);
  }

  .account-caret {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--muted-fg);
    transition: transform 0.15s ease;
  }

  .account-caret-open {
    transform: rotate(180deg);
  }

  .account-menu {
    position: absolute;
    top: calc(100% + 0.45rem);
    right: 0;
    z-index: 60;
    width: 16.5rem;
    padding: 0.4rem;
    border: 1px solid var(--border);
    border-radius: 0.85rem;
    background: var(--card);
    box-shadow:
      0 16px 40px -16px rgb(0 0 0 / 0.45),
      0 0 0 1px color-mix(in oklab, var(--border) 70%, transparent);
    animation: account-in 0.16s cubic-bezier(0.22, 1, 0.36, 1) both;
  }

  .account-menu-head {
    display: flex;
    align-items: flex-start;
    gap: 0.7rem;
    padding: 0.55rem 0.55rem 0.7rem;
  }

  .account-menu-id {
    min-width: 0;
    padding-top: 0.05rem;
  }

  .account-menu-name {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--fg);
  }

  .account-menu-email {
    margin: 0.15rem 0 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.75rem;
    color: var(--muted-fg);
  }

  .account-badge {
    display: inline-flex;
    margin-top: 0.4rem;
    padding: 0.12rem 0.45rem;
    border-radius: 9999px;
    border: 1px solid var(--border);
    background: var(--muted);
    font-size: 0.65rem;
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--muted-fg);
  }

  .account-menu-rule {
    height: 1px;
    margin: 0.15rem 0.35rem 0.25rem;
    background: var(--border);
  }

  .account-signout {
    display: flex;
    width: 100%;
    align-items: center;
    gap: 0.55rem;
    padding: 0.55rem 0.65rem;
    border: 0;
    border-radius: 0.5rem;
    background: transparent;
    color: var(--muted-fg);
    font-size: 0.8125rem;
    font-weight: 500;
    cursor: pointer;
    text-align: left;
  }

  .account-signout:hover {
    color: var(--destructive);
    background: color-mix(in oklab, var(--destructive) 8%, transparent);
  }

  @keyframes account-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @media (min-width: 640px) {
    .account-meta {
      display: flex;
    }

    .account-trigger {
      padding-right: 0.55rem;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .account-menu,
    .account-caret {
      animation: none;
      transition: none;
    }
  }
</style>
