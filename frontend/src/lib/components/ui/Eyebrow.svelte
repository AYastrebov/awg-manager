<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    color?: 'muted' | 'yellow' | 'success' | 'error';
    children: Snippet;
  }

  let { color = 'muted', children }: Props = $props();
</script>

<span class="eyebrow" data-color={color}>
  {@render children()}
</span>

<style>
  .eyebrow {
    font: 600 12px/1.4 var(--font-sans);
    letter-spacing: 1.5px;
    text-transform: uppercase;
  }
  .eyebrow[data-color='muted']   { color: var(--color-text-muted); }
  .eyebrow[data-color='yellow']  { color: var(--color-yellow); }
  .eyebrow[data-color='success'] { color: var(--color-success); }
  .eyebrow[data-color='error']   { color: var(--color-error); }
</style>
