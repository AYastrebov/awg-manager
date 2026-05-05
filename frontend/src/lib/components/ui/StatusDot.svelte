<script lang="ts" module>
  export type StatusDotVariant = 'success' | 'error' | 'warning' | 'info' | 'muted';
  export type StatusDotSize = 'sm' | 'md';
</script>

<script lang="ts">
  interface Props {
    variant?: StatusDotVariant;
    pulse?: boolean;
    size?: StatusDotSize;
    ariaLabel?: string;
    halo?: boolean;
  }

  let {
    variant = 'muted',
    pulse = false,
    size = 'md',
    ariaLabel,
    halo = false,
  }: Props = $props();
</script>

<span
  class="dot"
  class:variant-success={variant === 'success'}
  class:variant-error={variant === 'error'}
  class:variant-warning={variant === 'warning'}
  class:variant-info={variant === 'info'}
  class:variant-muted={variant === 'muted'}
  class:size-sm={size === 'sm'}
  class:size-md={size === 'md'}
  class:is-pulse={pulse}
  class:is-halo={halo}
  role={ariaLabel ? 'img' : undefined}
  aria-label={ariaLabel}
></span>

<style>
  .dot {
    display: inline-block;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .size-sm { width: 6px; height: 6px; }
  .size-md { width: 8px; height: 8px; }

  .variant-success { background: var(--color-success); }
  .variant-error { background: var(--color-error); }
  .variant-warning { background: var(--color-warning); }
  .variant-info { background: var(--color-info); }
  .variant-muted { background: var(--color-text-muted); }

  .is-pulse {
    animation: dot-pulse 1.4s ease-in-out infinite;
  }

  .is-halo.variant-success { box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-success) 18%, transparent); }
  .is-halo.variant-error   { box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-error) 18%, transparent); }
  .is-halo.variant-warning { box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-warning) 18%, transparent); }
  .is-halo.variant-info    { box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-info) 18%, transparent); }
  .is-halo.variant-muted   { box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-text-muted) 18%, transparent); }

  @keyframes dot-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
</style>
