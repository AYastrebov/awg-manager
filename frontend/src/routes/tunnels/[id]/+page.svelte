<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { tunnels } from '$lib/stores/tunnels';
	import { systemInfo } from '$lib/stores/system';
	import { notifications } from '$lib/stores/notifications';
	import { api } from '$lib/api/client';
	import { feedTraffic, getTrafficRates, subscribeTraffic } from '$lib/stores/traffic';
	import { pingCheckStatus } from '$lib/stores/pingcheck';
	import { monitoringStore } from '$lib/stores/monitoring';
	import {
		Button,
		Toggle,
		StatusDot,
		Eyebrow,
		Icon,
		Tooltip,
		Breadcrumb,
		CodeBlock,
		TrafficChart,
		Modal
	} from '$lib/components/ui';
	import { ReplaceTunnelConfigModal } from '$lib/components/tunnels';
	import { formatDuration, formatRelativeTime, secondsSince } from '$lib/utils/format';
	import type { AWGTunnel } from '$lib/types';
	import QRCode from 'qrcode';

	// ─── Lifecycle / data fetch ───
	let tunnelId = $derived($page.params.id ?? '');
	let tunnel = $state<AWGTunnel | null>(null);
	let loading = $state(true);

	let unsubTraffic: (() => void) | undefined;
	let unsubPingcheck: (() => void) | undefined;
	let trafficTick = $state(0);

	onMount(async () => {
		unsubTraffic = subscribeTraffic(() => {
			trafficTick++;
		});
		unsubPingcheck = pingCheckStatus.subscribe(() => {});
		await loadTunnel();
	});

	onDestroy(() => {
		unsubTraffic?.();
		unsubPingcheck?.();
	});

	async function loadTunnel() {
		if (!tunnelId) {
			notifications.error('ID туннеля не указан');
			goto('/');
			return;
		}
		loading = true;
		try {
			tunnel = await api.getTunnel(tunnelId);
		} catch (e) {
			notifications.error(`Ошибка загрузки: ${(e as Error).message}`);
			goto('/');
		} finally {
			loading = false;
		}
	}

	// ─── Derived state ───
	let sysInfo = $derived($systemInfo.data);

	function statusBucket(
		raw: string | undefined
	): 'running' | 'broken' | 'stopped' | 'starting' | 'other' {
		const s = (raw || '').toLowerCase();
		if (s === 'up' || s === 'running' || s === 'alive') return 'running';
		if (s === 'broken' || s === 'error') return 'broken';
		if (s === 'starting' || s === 'recovering' || s === 'pending') return 'starting';
		if (s === 'down' || s === 'stopped' || s === 'disabled') return 'stopped';
		return 'other';
	}

	function statusToVariant(
		raw: string | undefined
	): 'success' | 'error' | 'warning' | 'muted' {
		const b = statusBucket(raw);
		if (b === 'running') return 'success';
		if (b === 'broken') return 'error';
		if (b === 'starting') return 'warning';
		return 'muted';
	}

	let isRunning = $derived(tunnel ? statusBucket(tunnel.state) === 'running' : false);

	// ─── Action handlers ───
	let actionBusy = $state(false);

	async function restartTunnel() {
		if (!tunnel || actionBusy) return;
		actionBusy = true;
		try {
			await tunnels.restart(tunnel.id);
			notifications.success('Туннель перезапущен');
			await loadTunnel();
		} catch (e) {
			notifications.error(`Restart: ${(e as Error).message}`);
		} finally {
			actionBusy = false;
		}
	}

	async function stopTunnel() {
		if (!tunnel || actionBusy) return;
		actionBusy = true;
		try {
			await tunnels.stop(tunnel.id);
			notifications.success('Туннель остановлен');
			await loadTunnel();
		} catch (e) {
			notifications.error(`Stop: ${(e as Error).message}`);
		} finally {
			actionBusy = false;
		}
	}

	async function startTunnel() {
		if (!tunnel || actionBusy) return;
		actionBusy = true;
		try {
			await tunnels.start(tunnel.id);
			notifications.success('Туннель запущен');
			await loadTunnel();
		} catch (e) {
			notifications.error(`Start: ${(e as Error).message}`);
		} finally {
			actionBusy = false;
		}
	}

	async function downloadConf() {
		if (!tunnel) return;
		try {
			const blob = await api.exportTunnel(tunnel.id);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = `${tunnel.name}.conf`;
			a.click();
			URL.revokeObjectURL(url);
		} catch (e) {
			notifications.error(`Скачивание: ${(e as Error).message}`);
		}
	}

	function goEdit() {
		goto(`/tunnels/${tunnelId}/edit`);
	}

	// ─── Modals ───
	let qrOpen = $state(false);
	let qrDataUrl = $state('');

	async function openQr() {
		if (!tunnel) return;
		try {
			const blob = await api.exportTunnel(tunnel.id);
			const text = await blob.text();
			qrDataUrl = await QRCode.toDataURL(text, {
				errorCorrectionLevel: 'M',
				margin: 2,
				width: 320
			});
			qrOpen = true;
		} catch (e) {
			notifications.error(`QR: ${(e as Error).message}`);
		}
	}

	let replaceOpen = $state(false);

	// ─── dots-menu state (Task 10) ───
	let dotsOpen = $state(false);
	let dotsAnchor = $state<HTMLButtonElement | undefined>();

	function toggleDots() {
		dotsOpen = !dotsOpen;
	}

	async function deleteTunnel() {
		if (!tunnel) return;
		if (!confirm(`Удалить туннель "${tunnel.name}"?`)) return;
		try {
			await tunnels.remove(tunnel.id);
			notifications.success('Туннель удалён');
			goto('/');
		} catch (e) {
			notifications.error(`Удаление: ${(e as Error).message}`);
		}
	}
