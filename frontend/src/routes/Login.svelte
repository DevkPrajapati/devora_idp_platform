<script lang="ts">
  import Logo from '$components/Logo.svelte';
  import ThemeToggle from '$components/ThemeToggle.svelte';
  import { auth } from '$stores/auth';
  import { router } from '$stores/router';
  import {
    Eye,
    EyeOff,
    LoaderCircle,
    ArrowRight,
    AlertCircle,
    Network,
    Container,
    Shield,
    Lock,
  } from '@lucide/svelte';

  let username = $state('');
  let password = $state('');
  let showPassword = $state(false);
  let isSubmitting = $state(false);
  let errorMsg = $state('');

  const authEnabled = auth.isEnabled();

  async function handleLogin(e: Event) {
    e.preventDefault();
    isSubmitting = true;
    errorMsg = '';

    // Read from the form, not only bind:value. Chrome autofill can paint a
    // username/password without firing input events, so Svelte state stays empty
    // and Keycloak returns "Invalid user credentials".
    const form = e.currentTarget as HTMLFormElement;
    const data = new FormData(form);
    const user = String(data.get('username') ?? username).trim();
    const pass = String(data.get('password') ?? password);
    username = user;
    password = pass;

    try {
      await auth.login(user, pass);
      router.navigate('/');
    } catch (err: unknown) {
      errorMsg = err instanceof Error ? err.message : 'Sign-in failed. Check your credentials.';
    } finally {
      isSubmitting = false;
    }
  }

  function handleDevContinue() {
    auth.loginDev();
    router.navigate('/');
  }
</script>

