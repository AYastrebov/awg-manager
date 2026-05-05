<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    children: Snippet;
  }

  let { children }: Props = $props();
</script>

<div class="strip">
  {@render children()}
</div>

<style>
  .strip {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    background: var(--color-bg-tertiary);
    border: 1px solid var(--color-border);
    border-radius: 12px;
    overflow: hidden;
  }
  .strip > :global(:not(:last-child)) {
    border-right: 1px solid var(--color-border);
  }
</style>