</script>

<div class="ch-page-container">
	{#if loading || !tunnel}
		<p class="ch-body">Загрузка туннеля…</p>
	{:else}
		<header class="detail-header-row">
			<Breadcrumb
				items={[
					{ label: 'Туннели', href: '/' },
					{ label: tunnel.name }
				]}
			/>
			<div class="ch-action-bar">
				<Button variant="secondary" size="sm" onclick={goEdit}>
					{#snippet iconBefore()}<Icon name="pencil" size={12} />{/snippet}
					Edit
				</Button>
				<Button variant="secondary" size="sm" onclick={downloadConf}>
					{#snippet iconBefore()}<Icon name="download" size={12} />{/snippet}
					.conf
				</Button>
				<Button variant="secondary" size="sm" onclick={openQr}>
					{#snippet iconBefore()}<Icon name="qrcode" size={12} />{/snippet}
					QR
				</Button>
				<Button
					variant="secondary"
					size="sm"
					onclick={restartTunnel}
					disabled={actionBusy}
				>
					{#snippet iconBefore()}<Icon name="refresh" size={12} />{/snippet}
					Restart
				</Button>
				{#if isRunning}
					<Button variant="danger" size="sm" onclick={stopTunnel} disabled={actionBusy}>
						{#snippet iconBefore()}<Icon name="player-stop" size={12} />{/snippet}
						Stop
					</Button>
				{:else}
					<Button variant="primary" size="sm" onclick={startTunnel} disabled={actionBusy}>
						{#snippet iconBefore()}
							<Icon name="player-stop" size={12} color="var(--color-on-yellow)" />
						{/snippet}
						Start
					</Button>
				{/if}
				<Tooltip text="Опции">
					<button
						bind:this={dotsAnchor}
						class="icon-btn dots-trigger"
						onclick={toggleDots}
						aria-label="Опции"
					>
						<Icon name="dots-vertical" size={14} />
					</button>
				</Tooltip>
				<!-- Task 10 fills dots popover here -->
			</div>
		</header>

		<!-- Task 7: Header card -->
		<!-- Task 8: Throughput chart -->
		<!-- Task 9: Bottom row -->
	{/if}
</div>

<!-- QR modal -->
<Modal bind:open={qrOpen} title="QR конфигурации" onclose={() => (qrOpen = false)}>
	{#if qrDataUrl}
		<div class="qr-wrap">
			<img src={qrDataUrl} alt="QR" />
			<p class="ch-caption">Отсканируйте на устройстве для импорта конфигурации.</p>
		</div>
	{/if}
</Modal>

{#if tunnel}
	<ReplaceTunnelConfigModal
		bind:open={replaceOpen}
		tunnelId={tunnel.id}
		tunnelName={tunnel.name}
		tunnelState={tunnel.state ?? 'stopped'}
		backendLabel={tunnel.backend === 'nativewg' ? 'NativeWG' : 'Kernel'}
		ndmsName={tunnel.interfaceName ?? tunnel.id}
		onclose={() => (replaceOpen = false)}
		onreplaced={() => {
			replaceOpen = false;
			loadTunnel();
		}}
	/>
{/if}

<style>
	.detail-header-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 16px;
		gap: 16px;
		flex-wrap: wrap;
	}
	.ch-action-bar {
		position: relative;
	}
	.icon-btn {
		background: transparent;
		border: none;
		cursor: pointer;
		padding: 4px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border-radius: 4px;
		color: var(--color-text-muted);
	}
	.icon-btn:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}
	.dots-trigger {
		height: 32px;
		padding: 0 8px;
	}

	.qr-wrap {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 12px;
	}
	.qr-wrap img {
		width: 320px;
		height: 320px;
		background: white;
		padding: 8px;
		border-radius: 8px;
	}
</style>
