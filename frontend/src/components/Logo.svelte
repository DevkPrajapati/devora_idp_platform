<script lang="ts">
  import { cn } from '$lib/utils';

  interface Props {
    /** `mark` = icon only. `lockup` = icon + DEVORA. `full` = lockup + tagline. */
    variant?: 'mark' | 'lockup' | 'full';
    size?: 'sm' | 'md' | 'lg' | 'xl';
    class?: string;
  }

  let { variant = 'lockup', size = 'md', class: className = '' }: Props = $props();

  const uid = $props.id();
  const showWord = $derived(variant !== 'mark');
  const showTagline = $derived(variant === 'full');
</script>

<span
  class={cn(
    'logo',
    `logo-${size}`,
    variant === 'full' && 'logo-full',
    className,
  )}
>
  <svg
    class="logo-mark"
    viewBox="0 0 108 98"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
    focusable="false"
  >
    <defs>
      <linearGradient id={`${uid}-g`} x1="6" y1="14" x2="102" y2="84" gradientUnits="userSpaceOnUse">
        <stop offset="0%" stop-color="#22D3EE" />
        <stop offset="42%" stop-color="#3B82F6" />
        <stop offset="100%" stop-color="#A855F7" />
      </linearGradient>
    </defs>

    <!-- Speed trails -->
    <rect x="26" y="16" width="30" height="12" rx="6" fill={`url(#${uid}-g)`} />
    <rect x="10" y="34" width="46" height="12" rx="6" fill={`url(#${uid}-g)`} />
    <rect x="4" y="52" width="52" height="12" rx="6" fill={`url(#${uid}-g)`} />
    <rect x="18" y="70" width="38" height="12" rx="6" fill={`url(#${uid}-g)`} />

    <!-- D bowl + stem -->
    <path
      fill={`url(#${uid}-g)`}
      fill-rule="evenodd"
      d="M50 16h14c18.5 0 36 12.4 36 33s-17.5 33-36 33H50V16Zm14 15c12.2 0 21 7.2 21 18s-8.8 18-21 18V31Z"
    />

    <!-- Cloud + Kubernetes helm -->
    <g
      fill="none"
      stroke="#fff"
      stroke-width="1.85"
      stroke-linejoin="round"
      stroke-linecap="round"
    >
      <path
        d="M65.5 53.2c-3.1 0-5.6-2.2-5.6-5.2 0-2.6 1.8-4.8 4.2-5.3.6-3.4 3.6-5.9 7.2-5.9 3.2 0 5.9 2 7 4.8 3.2.3 5.7 2.9 5.7 6.1 0 3.4-2.8 6.1-6.3 6.1H65.5Z"
      />
      <g transform="translate(75.2 47.4)">
        <polygon
          points="0,-5.05 3.95,-3.15 4.92,1.12 2.19,4.55 -2.19,4.55 -4.92,1.12 -3.95,-3.15"
        />
        <circle r="1.85" />
        <path
          d="M0 0L0-5.05M0 0L3.95-3.15M0 0L4.92 1.12M0 0L2.19 4.55M0 0L-2.19 4.55M0 0L-4.92 1.12M0 0L-3.95-3.15"
        />
      </g>
    </g>
  </svg>

  {#if showWord}
    <span class="logo-text" data-wordmark>
      <span class="logo-name">
        DEVOR<span class="logo-a" aria-hidden="true">
          <svg viewBox="0 0 22 28" fill="none" xmlns="http://www.w3.org/2000/svg">
            <defs>
              <linearGradient id={`${uid}-a`} x1="11" y1="2" x2="20" y2="26" gradientUnits="userSpaceOnUse">
                <stop offset="0%" stop-color="#22D3EE" />
                <stop offset="55%" stop-color="#3B82F6" />
                <stop offset="100%" stop-color="#A855F7" />
              </linearGradient>
            </defs>
            <path d="M3.2 25.4 L11 2.8" stroke="currentColor" stroke-width="4.15" stroke-linecap="round" />
            <path d="M11 2.8 L18.8 25.4" stroke={`url(#${uid}-a)`} stroke-width="4.15" stroke-linecap="round" />
          </svg>
        </span>
      </span>
      {#if showTagline}
        <span class="logo-tagline">Self-service developer infrastructure platform</span>
      {/if}
    </span>
  {/if}
</span>

<style>
  .logo {
    display: inline-flex;
    align-items: center;
    gap: 0.55rem;
    min-width: 0;
    color: currentColor;
    line-height: 1;
    user-select: none;
  }

  .logo-mark {
    display: block;
    flex-shrink: 0;
    overflow: visible;
  }

  .logo-sm .logo-mark {
    width: 1.75rem;
    height: 1.6rem;
  }

  .logo-md .logo-mark {
    width: 2.05rem;
    height: 1.85rem;
  }

  .logo-lg .logo-mark {
    width: 3.75rem;
    height: 3.4rem;
    filter: drop-shadow(0 0 14px rgba(56, 189, 248, 0.32));
  }

  .logo-xl .logo-mark {
    width: 4.5rem;
    height: 4.1rem;
    filter: drop-shadow(0 0 18px rgba(56, 189, 248, 0.3));
  }

  .logo-text {
    display: flex;
    min-width: 0;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
  }

  .logo-name {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    white-space: nowrap;
    font-family: Inter, ui-sans-serif, system-ui, sans-serif;
    font-weight: 800;
    letter-spacing: -0.045em;
    line-height: 0.9;
    color: currentColor;
  }

  .logo-sm .logo-name {
    font-size: 0.98rem;
  }

  .logo-md .logo-name {
    font-size: 1.12rem;
  }

  .logo-lg .logo-name {
    font-size: clamp(1.85rem, 6vw, 2.55rem);
  }

  .logo-xl .logo-name {
    font-size: clamp(2.15rem, 3.6vw, 2.85rem);
  }

  .logo-a {
    display: inline-flex;
    height: 0.9em;
    width: 0.7em;
    margin-left: -0.02em;
  }

  .logo-a svg {
    display: block;
    width: 100%;
    height: 100%;
    overflow: visible;
  }

  .logo-tagline {
    display: block;
    margin-top: 0.42rem;
    padding-top: 0.38rem;
    border-top: 1px solid color-mix(in oklab, currentColor 22%, transparent);
    font-size: 0.62rem;
    font-weight: 500;
    letter-spacing: 0.1em;
    line-height: 1.45;
    text-transform: uppercase;
    color: color-mix(in oklab, currentColor 58%, transparent);
    max-width: 16.5rem;
  }

  .logo-full {
    gap: 0.85rem;
  }

  /* Header lockup: drop the wordmark when the row is too tight. */
  @media (max-width: 379px) {
    .logo-sm .logo-text {
      display: none;
    }
  }

  @media (max-width: 420px) {
    .logo-full {
      flex-direction: column;
      align-items: center;
      text-align: center;
      gap: 0.7rem;
    }

    .logo-full .logo-text {
      align-items: center;
    }

    .logo-full .logo-tagline {
      text-align: center;
    }
  }
</style>
