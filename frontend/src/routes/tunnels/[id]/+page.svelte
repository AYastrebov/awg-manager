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

	// ─── Header card helpers (Task 7) ───
	function rxRateBps(): number {
		if (!tunnel) return 0;
		void trafficTick;
		const r = getTrafficRates(tunnel.id);
		if (!r || !r.rx.length) return 0;
		return r.rx[r.rx.length - 1] ?? 0;
	}

	function txRateBps(): number {
		if (!tunnel) return 0;
		void trafficTick;
		const r = getTrafficRates(tunnel.id);
		if (!r || !r.tx.length) return 0;
		return r.tx[r.tx.length - 1] ?? 0;
	}

	function uptimeText(): string {
		if (!tunnel || !isRunning) return '—';
		// stateInfo.connectedAt is the canonical "running since" timestamp on
		// the single-tunnel API response (TS type doesn't list it; cast).
		const startedAt = (tunnel.stateInfo as any)?.connectedAt;
		if (!startedAt) return '—';
		void trafficTick; // tick keeps uptime fresh between fetches
		const sec = secondsSince(startedAt);
		if (!sec) return '—';
		return formatDuration(sec);
	}

	function pingMs(): number | null {
		if (!tunnel) return null;
		// pingCheckStatus is a polling store: { data: TunnelPingStatus[] | null }.
		// Flat array, look up by tunnelId.
		const list = $pingCheckStatus.data;
		if (!Array.isArray(list)) return null;
		const status = list.find((s) => s.tunnelId === tunnel!.id);
		if (!status || status.status !== 'alive') return null;
		const lat = status.lastLatency;
		if (lat === undefined || lat === null || lat <= 0) return null;
		return Math.round(lat);
	}

	// ─── Throughput chart helpers (Task 8) ───
	function chartRx(): number[] {
		if (!tunnel) return [];
		void trafficTick;
		const r = getTrafficRates(tunnel.id);
		return r?.rx ?? [];
	}

	function chartTx(): number[] {
		if (!tunnel) return [];
		void trafficTick;
		const r = getTrafficRates(tunnel.id);
		return r?.tx ?? [];
	}

	function timeAxisLabels(): string[] {
		const now = new Date();
		const labels: string[] = [];
		for (let i = 6; i >= 0; i--) {
			const t = new Date(now.getTime() - i * 10 * 60_000);
			labels.push(`${t.getHours().toString().padStart(2, '0')}:${t.getMinutes().toString().padStart(2, '0')}`);
		}
		return labels;
	}

	// ─── Monitoring targets helpers (Task 9) ───
	type MonitoringTargetRow = {
		targetId: string;
		name: string;
		latencyMs: number | null;
		ok: boolean;
		ts: string;
	};

	function monitoringTargets(): MonitoringTargetRow[] {
		if (!tunnel) return [];
		const snap = $monitoringStore.snapshot;
		if (!snap) return [];
		const cellsForTunnel = snap.cells.filter((c) => c.tunnelId === tunnel!.id);
		if (cellsForTunnel.length === 0) return [];
		const targetMap = new Map<string, { id: string; host: string; name: string }>();
		for (const t of snap.targets) targetMap.set(t.id, t);
		return cellsForTunnel.map((c) => ({
			targetId: c.targetId,
			name: targetMap.get(c.targetId)?.name ?? c.targetId,
			latencyMs: c.latencyMs,
			ok: c.ok,
			ts: c.ts
		}));
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

		<div class="ch-card detail-header">
			<div class="header-left">
				<div class="title-row">
					<StatusDot
						variant={statusToVariant(tunnel.state)}
						halo={isRunning}
						ariaLabel={tunnel.state}
					/>
					<h1 class="ch-title-lg detail-name">{tunnel.name}</h1>
				</div>
				<span class="ch-mono meta">
					{tunnel.peer?.endpoint || '—'} · {tunnel.backend ?? 'kernel'} · {(tunnel as any).awgVersion ?? '—'} · MTU {tunnel.interface?.mtu ?? '?'}
				</span>
				<div class="autostart">
					<Toggle
						size="sm"
						checked={tunnel.enabled}
						onchange={async (next) => {
							if (!tunnel) return;
							try {
								if (next) await tunnels.start(tunnel.id);
								else await tunnels.stop(tunnel.id);
								await loadTunnel();
							} catch (e) {
								notifications.error(`Auto-start: ${(e as Error).message}`);
							}
						}}
					/>
					<span class="ch-mono autostart-label">auto-start: {tunnel.enabled ? 'ON' : 'OFF'}</span>
				</div>
			</div>
			<div class="hstat">
				<div class="hstat-label">throughput</div>
				<div class="hstat-value">
					↓{(rxRateBps() / 1024 / 1024).toFixed(1)}
					<span class="hstat-sub-inline">/ ↑{(txRateBps() / 1024 / 1024).toFixed(1)}</span>
				</div>
				<div class="hstat-sub">MB/s · live</div>
			</div>
			<div class="hstat">
				<div class="hstat-label">handshake</div>
				<div class="hstat-value">{tunnel.stateInfo?.lastHandshake ? formatRelativeTime(tunnel.stateInfo.lastHandshake) : '—'}</div>
				<div class="hstat-sub">последний обмен</div>
			</div>
			<div class="hstat">
				<div class="hstat-label">uptime</div>
				<div class="hstat-value">{uptimeText()}</div>
				<div class="hstat-sub">с момента запуска</div>
			</div>
			<div class="hstat">
				<div class="hstat-label">latency</div>
				<div class="hstat-value">{pingMs() !== null ? `${pingMs()}ms` : '—'}</div>
				<div class="hstat-sub">pingcheck</div>
			</div>
		</div>

		<!-- Task 8: Throughput chart -->
		<div class="ch-card throughput-card">
			<div class="chart-header">
				<div class="chart-title">
					<Eyebrow>Throughput · 60 минут</Eyebrow>
					<div class="chart-legend">
						RX <span class="rx-num">{(rxRateBps() / 1024 / 1024).toFixed(1)} MB/s</span>
						·
						TX <span class="tx-num">{(txRateBps() / 1024 / 1024).toFixed(1)} MB/s</span>
					</div>
				</div>
				<div class="range-chips">
					{#each ['5m', '1h', '6h', '24h', '7d'] as r (r)}
						{#if r === '1h'}
							<span class="chip active" data-r={r}>{r}</span>
						{:else}
							<Tooltip text="Доступен только 1h-окно">
								<span class="chip disabled" data-r={r}>{r}</span>
							</Tooltip>
						{/if}
					{/each}
				</div>
			</div>

			<div class="chart-body">
				<TrafficChart
					rxRates={chartRx()}
					txRates={chartTx()}
					height={220}
				/>
			</div>

			<div class="time-axis">
				{#each timeAxisLabels() as label, i (i)}
					<span>{label}</span>
				{/each}
			</div>
		</div>

		<!-- Task 9: Bottom row -->
		<div class="bottom-row">
			<div class="ch-card targets-card">
				<div class="targets-head">
					<div>
						<Eyebrow>Цели мониторинга · {monitoringTargets().length}</Eyebrow>
						<div class="ch-caption targets-sub">Что проверяет pingcheck через этот туннель</div>
					</div>
				</div>
				{#if monitoringTargets().length === 0}
					<div class="empty">
						Pingcheck отключён или нет данных.
						<a
							class="link"
							href={`/tunnels/${tunnelId}/edit?tab=routing`}
							onclick={(e) => {
								e.preventDefault();
								goto(`/tunnels/${tunnelId}/edit?tab=routing`);
							}}
						>
							Включить
						</a>
					</div>
				{:else}
					<div class="targets-table">
						<div class="trow head">
							<span>Target</span>
							<span>Latency</span>
							<span>Last check</span>
							<span>Status</span>
						</div>
						{#each monitoringTargets() as row (row.targetId)}
							<div class="trow">
								<span class="target-name">{row.name}</span>
								<span class="ch-mono">{row.latencyMs !== null ? `${row.latencyMs}ms` : '—'}</span>
								<span class="ch-mono muted">{formatRelativeTime(row.ts)}</span>
								<StatusDot
									variant={row.ok ? 'success' : 'error'}
									halo={row.ok}
									ariaLabel={row.ok ? 'alive' : 'failed'}
								/>
							</div>
						{/each}
					</div>
				{/if}
			</div>

			<div class="ch-card conf-card">
				<div class="conf-head">
					<div class="dots-row">
						<span class="dot"></span><span class="dot"></span><span class="dot"></span>
					</div>
					<span class="ch-mono filename">{tunnel.name}.conf</span>
					<span class="ch-mono synced">● synced</span>
				</div>
				<div class="conf-body">
					{#if tunnel.configPreview}
						<CodeBlock text={tunnel.configPreview} language="awg-conf" lineNumbers />
					{:else}
						<div class="empty">Превью конфига недоступно.</div>
					{/if}
				</div>
				<div class="conf-foot">
					<span class="ch-mono">{tunnel.configPreview ? tunnel.configPreview.split('\n').length : 0} строк · live</span>
					<Button variant="secondary" size="sm" onclick={downloadConf}>
						{#snippet iconBefore()}<Icon name="download" size={12} />{/snippet}
						Скачать
					</Button>
				</div>
			</div>
		</div>
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

	.detail-header {
		padding: 24px;
		margin-bottom: 16px;
		display: grid;
		grid-template-columns: 1.2fr repeat(4, 1fr);
		gap: 24px;
		align-items: center;
	}
	.header-left {
		min-width: 0;
	}
	.title-row {
		display: flex;
		align-items: center;
		gap: 12px;
		margin-bottom: 10px;
	}
	.detail-name {
		font: 700 26px/1.1 var(--font-sans);
		letter-spacing: -0.6px;
		color: var(--color-text-primary);
		margin: 0;
	}
	.meta {
		display: block;
		font: 500 12px/1.4 var(--font-mono);
		color: var(--color-text-muted);
	}
	.autostart {
		margin-top: 12px;
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.autostart-label {
		font: 500 11px/1 var(--font-mono);
		color: var(--color-text-secondary);
	}
	.hstat {
		padding-left: 24px;
		border-left: 1px solid var(--color-border);
	}
	.hstat-label {
		font: 400 11px/1 var(--font-mono);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.8px;
		margin-bottom: 8px;
	}
	.hstat-value {
		font: 700 22px/1 var(--font-sans);
		color: var(--color-yellow);
		letter-spacing: -0.5px;
	}
	.hstat-sub-inline {
		font-size: 16px;
		color: var(--color-text-secondary);
		font-weight: 600;
	}
	.hstat-sub {
		font: 400 11px/1.4 var(--font-mono);
		color: var(--color-text-muted);
		margin-top: 6px;
	}

	.throughput-card {
		padding: 20px;
		margin-bottom: 16px;
	}
	.chart-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		margin-bottom: 16px;
		gap: 16px;
	}
	.chart-title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.chart-legend {
		font: 500 13px/1.4 var(--font-sans);
		color: var(--color-text-muted);
	}
	.chart-legend .rx-num {
		color: var(--color-yellow);
		font-family: var(--font-mono);
	}
	.chart-legend .tx-num {
		color: var(--color-info);
		font-family: var(--font-mono);
	}

	.range-chips {
		display: flex;
		gap: 4px;
		padding: 4px;
		background: var(--color-bg-hover);
		border-radius: 6px;
	}
	.range-chips .chip {
		padding: 4px 10px;
		font: 600 11px/1 var(--font-sans);
		border-radius: 4px;
		cursor: pointer;
		user-select: none;
		background: transparent;
		color: var(--color-text-secondary);
		transition: background 120ms ease, color 120ms ease;
	}
	.range-chips .chip.active {
		background: var(--color-yellow);
		color: var(--color-canvas);
	}
	.range-chips .chip.disabled {
		opacity: 0.45;
		cursor: not-allowed;
	}

	.chart-body {
		height: 220px;
	}

	.time-axis {
		display: flex;
		justify-content: space-between;
		margin-top: 12px;
		font: 500 11px/1 var(--font-mono);
		color: var(--color-text-muted);
	}

	/* Task 9: Bottom row */
	.bottom-row {
		display: grid;
		grid-template-columns: 1.4fr 1fr;
		gap: 16px;
	}

	/* Targets card */
	.targets-card {
		overflow: hidden;
	}
	.targets-head {
		padding: 16px 20px;
		border-bottom: 1px solid var(--color-border);
	}
	.targets-sub {
		margin-top: 4px;
	}
	.targets-table {
		display: flex;
		flex-direction: column;
	}
	.trow {
		display: grid;
		grid-template-columns: 2fr 1fr 1.2fr 80px;
		padding: 12px 20px;
		border-bottom: 1px solid var(--color-border);
		align-items: center;
		gap: 12px;
	}
	.trow:last-child {
		border-bottom: none;
	}
	.trow.head {
		font: 600 10px/1 var(--font-sans);
		letter-spacing: 1.2px;
		text-transform: uppercase;
		color: var(--color-text-muted);
	}
	.target-name {
		font: 600 13px/1.2 var(--font-sans);
		color: var(--color-text-primary);
	}
	.muted {
		color: var(--color-text-muted);
	}
	.empty {
		padding: 24px;
		text-align: center;
		color: var(--color-text-muted);
		font-size: 13px;
	}
	.empty .link {
		color: var(--color-yellow);
		text-decoration: none;
		margin-left: 6px;
	}
	.empty .link:hover {
		text-decoration: underline;
	}

	/* Conf card */
	.conf-card {
		overflow: hidden;
		display: flex;
		flex-direction: column;
	}
	.conf-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 12px 16px;
		border-bottom: 1px solid var(--color-border);
	}
	.dots-row {
		display: flex;
		gap: 6px;
	}
	.dots-row .dot {
		width: 9px;
		height: 9px;
		border-radius: 9999px;
		background: var(--color-border-hover);
	}
	.filename {
		font: 500 12px/1 var(--font-mono);
		color: var(--color-text-muted);
	}
	.synced {
		font: 500 11px/1 var(--font-mono);
		color: var(--color-success);
	}
	.conf-body {
		flex: 1;
		max-height: 360px;
		overflow: auto;
	}
	.conf-foot {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 10px 16px;
		border-top: 1px solid var(--color-border);
	}
	.conf-foot .ch-mono {
		font: 500 11px/1 var(--font-mono);
		color: var(--color-text-muted);
	}
</style>
