<script lang="ts" module>
  import { TABLER_ICONS, type TablerIconName } from './tabler-icons';
  export type { TablerIconName };
</script>

<script lang="ts">
  interface Props {
    name: TablerIconName | string;
    size?: number;
    stroke?: number;
    color?: string;
    title?: string;
    class?: string;
  }

  let {
    name,
    size = 16,
    stroke = 1.75,
    color = 'currentColor',
    title,
    class: cls = '',
  }: Props = $props();

  function toPascal(s: string): string {
    return s
      .split('-')
      .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
      .join('');
  }

  const inner = $derived((TABLER_ICONS as Record<string, string>)[toPascal(name)] ?? '');
  const exists = $derived(inner.length > 0);

  $effect(() => {
    if (!exists) {
      console.warn(
        `[Icon] unknown name: "${name}" — add it to scripts/tabler-icons.json and run npm run gen:icons`,
      );
    }
  });
</script>

{#if exists}
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke={color}
    stroke-width={stroke}
    stroke-linecap="round"
    stroke-linejoin="round"
    class={cls}
    aria-hidden={title ? undefined : true}
    role={title ? 'img' : undefined}
  >
    {#if title}<title>{title}</title>{/if}
    {@html inner}
  </svg>
{:else}
  <span aria-hidden="true" style="display:inline-block;width:{size}px;height:{size}px"></span>
{/if}

<style>
  svg {
    display: inline-block;
    vertical-align: middle;
    flex-shrink: 0;
  }
</style>