<div class="login-root">
  <aside class="login-showcase">
    <div class="login-showcase-bg" aria-hidden="true">
      <div class="login-showcase-grid"></div>
      <div class="login-showcase-glow login-showcase-glow-a"></div>
      <div class="login-showcase-glow login-showcase-glow-b"></div>
    </div>

    <div class="login-showcase-inner">
      <h1 class="sr-only">DEVORA</h1>
      <Logo variant="lockup" size="xl" />
      <p class="login-kicker">Internal developer platform</p>
      <p class="login-headline">Infrastructure, without the wait.</p>
      <p class="login-lede">
        Provision clusters, ship workloads, and control access from one console — no ticket queue.
      </p>

      <ul class="login-points">
        <li>
          <span class="login-point-icon"><Network class="h-4 w-4" /></span>
          <span>
            <strong>Clusters</strong>
            <span>Connect the fleet and watch live capacity</span>
          </span>
        </li>
        <li>
          <span class="login-point-icon"><Container class="h-4 w-4" /></span>
          <span>
            <strong>Deploy</strong>
            <span>Builds, rollouts, and services in one place</span>
          </span>
        </li>
        <li>
          <span class="login-point-icon"><Shield class="h-4 w-4" /></span>
          <span>
            <strong>Control</strong>
            <span>Namespaces, RBAC, and a full audit trail</span>
          </span>
        </li>
      </ul>
    </div>

    <p class="login-showcase-foot">DEVORA · Self-service developer infrastructure</p>
  </aside>

  <main class="login-stage">
    <div class="login-stage-bar">
      <div class="login-stage-brand">
        <Logo variant="lockup" size="sm" />
      </div>
      <ThemeToggle />
    </div>

    <div class="login-stage-body">
      <section class="login-panel">
        {#if !authEnabled}
          <div class="login-dev">
            <p class="login-dev-copy">Authentication is off in this environment.</p>
            <button type="button" class="login-submit" onclick={handleDevContinue}>
              Continue to console
              <ArrowRight class="h-4 w-4" />
            </button>
          </div>
        {:else}
          <header class="login-panel-head">
            <h2 class="login-panel-title">Sign in</h2>
            <p class="login-panel-sub">Use your platform account to access the console.</p>
          </header>

          <form class="login-form" onsubmit={handleLogin}>
            {#if errorMsg}
              <div class="login-error" role="alert">
                <AlertCircle class="h-4 w-4 shrink-0 mt-0.5" />
                <p>{errorMsg}</p>
              </div>
            {/if}

            <div class="login-field">
              <label for="username">Username</label>
              <input
                id="username"
                name="username"
                type="text"
                required
                autocomplete="username"
                placeholder="Enter username"
                bind:value={username}
                disabled={isSubmitting}
              />
            </div>

            <div class="login-field">
              <label for="password">Password</label>
              <div class="login-password">
                <input
                  id="password"
                  name="password"
                  type={showPassword ? 'text' : 'password'}
                  required
                  autocomplete="current-password"
                  placeholder="Enter password"
                  bind:value={password}
                  disabled={isSubmitting}
                />
                <button
                  type="button"
                  class="login-eye"
                  onclick={() => (showPassword = !showPassword)}
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                  tabindex="-1"
                >
                  {#if showPassword}
                    <EyeOff class="h-4 w-4" />
                  {:else}
                    <Eye class="h-4 w-4" />
                  {/if}
                </button>
              </div>
            </div>

            <button type="submit" class="login-submit" disabled={isSubmitting}>
              {#if isSubmitting}
                <LoaderCircle class="h-4 w-4 animate-spin" />
                Signing in…
              {:else}
                Sign in
                <span class="login-submit-arrow">
                  <ArrowRight class="h-4 w-4" />
                </span>
              {/if}
            </button>
          </form>
        {/if}
      </section>

      <p class="login-foot">
        <Lock class="h-3 w-3" />
        Secure access · Platform credentials only
      </p>
    </div>
  </main>
</div>

<style>
  .login-root {
    display: grid;
    grid-template-columns: minmax(0, 1.08fr) minmax(26rem, 0.92fr);
    min-height: 100dvh;
    background: var(--bg);
    color: var(--fg);
  }

  .login-showcase {
    position: relative;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    overflow-x: hidden;
    overflow-y: auto;
    padding: 3rem 3.25rem 2rem;
    color: #fafafa;
    background: #050505;
    border-right: 1px solid rgba(255, 255, 255, 0.06);
  }

  .login-showcase-bg {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }

  .login-showcase-grid {
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(255, 255, 255, 0.035) 1px, transparent 1px),
      linear-gradient(90deg, rgba(255, 255, 255, 0.035) 1px, transparent 1px);
    background-size: 56px 56px;
    mask-image: radial-gradient(ellipse 80% 70% at 40% 45%, black 10%, transparent 72%);
  }

  .login-showcase-glow {
    position: absolute;
    border-radius: 9999px;
    filter: blur(80px);
    opacity: 0.55;
  }

  .login-showcase-glow-a {
    width: 22rem;
    height: 22rem;
    left: -6rem;
    top: -4rem;
    background: rgba(34, 211, 238, 0.16);
  }

  .login-showcase-glow-b {
    width: 24rem;
    height: 24rem;
    right: -8rem;
    bottom: 10%;
    background: rgba(168, 85, 247, 0.14);
  }

  .login-showcase-inner {
    position: relative;
    z-index: 1;
    max-width: 32rem;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    margin: auto 0;
    animation: rise-in 0.7s cubic-bezier(0.22, 1, 0.36, 1) both;
  }

  .login-showcase-foot {
    position: relative;
    z-index: 1;
    margin: 2rem 0 0;
    font-size: 0.72rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: rgba(250, 250, 250, 0.32);
  }

  .login-kicker {
    margin: 1.35rem 0 0;
    font-size: 0.72rem;
    font-weight: 600;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: rgba(250, 250, 250, 0.48);
  }

  .login-headline {
    margin: 0.7rem 0 0;
    font-size: clamp(1.85rem, 3.2vw, 2.55rem);
    font-weight: 700;
    letter-spacing: -0.04em;
    line-height: 1.12;
    color: #fafafa;
  }

  .login-lede {
    margin: 0.9rem 0 0;
    max-width: 28rem;
    font-size: 0.98rem;
    line-height: 1.55;
    color: rgba(250, 250, 250, 0.58);
  }

  .login-points {
    list-style: none;
    margin: 2rem 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    width: 100%;
    max-width: 26rem;
  }

  .login-points li {
    display: flex;
    align-items: flex-start;
    gap: 0.8rem;
  }

  .login-point-icon {
    display: inline-flex;
    height: 2.1rem;
    width: 2.1rem;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    border-radius: 0.55rem;
    border: 1px solid rgba(255, 255, 255, 0.1);
    background: rgba(255, 255, 255, 0.04);
    color: #67e8f9;
  }

  .login-points strong {
    display: block;
    font-size: 0.875rem;
    font-weight: 600;
    color: #fafafa;
  }

  .login-points li span span {
    display: block;
    margin-top: 0.12rem;
    font-size: 0.8rem;
    line-height: 1.4;
    color: rgba(250, 250, 250, 0.48);
  }

  .login-stage {
    display: flex;
    flex-direction: column;
    min-width: 0;
    background: var(--bg);
  }

  .login-stage-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 1rem 1.25rem 0;
  }

  .login-stage-brand {
    display: none;
    color: var(--fg);
  }

  .login-stage-bar :global(button) {
    margin-left: auto;
  }

  .login-stage-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 1.5rem 1.75rem 2.25rem;
  }

  .login-panel {
    width: 100%;
    max-width: 24rem;
    animation: rise-in 0.65s cubic-bezier(0.22, 1, 0.36, 1) 0.08s both;
  }

  .login-panel-head {
    margin-bottom: 1.5rem;
  }

  .login-panel-title {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 600;
    letter-spacing: -0.03em;
    color: var(--fg);
  }

  .login-panel-sub {
    margin: 0.4rem 0 0;
    font-size: 0.9rem;
    line-height: 1.45;
    color: var(--muted-fg);
    overflow-wrap: break-word;
  }

  .login-form {
    display: flex;
    flex-direction: column;
    gap: 1.05rem;
  }

  .login-field {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .login-field label {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--fg);
  }

  .login-field input,
  .login-password input {
    width: 100%;
    height: 2.75rem;
    border-radius: 0.5rem;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--fg);
    padding: 0 0.85rem;
    font-size: 0.9375rem;
    outline: none;
    transition:
      border-color 0.15s ease,
      box-shadow 0.15s ease;
  }

  .login-password input {
    padding-right: 2.75rem;
  }

  .login-field input::placeholder,
  .login-password input::placeholder {
    color: var(--muted-fg);
    opacity: 0.75;
  }

  .login-field input:focus,
  .login-password input:focus {
    border-color: var(--ring);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--ring) 16%, transparent);
  }

  /* Chrome paints autofill with a light fill + black text, which hides the
     value on a dark form and used to look like the field was empty/wrong. */
  .login-field input:-webkit-autofill,
  .login-field input:-webkit-autofill:hover,
  .login-field input:-webkit-autofill:focus,
  .login-password input:-webkit-autofill,
  .login-password input:-webkit-autofill:hover,
  .login-password input:-webkit-autofill:focus {
    -webkit-text-fill-color: var(--fg);
    caret-color: var(--fg);
    box-shadow: 0 0 0 1000px var(--bg) inset;
    border-color: var(--border);
    transition: background-color 99999s ease-out 0s;
  }

  .login-field input:disabled,
  .login-password input:disabled {
    opacity: 0.65;
  }

  .login-password {
    position: relative;
  }

  .login-eye {
    position: absolute;
    right: 0.4rem;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.35rem;
    border: 0;
    background: transparent;
    color: var(--muted-fg);
    cursor: pointer;
    border-radius: 0.4rem;
  }

  .login-eye:hover {
    color: var(--fg);
    background: var(--accent);
  }

  .login-error {
    display: flex;
    gap: 0.55rem;
    align-items: flex-start;
    padding: 0.75rem 0.85rem;
    border-radius: 0.5rem;
    border: 1px solid color-mix(in oklab, var(--destructive) 40%, var(--border));
    background: color-mix(in oklab, var(--destructive) 10%, var(--bg));
    color: var(--destructive);
    font-size: 0.82rem;
    line-height: 1.4;
  }

  .login-error p {
    margin: 0;
  }

  .login-submit {
    margin-top: 0.25rem;
    display: inline-flex;
    width: 100%;
    height: 2.75rem;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    border: 0;
    border-radius: 0.5rem;
    padding: 0 1rem;
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--primary-fg);
    background: var(--primary);
    cursor: pointer;
    transition:
      opacity 0.15s ease,
      transform 0.15s ease;
  }

  .login-submit:hover:not(:disabled) {
    opacity: 0.92;
  }

  .login-submit:active:not(:disabled) {
    transform: translateY(1px);
  }

  .login-submit:disabled {
    opacity: 0.65;
    cursor: not-allowed;
  }

  .login-submit:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .login-submit:hover:not(:disabled) .login-submit-arrow {
    transform: translateX(3px);
  }

  .login-submit-arrow {
    display: inline-flex;
    transition: transform 0.18s ease;
  }

  .login-dev {
    display: flex;
    flex-direction: column;
    gap: 1.15rem;
  }

  .login-dev-copy {
    margin: 0;
    font-size: 0.95rem;
    line-height: 1.5;
    color: var(--muted-fg);
  }

  .login-foot {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    margin: 1.75rem 0 0;
    font-size: 0.75rem;
    letter-spacing: 0.01em;
    color: var(--muted-fg);
  }

  @keyframes rise-in {
    from {
      opacity: 0;
      transform: translateY(12px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @media (max-height: 740px) {
    .login-points {
      margin-top: 1.25rem;
    }

    .login-showcase {
      padding-top: 2rem;
      padding-bottom: 1.25rem;
    }
  }

  @media (max-width: 959px) {
    .login-root {
      grid-template-columns: 1fr;
    }

    .login-showcase {
      display: none;
    }

    .login-stage-brand {
      display: flex;
    }

    .login-stage-bar {
      padding: 1rem 1.15rem 0;
    }

    .login-stage-body {
      padding: 1.25rem 1.25rem 2rem;
      justify-content: flex-start;
      padding-top: 12vh;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .login-showcase-inner,
    .login-panel {
      animation: none !important;
    }
  }
</style>
