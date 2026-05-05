<script lang="ts">
  interface Props {
    data: number[];
    color?: string;
    width?: number;
    height?: number;
    fillOpacity?: number;
  }

  let {
    data,
    color = 'var(--color-yellow)',
    width = 120,
    height = 28,
    fillOpacity = 0.12,
  }: Props = $props();

  let path = $derived.by(() => {
    if (data.length === 0) return '';
    const max = Math.max(...data, 1);
    const stepX = width / Math.max(1, data.length - 1);
    return data
      .map(
        (v, i) =>
          `${i === 0 ? 'M' : 'L'} ${(i * stepX).toFixed(1)} ${(height - (v / max) * height).toFixed(1)}`
      )
      .join(' ');
  });
  let area = $derived(path ? `${path} L ${width} ${height} L 0 ${height} Z` : '');
</script>

{#if data.length > 0}
  <svg {width} {height} class="sparkline">
    <path d={area} fill={color} fill-opacity={fillOpacity} />
    <path d={path} fill="none" stroke={color} stroke-width="1.5" />
  </svg>
{:else}
  <span style="display:inline-block;width:{width}px;height:{height}px"></span>
{/if}

<style>
  .sparkline { display: block; }
</style>
