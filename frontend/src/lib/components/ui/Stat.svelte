<script lang="ts">
  interface Props {
    value: string;
    label: string;
    sub?: string;
  }

  let { value, label, sub }: Props = $props();
</script>

<div class="stat">
  <span class="value">{value}</span>
  <span class="label">{label}</span>
  {#if sub}
    <span class="sub">{sub}</span>
  {/if}
</div>

<style>
  .stat {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 20px 24px;
  }
  .value {
    font: 700 36px/1 var(--font-sans);
    letter-spacing: -1px;
    color: var(--color-yellow);
  }
  .label {
    font: 500 12px/1.4 var(--font-sans);
    color: var(--color-text-secondary);
    margin-top: 4px;
  }
  .sub {
    font: 400 11px/1.4 var(--font-mono);
    color: var(--color-text-muted);
  }
</style>
