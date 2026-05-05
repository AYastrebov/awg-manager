<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onDestroy } from 'svelte';

	interface Props {
		text: string;
		side?: 'top' | 'bottom';
		delay?: number;
		children: Snippet;
	}

	let { text, side = 'top', delay = 200, children }: Props = $props();

	let trigger: HTMLSpanElement | undefined = $state();
	let visible = $state(false);
	let coords = $state({ x: 0, y: 0 });
	let showTimer: ReturnType<typeof setTimeout> | null = null;

	function show() {
		if (!trigger) return;
		if (showTimer) clearTimeout(showTimer);
		showTimer = setTimeout(() => {
			if (!trigger) return;
			const r = trigger.getBoundingClientRect();
			coords =
				side === 'top'
					? { x: r.left + r.width / 2, y: r.top - 6 }
					: { x: r.left + r.width / 2, y: r.bottom + 6 };
			visible = true;
		}, delay);
	}

	function hide() {
		if (showTimer) {
			clearTimeout(showTimer);
			showTimer = null;
		}
		visible = false;
	}

	onDestroy(() => {
		if (showTimer) clearTimeout(showTimer);
	});
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<span
	bind:this={trigger}
	class="trigger"
	onmouseenter={show}
	onmouseleave={hide}
	onfocusin={show}
	onfocusout={hide}
>
	{@render children()}
</span>

{#if visible}
	<div
		class="tooltip"
		role="tooltip"
		data-side={side}
		style="left: {coords.x}px; top: {coords.y}px;"
	>
		{text}
	</div>
{/if}

<style>
	.trigger {
		display: inline-flex;
		align-items: center;
	}
	.tooltip {
		position: fixed;
		z-index: 1000;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border-hover);
		color: var(--color-text-primary);
		padding: 6px 10px;
		border-radius: 4px;
		font: 500 11px/1 var(--font-mono);
		white-space: nowrap;
		pointer-events: none;
	}
	.tooltip[data-side='top'] {
		transform: translate(-50%, -100%);
	}
	.tooltip[data-side='bottom'] {
		transform: translate(-50%, 0);
	}
</style>
