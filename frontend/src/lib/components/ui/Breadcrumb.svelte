<script lang="ts" module>
  export interface BreadcrumbItem {
    label: string;
    href?: string;
  }
</script>

<script lang="ts">
  import Icon from './Icon.svelte';
  import { goto } from '$app/navigation';

  interface Props {
    items: BreadcrumbItem[];
    class?: string;
  }

  let { items, class: cls = '' }: Props = $props();

  function handleClick(e: MouseEvent, href: string | undefined) {
    if (!href) return;
    if (e.metaKey || e.ctrlKey || e.button !== 0) return;
    e.preventDefault();
    goto(href);
  }
</script>

<nav class="breadcrumb {cls}" aria-label="Breadcrumb">
  {#each items as item, i (i)}
    {#if i > 0}
      <Icon name="chevron-right" size={12} color="var(--color-text-muted-soft)" />
    {/if}
    {#if item.href}
      <a class="crumb link" href={item.href} onclick={(e) => handleClick(e, item.href)}>
        {item.label}
      </a>
    {:else}
      <span class="crumb current">{item.label}</span>
    {/if}
  {/each}
</nav>

<style>
  .breadcrumb {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font: 500 13px/1 var(--font-sans);
  }
  .crumb { display: inline-flex; align-items: center; }
  .crumb.link {
    color: var(--color-text-muted);
    text-decoration: none;
    cursor: pointer;
    transition: color 120ms ease;
  }
  .crumb.link:hover { color: var(--color-text-primary); }
  .crumb.current {
    font-family: var(--font-mono);
    font-weight: 500;
    color: var(--color-text-secondary);
  }
</style>
