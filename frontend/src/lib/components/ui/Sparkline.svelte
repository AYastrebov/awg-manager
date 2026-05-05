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

  // Deterministic placeholder wave for empty state — gentle sin curve
  // matching the visual feel of design's `rndSpark(28, 40, 24)`.
  const PLACEHOLDER_DATA = Array.from({ length: 28 }, (_, i) =>
    40 + Math.sin(i * 0.55) * 10 + Math.cos(i * 0.31) * 4
  );

  let isPlaceholder = $derived(data.length === 0);
  let effectiveData = $derived(isPlaceholder ? PLACEHOLDER_DATA : data);
  let effectiveColor = $derived(isPlaceholder ? 'var(--color-border-hover)' : color);
  let effectiveFillOpacity = $derived(isPlaceholder ? 0.08 : fillOpacity);

  let path = $derived.by(() => {
    if (effectiveData.length === 0) return '';
    const max = Math.max(...effectiveData, 1);
    const stepX = width / Math.max(1, effectiveData.length - 1);
    return effectiveData
      .map(
        (v, i) =>
          `${i === 0 ? 'M' : 'L'} ${(i * stepX).toFixed(1)} ${(height - (v / max) * height).toFixed(1)}`
      )
      .join(' ');
  });
  let area = $derived(path ? `${path} L ${width} ${height} L 0 ${height} Z` : '');
</script>

{#if effectiveData.length > 0}
  <svg {width} {height} class="sparkline" aria-hidden={isPlaceholder ? 'true' : undefined}>
    <path d={area} fill={effectiveColor} fill-opacity={effectiveFillOpacity} />
    <path d={path} fill="none" stroke={effectiveColor} stroke-width="1.5" />
  </svg>
{:else}
  <span style="display:inline-block;width:{width}px;height:{height}px"></span>
{/if}

<style>
  .sparkline { display: block; }
</style>
