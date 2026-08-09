<script lang="ts">
  import { auth } from '$stores/auth';
  import { router } from '$stores/router';
  import { Eye, EyeOff, LoaderCircle, ArrowRight, AlertCircle } from '@lucide/svelte';

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

    try {
      await auth.login(username, password);
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
  <div class="login-atmosphere" aria-hidden="true">
    <div class="login-grid"></div>
    <div class="login-glow login-glow-a"></div>
    <div class="login-glow login-glow-b"></div>
    <div class="login-orbit"></div>
  </div>

  <main class="login-main">
    <div class="login-brand animate-in">
      <div class="login-mark" aria-hidden="true">
        <svg viewBox="0 0 40 40" class="login-mark-svg">
          <rect x="4" y="4" width="32" height="32" rx="8" fill="currentColor" opacity="0.12" />
          <path
            d="M12 26V14h5.2c3.1 0 5 1.6 5 4.1 0 1.6-.8 2.9-2.2 3.5L24.8 26h-3.4l-4.2-4.1H15V26H12Zm3-6.8h2c1.5 0 2.3-.7 2.3-1.8S18.5 15.6 17 15.6h-2v3.6Z"
            fill="currentColor"
          />
          <path d="M27 14h3v12h-3V14Z" fill="currentColor" opacity="0.85" />
        </svg>
      </div>
      <h1 class="login-title">IDP</h1>
      <p class="login-tagline">Internal Developer Platform</p>
    </div>

    <section class="login-panel animate-in delay-1">
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
              <ArrowRight class="h-4 w-4 login-submit-arrow" />
            {/if}
          </button>
        </form>
      {/if}
    </section>

    <p class="login-foot animate-in delay-2">Secure access · Platform credentials only</p>
  </main>
</div>

<style>
  .login-root {
    position: relative;
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem 1.25rem;
    overflow: hidden;
    color: #e8ecef;
    background: #0a1210;
  }

  .login-atmosphere {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }

  .login-grid {
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(16, 185, 129, 0.06) 1px, transparent 1px),
      linear-gradient(90deg, rgba(16, 185, 129, 0.06) 1px, transparent 1px);
    background-size: 48px 48px;
    mask-image: radial-gradient(ellipse 70% 60% at 50% 40%, black 20%, transparent 75%);
    animation: grid-drift 28s linear infinite;
  }

  .login-glow {
    position: absolute;
    border-radius: 9999px;
    filter: blur(80px);
  }

  .login-glow-a {
    width: 42vw;
    height: 42vw;
    max-width: 520px;
    max-height: 520px;
    top: -12%;
    left: 50%;
    transform: translateX(-60%);
    background: rgba(16, 185, 129, 0.22);
    animation: glow-pulse 8s ease-in-out infinite;
  }

  .login-glow-b {
    width: 36vw;
    height: 36vw;
    max-width: 420px;
    max-height: 420px;
    bottom: -18%;
    right: 8%;
    background: rgba(6, 95, 70, 0.35);
    animation: glow-pulse 10s ease-in-out infinite reverse;
  }

  .login-orbit {
    position: absolute;
    width: min(720px, 90vw);
    height: min(720px, 90vw);
    left: 50%;
    top: 42%;
    transform: translate(-50%, -50%);
    border: 1px solid rgba(16, 185, 129, 0.1);
    border-radius: 9999px;
    animation: orbit-spin 48s linear infinite;
  }

  .login-orbit::after {
    content: '';
    position: absolute;
    width: 8px;
    height: 8px;
    border-radius: 9999px;
    background: #34d399;
    top: 8%;
    left: 50%;
    box-shadow: 0 0 16px rgba(52, 211, 153, 0.8);
  }

  .login-main {
    position: relative;
    z-index: 1;
    width: 100%;
    max-width: 420px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1.75rem;
  }

  .login-brand {
    text-align: center;
  }

  .login-mark {
    display: inline-flex;
    color: #34d399;
    margin-bottom: 0.85rem;
  }

  .login-mark-svg {
    width: 3.25rem;
    height: 3.25rem;
  }

  .login-title {
    margin: 0;
    font-size: clamp(2.75rem, 8vw, 3.75rem);
    font-weight: 700;
    letter-spacing: -0.06em;
    line-height: 1;
    color: #f4faf7;
  }

  .login-tagline {
    margin: 0.55rem 0 0;
    font-size: 0.95rem;
    letter-spacing: 0.04em;
    color: rgba(167, 198, 184, 0.85);
  }

  .login-panel {
    width: 100%;
    padding: 1.75rem 1.5rem 1.5rem;
    border-radius: 1rem;
    border: 1px solid rgba(52, 211, 153, 0.14);
    background: linear-gradient(180deg, rgba(17, 28, 24, 0.92), rgba(10, 18, 16, 0.88));
    backdrop-filter: blur(12px);
    box-shadow: 0 24px 60px rgba(0, 0, 0, 0.35);
  }

  .login-panel-head {
    margin-bottom: 1.35rem;
  }

  .login-panel-title {
    margin: 0;
    font-size: 1.15rem;
    font-weight: 600;
    color: #f0f7f3;
  }

  .login-panel-sub {
    margin: 0.35rem 0 0;
    font-size: 0.875rem;
    color: rgba(148, 173, 162, 0.95);
  }

  .login-form {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .login-field {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .login-field label {
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: rgba(148, 173, 162, 0.95);
  }

  .login-field input,
  .login-password input {
    width: 100%;
    border-radius: 0.65rem;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(5, 12, 10, 0.65);
    color: #f4faf7;
    padding: 0.7rem 0.85rem;
    font-size: 0.95rem;
    outline: none;
    transition:
      border-color 0.2s ease,
      box-shadow 0.2s ease,
      background 0.2s ease;
  }

  .login-password input {
    padding-right: 2.75rem;
  }

  .login-field input::placeholder,
  .login-password input::placeholder {
    color: rgba(100, 120, 112, 0.9);
  }

  .login-field input:focus,
  .login-password input:focus {
    border-color: rgba(52, 211, 153, 0.55);
    box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.15);
    background: rgba(5, 12, 10, 0.9);
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
    right: 0.55rem;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.35rem;
    border: 0;
    background: transparent;
    color: rgba(148, 173, 162, 0.95);
    cursor: pointer;
    border-radius: 0.4rem;
  }

  .login-eye:hover {
    color: #d1fae5;
    background: rgba(255, 255, 255, 0.04);
  }

  .login-error {
    display: flex;
    gap: 0.55rem;
    align-items: flex-start;
    padding: 0.75rem 0.85rem;
    border-radius: 0.65rem;
    border: 1px solid rgba(244, 63, 94, 0.28);
    background: rgba(127, 29, 29, 0.28);
    color: #fecdd3;
    font-size: 0.82rem;
    line-height: 1.4;
  }

  .login-submit {
    margin-top: 0.35rem;
    display: inline-flex;
    width: 100%;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    border: 0;
    border-radius: 0.65rem;
    padding: 0.8rem 1rem;
    font-size: 0.95rem;
    font-weight: 600;
    color: #042f1e;
    background: linear-gradient(180deg, #34d399 0%, #10b981 100%);
    cursor: pointer;
    transition:
      transform 0.18s ease,
      filter 0.18s ease,
      opacity 0.18s ease;
  }

  .login-submit:hover:not(:disabled) {
    filter: brightness(1.05);
    transform: translateY(-1px);
  }

  .login-submit:active:not(:disabled) {
    transform: translateY(0);
  }

  .login-submit:disabled {
    opacity: 0.65;
    cursor: not-allowed;
  }

  .login-submit:hover:not(:disabled) .login-submit-arrow {
    transform: translateX(3px);
  }

  .login-submit-arrow {
    transition: transform 0.18s ease;
  }

  .login-dev {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .login-dev-copy {
    margin: 0;
    font-size: 0.9rem;
    color: rgba(167, 198, 184, 0.9);
  }

  .login-foot {
    margin: 0;
    font-size: 0.75rem;
    letter-spacing: 0.04em;
    color: rgba(120, 145, 134, 0.85);
  }

  .animate-in {
    animation: rise-in 0.65s cubic-bezier(0.22, 1, 0.36, 1) both;
  }

  .delay-1 {
    animation-delay: 0.12s;
  }

  .delay-2 {
    animation-delay: 0.22s;
  }

  @keyframes rise-in {
    from {
      opacity: 0;
      transform: translateY(14px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @keyframes glow-pulse {
    0%,
    100% {
      opacity: 0.7;
      transform: translateX(-60%) scale(1);
    }
    50% {
      opacity: 1;
      transform: translateX(-60%) scale(1.06);
    }
  }

  .login-glow-b {
    animation-name: glow-pulse-b;
  }

  @keyframes glow-pulse-b {
    0%,
    100% {
      opacity: 0.55;
      transform: scale(1);
    }
    50% {
      opacity: 0.9;
      transform: scale(1.08);
    }
  }

  @keyframes grid-drift {
    from {
      background-position: 0 0;
    }
    to {
      background-position: 48px 48px;
    }
  }

  @keyframes orbit-spin {
    from {
      transform: translate(-50%, -50%) rotate(0deg);
    }
    to {
      transform: translate(-50%, -50%) rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .login-grid,
    .login-glow,
    .login-orbit,
    .animate-in {
      animation: none !important;
    }
  }
</style>
