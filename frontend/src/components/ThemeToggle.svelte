<script lang="ts">
  import { theme } from '$stores/theme';
  import { Moon, Sun } from '@lucide/svelte';
</script>

<button
  type="button"
  onclick={() => theme.toggle()}
  class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
  aria-label="Toggle theme"
>
  <Sun class="h-4 w-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
  <Moon class="absolute h-4 w-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
</button>
