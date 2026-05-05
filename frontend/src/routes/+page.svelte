<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { tunnels } from '$lib/stores/tunnels';
	import { systemInfo as systemInfoStore } from '$lib/stores/system';
	import { notifications } from '$lib/stores/notifications';
	import { feedTraffic, getTrafficRates, subscribeTraffic } from '$lib/stores/traffic';
	import { singboxTunnels } from '$lib/stores/singbox';
	import {
		Tabs,
		Button,
		Toggle,
		StatusDot,
		Stat,
		StatStrip,
		Sparkline,
		Eyebrow,
		Icon
	} from '$lib/components/ui';
	import { AdoptTunnelDialog } from '$lib/components/tunnels';
	import { formatRelativeTime } from '$lib/utils/format';
	import type { TunnelListItem, ExternalTunnel } from '$lib/types';

	type TunnelTab = 'awg' | 'singbox' | 'system';
	type FilterChip = 'all' | 'running' | 'broken' | 'stopped';

	// Polling-store subscription: first subscriber triggers fetch,
	// last unsubscribe stops polling.
	let unsubTunnels: (() => void) | undefined;
	let unsubSingboxTunnels: (() => void) | undefined;
	// Traffic store is callback-based, not a Svelte store. We keep a tick
	// counter that the listener bumps on every feedTraffic() call so any
	// $derived that reads getTrafficRates() re-runs in step with live data.
	let trafficTick = $state(0);
	let unsubTraffic: (() => void) | undefined;

	onMount(() => {
		unsubTunnels = tunnels.subscribe(() => {});
		unsubSingboxTunnels = singboxTunnels.subscribe(() => {});
		unsubTraffic = subscribeTraffic(() => {
			trafficTick += 1;
		});
	});
	onDestroy(() => {
		unsubTunnels?.();
		unsubSingboxTunnels?.();
		unsubTraffic?.();
	});

	let sysInfo = $derived($systemInfoStore.data);
	let tunnelSnap = $derived($tunnels);
	let awgList = $derived(tunnelSnap.data?.tunnels ?? []);
	let systemList = $derived(tunnelSnap.data?.system ?? []);
	let externalList = $derived(tunnelSnap.data?.external ?? []);
	let loading = $derived(!sysInfo || tunnelSnap.lastFetchedAt === 0);

	// System tunnels don't emit tunnel:traffic SSE events — feed the
	// traffic store from the polled snapshot (~5s) so per-system-tunnel
	// rates stay alive on this page. Skip ones already managed (their
	// SSE feed is handled in +layout).
	$effect(() => {
		for (const st of systemList) {
			const isManaged = awgList.some(
				(m) =>
					(m.ndmsName && m.ndmsName === st.id) ||
					(m.interfaceName && m.interfaceName === st.id)
			);
			if (isManaged) continue;
			if (st.status === 'up' && st.peer) {
				feedTraffic(st.id, st.peer.rxBytes, st.peer.txBytes);
			}
		}
	});

	let activeTab = $state<TunnelTab>('awg');
	let filter = $state<FilterChip>('all');
	let searchQuery = $state('');

	let singboxList = $derived($singboxTunnels.data ?? []);
	let singboxCount = $derived(singboxList.length);

	let tabs = $derived([
		{ id: 'awg', label: 'AWG', badge: awgList.length },
		{ id: 'singbox', label: 'Sing-box', badge: singboxCount },
		{ id: 'system', label: 'System', badge: systemList.length }
	]);

	function statusBucket(
		raw: string
	): 'running' | 'broken' | 'stopped' | 'starting' | 'other' {
		const s = (raw || '').toLowerCase();
		if (s === 'up' || s === 'running' || s === 'alive') return 'running';
		if (s === 'broken' || s === 'error') return 'broken';
		if (s === 'starting' || s === 'recovering' || s === 'pending') return 'starting';
		if (s === 'down' || s === 'stopped' || s === 'disabled' || s === 'needs_start')
			return 'stopped';
		return 'other';
	}

	function statusToVariant(raw: string): 'success' | 'error' | 'warning' | 'muted' {
		const b = statusBucket(raw);
		if (b === 'running') return 'success';
		if (b === 'broken') return 'error';
		if (b === 'starting') return 'warning';
		return 'muted';
	}

	let visibleTunnels = $derived.by(() => {
		let list: TunnelListItem[] = activeTab === 'awg' ? awgList : [];
		if (filter !== 'all') {
			list = list.filter((t) => statusBucket(t.status) === filter);
		}
		if (searchQuery) {
			const q = searchQuery.toLowerCase();
			list = list.filter(
				(t) =>
					t.name.toLowerCase().includes(q) ||
					(t.endpoint || '').toLowerCase().includes(q) ||
					(t.address || '').toLowerCase().includes(q)
			);
		}
		return list;
	});

	let runningCount = $derived(
		awgList.filter((t) => statusBucket(t.status) === 'running').length
	);
	let brokenCount = $derived(
		awgList.filter((t) => statusBucket(t.status) === 'broken').length
	);
	let stoppedCount = $derived(
		awgList.filter((t) => statusBucket(t.status) === 'stopped').length
	);
	let totalCount = $derived(awgList.length);
	let totalRx = $derived(awgList.reduce((a, t) => a + (t.rxBytes ?? 0), 0));
	let totalTx = $derived(awgList.reduce((a, t) => a + (t.txBytes ?? 0), 0));

	// Reactively read the latest rate for a single tunnel — touches
	// trafficTick so $derived re-runs on every feedTraffic notification.
	function latestRate(id: string): { rx: number; tx: number } {
		void trafficTick;
		const r = getTrafficRates(id);
		const rx = r.rx.length > 0 ? r.rx[r.rx.length - 1] : 0;
		const tx = r.tx.length > 0 ? r.tx[r.tx.length - 1] : 0;
		return { rx, tx };
	}

	let peakRate = $derived.by(() => {
		void trafficTick;
		let max = 0;
		let name = '';
		for (const t of awgList) {
			if (statusBucket(t.status) !== 'running') continue;
			const { rx, tx } = latestRate(t.id);
			const rate = rx + tx;
			if (rate > max) {
				max = rate;
				name = t.name;
			}
		}
		return { mbps: max / (1024 * 1024), tunnel: name };
	});

	let mostRecentHandshake = $derived.by(() => {
		for (const t of awgList) {
			if (statusBucket(t.status) !== 'running') continue;
			if (t.lastHandshake) {
				return {
					value: formatRelativeTime(t.lastHandshake),
					name: t.name + (t.awgVersion ? ' · ' + t.awgVersion : '')
				};
			}
		}
		return { value: '—', name: '' };
	});

	function fmtBytes(b: number): string {
		if (!b) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(b) / Math.log(k));
		return parseFloat((b / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	// Adopt-external-tunnel dialog state. The dialog itself is keyed off
	// `adoptingTunnel`; open/error/loading use the dialog's $bindable props.
	let adoptingTunnel = $state<ExternalTunnel | null>(null);
	let adoptDialogOpen = $state(false);
	let adoptError = $state('');
	let adoptLoading = $state(false);

	function openAdoptDialog(et: ExternalTunnel): void {
		adoptingTunnel = et;
		adoptError = '';
		adoptLoading = false;
		adoptDialogOpen = true;
	}

	function closeAdoptDialog(): void {
		adoptDialogOpen = false;
		adoptingTunnel = null;
		adoptError = '';
		adoptLoading = false;
	}

	async function handleAdopt(data: { content: string; name: string }): Promise<void> {
		if (!adoptingTunnel) return;
		adoptLoading = true;
		adoptError = '';
		try {
			const adopted = await tunnels.adoptExternal(
				adoptingTunnel.interfaceName,
				data.content,
				data.name
			);
			if (adopted.warnings?.length) {
				adopted.warnings.forEach((w) => notifications.warning(w));
			}
			notifications.success('Туннель импортирован');
			closeAdoptDialog();
		} catch (e) {
			adoptError = e instanceof Error ? e.message : 'Не удалось импортировать туннель';
		} finally {
			adoptLoading = false;
		}
	}

	let toggleLoading = $state<Record<string, boolean>>({});
	async function handleToggle(t: TunnelListItem) {
		if (toggleLoading[t.id]) return;
		// needs_start is NOT "on" — reuse the lifecycle interpretation
		// from the previous home page so the toggle reflects intent.
		const isOn = ['running', 'starting', 'broken'].includes(t.status);
		toggleLoading = { ...toggleLoading, [t.id]: true };
		try {
			if (isOn) {
				await tunnels.stop(t.id);
				notifications.success('Туннель остановлен');
			} else {
				await tunnels.start(t.id);
				notifications.success('Туннель запущен');
			}
		} catch (e) {
			notifications.error(e instanceof Error ? e.message : 'Ошибка');
		} finally {
			const { [t.id]: _, ...rest } = toggleLoading;
			toggleLoading = rest;
		}
	}

	function openDetail(id: string) {
		goto(`/tunnels/${id}`);
	}
	function createTunnel() {
		goto('/tunnels/new');
	}
	function importTunnel() {
		// First-mile: route to the existing /tunnels/new page (link-import
		// tab already accepts pasted .conf content). Real drag-and-drop
		// landing page is a later mile.
		goto('/tunnels/new?tab=link');
	}
</script>

<svelte:head>
	<title>Туннели - AWG Manager</title>
</svelte:head>

<div class="ch-page-container">
	{#if loading}
		<p class="ch-body">Загрузка туннелей…</p>
	{:else}
		<header class="ch-page-header">
			<div>
				<Eyebrow color="yellow">AWG · WireGuard fork</Eyebrow>
				<h1 class="ch-display-sm header-title">
					Туннели <span class="dim">· {totalCount}</span>
				</h1>
				<p class="ch-caption header-sub">
					{runningCount} активны · {brokenCount} broken · {stoppedCount} остановлен
				</p>
			</div>
			<div class="ch-action-bar">
				<Button variant="secondary" size="md" onclick={importTunnel}>
					{#snippet iconBefore()}
						<Icon name="upload" size={14} />
					{/snippet}
					Импорт .conf
				</Button>
				<Button variant="primary" size="md" onclick={createTunnel}>
					{#snippet iconBefore()}
						<Icon name="plus" size={14} color="var(--color-on-yellow)" />
					{/snippet}
					Создать туннель
				</Button>
			</div>
		</header>

		<div class="tabs-row">
			<Tabs {tabs} active={activeTab} onchange={(id) => (activeTab = id as TunnelTab)} />
		</div>

		<div class="strip-row">
			<StatStrip>
				<Stat
					value={`${runningCount}/${totalCount}`}
					label="туннелей online"
					sub={totalCount === 0 ? 'нет туннелей' : 'из подключённых AWG'}
				/>
				<Stat
					value={peakRate.mbps.toFixed(1)}
					label="MB/s ↓ пиковая"
					sub={peakRate.tunnel || '—'}
				/>
				<Stat
					value={fmtBytes(totalRx + totalTx).split(' ')[0]}
					label={`${fmtBytes(totalRx + totalTx).split(' ')[1] || ''} обмен с момента запуска`}
					sub={`↓ ${fmtBytes(totalRx)}  ↑ ${fmtBytes(totalTx)}`}
				/>
				<Stat
					value={mostRecentHandshake.value}
					label="последний handshake"
					sub={mostRecentHandshake.name || '—'}
				/>
			</StatStrip>
		</div>

		<div class="search-row">
			<div class="search-input">
				<Icon name="search" size={14} color="var(--color-text-muted)" />
				<input
					type="text"
					bind:value={searchQuery}
					placeholder="SELECT * FROM tunnels WHERE status = 'running' …"
				/>
			</div>
			<div class="filter-chips">
				{#each ['all', 'running', 'broken', 'stopped'] as f (f)}
					<button
						class="chip"
						class:active={filter === f}
						onclick={() => (filter = f as FilterChip)}>{f}</button
					>
				{/each}
			</div>
		</div>

		{#if activeTab === 'awg'}
			<div class="ch-card table">
				<div class="row head">
					<span></span>
					<span>Туннель</span>
					<span>Status</span>
					<span>Endpoint · IP</span>
					<span>Throughput</span>
					<span>Handshake</span>
					<span class="text-right">Backend</span>
					<span></span>
				</div>
				{#each visibleTunnels as t (t.id)}
					{@const rate = latestRate(t.id)}
					{@const rxArr = (() => {
						void trafficTick;
						return getTrafficRates(t.id).rx;
					})()}
					{@const txArr = (() => {
						void trafficTick;
						return getTrafficRates(t.id).tx;
					})()}
					{@const sparkData = rxArr
						.slice(-28)
						.map((v, i) => v + (txArr.slice(-28)[i] ?? 0))}
					<div class="row">
						<Toggle
							checked={['running', 'starting', 'broken'].includes(t.status)}
							size="sm"
							loading={!!toggleLoading[t.id]}
							onchange={() => handleToggle(t)}
						/>
						<div class="cell-name">
							<div class="name-line">
								<span class="ch-title-sm name-text">{t.name}</span>
								{#if t.backend}
									<span class="badge backend" data-backend={t.backend}>{t.backend}</span>
								{/if}
								{#if t.awgVersion}
									<span class="badge sig">{t.awgVersion}</span>
								{/if}
							</div>
							<div class="ch-mono sub">
								{t.address || '—'} · MTU {t.mtu ?? '?'}
							</div>
						</div>
						<div class="cell-status">
							<StatusDot
								variant={statusToVariant(t.status)}
								halo={statusBucket(t.status) === 'running'}
								ariaLabel={t.status}
							/>
							<span class="status-label">{t.status}</span>
						</div>
						<div class="ch-mono">
							<div>{t.endpoint || '—'}</div>
							<div class="muted">{t.address || '—'}</div>
						</div>
						<div class="cell-rate">
							<Sparkline
								data={sparkData}
								color={statusBucket(t.status) === 'running'
									? 'var(--color-yellow)'
									: 'var(--color-border-hover)'}
								width={92}
								height={28}
							/>
							<div class="ch-mono rate-text">
								<div class="up">↓ {(rate.rx / 1024 / 1024).toFixed(1)} MB/s</div>
								<div>↑ {(rate.tx / 1024 / 1024).toFixed(1)} MB/s</div>
							</div>
						</div>
						<span class="ch-mono handshake">
							{t.lastHandshake ? formatRelativeTime(t.lastHandshake) : '—'}
						</span>
						<span class="cell-backend">{t.backend ?? '—'}</span>
						<div class="row-actions">
							<button
								class="icon-btn"
								onclick={() => openDetail(t.id)}
								aria-label="Открыть детали"
							>
								<Icon name="chart-line" size={14} color="var(--color-text-muted)" />
							</button>
							<button class="icon-btn" aria-label="Опции">
								<Icon name="dots-vertical" size={14} color="var(--color-text-muted)" />
							</button>
						</div>
					</div>
				{/each}
				{#if visibleTunnels.length === 0 && externalList.length === 0}
					<div class="empty-row ch-caption">Туннели не найдены.</div>
				{/if}
				{#if externalList.length > 0}
					<div class="row divider">
						<span></span>
						<span class="divider-label">Не управляются · {externalList.length}</span>
						<span></span>
						<span></span>
						<span></span>
						<span></span>
						<span></span>
						<span></span>
					</div>
					{#each externalList as et (et.interfaceName)}
						<div class="row external">
							<span></span>
							<div class="cell-name">
								<div class="name-line">
									<span class="ch-title-sm muted-name">{et.interfaceName}</span>
									<span class="badge external-badge">EXTERNAL</span>
									{#if et.isAWG}
										<span class="badge sig">AWG</span>
									{/if}
								</div>
								<div class="ch-mono sub">
									{et.publicKey ? et.publicKey.slice(0, 16) + '…' : ''} · #{et.tunnelNumber}
								</div>
							</div>
							<span class="ch-mono muted">не управляется</span>
							<div class="ch-mono">
								{et.endpoint || ''}
							</div>
							<div class="cell-rate">
								<Sparkline data={[]} width={92} height={28} />
								<div class="ch-mono rate-text">
									<div>↓ {fmtBytes(et.rxBytes)}</div>
									<div>↑ {fmtBytes(et.txBytes)}</div>
								</div>
							</div>
							<span></span>
							<div class="adopt-action">
								<Button variant="primary" size="md" onclick={() => openAdoptDialog(et)}>
									{#snippet iconBefore()}
										<Icon name="plus" size={14} color="var(--color-canvas)" />
									{/snippet}
									Взять под управление
								</Button>
							</div>
						</div>
					{/each}
				{/if}
			</div>
		{:else if activeTab === 'singbox'}
			<div class="ch-card placeholder">
				<span class="ch-caption"
					>Sing-box outbounds — полная страница будет в следующей миле редизайна.</span
				>
			</div>
		{:else}
			<div class="ch-card placeholder">
				<span class="ch-caption"
					>Системные туннели — полная страница будет в следующей миле редизайна.</span
				>
			</div>
		{/if}

		<div class="ch-card system-row">
			<Eyebrow>System</Eyebrow>
			<div class="ch-mono system-line">
				<span
					><span class="muted">kernel</span>
					{sysInfo?.firmwareVersion || '—'} · OS{sysInfo?.isOS5 ? '5' : '4'}</span
				>
				<span
					><span class="muted">arch</span>
					{sysInfo?.goArch || '—'}</span
				>
				<span
					><span class="muted">module</span> amneziawg
					<span class={sysInfo?.kernelModuleLoaded ? 'ok' : 'muted'}>
						{sysInfo?.kernelModuleLoaded
							? 'loaded'
							: sysInfo?.kernelModuleExists
								? 'present'
								: 'missing'}
					</span>
				</span>
				<span
					><span class="muted">singbox</span>
					{sysInfo?.singbox?.version || '—'}
					{#if sysInfo?.singbox?.installed}<span class="ok">installed</span>{/if}
				</span>
			</div>
		</div>
	{/if}
</div>

{#if adoptingTunnel}
	<AdoptTunnelDialog
		interfaceName={adoptingTunnel.interfaceName}
		bind:open={adoptDialogOpen}
		bind:error={adoptError}
		bind:loading={adoptLoading}
		onclose={closeAdoptDialog}
		onadopt={handleAdopt}
	/>
{/if}

<style>
	.header-title {
		margin: 8px 0 4px;
	}
	.header-title .dim {
		color: var(--color-text-muted);
		font-weight: 500;
	}
	.header-sub {
		margin: 0;
	}

	.tabs-row {
		margin-top: 16px;
	}
	.strip-row {
		margin-top: 16px;
	}

	.search-row {
		display: flex;
		align-items: center;
		gap: 12px;
		margin: 16px 0 12px;
	}
	.search-input {
		flex: 1;
		height: 36px;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: 8px;
		padding: 0 12px;
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}
	.search-input input {
		flex: 1;
		background: transparent;
		border: none;
		outline: none;
		font: 400 13px/1 var(--font-mono);
		color: var(--color-text-secondary);
	}
	.search-input input::placeholder {
		color: var(--color-text-muted-soft);
	}
	.filter-chips {
		display: flex;
		gap: 8px;
	}
	.chip {
		padding: 7px 12px;
		font: 500 12px/1 var(--font-sans);
		color: var(--color-text-secondary);
		background: transparent;
		border: 1px solid var(--color-border-hover);
		border-radius: 6px;
		cursor: pointer;
		text-transform: capitalize;
	}
	.chip.active {
		color: var(--color-on-yellow);
		background: var(--color-yellow);
		border-color: transparent;
	}
	.chip:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}
	.chip.active:hover {
		background: var(--color-yellow-active);
		color: var(--color-canvas);
	}

	.table {
		overflow: hidden;
		margin-bottom: 20px;
		width: 100%;
	}
	.row {
		display: grid;
		grid-template-columns: 32px minmax(0, 2fr) 1fr minmax(0, 1.4fr) minmax(
				0,
				1.5fr
			) 1fr 80px 60px;
		padding: 14px 20px;
		border-bottom: 1px solid var(--color-border);
		align-items: center;
		gap: 16px;
	}
	.row:last-child {
		border-bottom: none;
	}
	.row.head {
		padding: 12px 20px;
		font: 600 11px/1 var(--font-sans);
		letter-spacing: 1.2px;
		text-transform: uppercase;
		color: var(--color-text-muted);
		align-items: center;
	}
	.row.head > span {
		display: flex;
		align-items: center;
		min-width: 0;
	}
	.text-right {
		text-align: right;
	}

	.cell-name {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.name-line {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.name-text {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.badge {
		font: 500 10px/1 var(--font-mono);
		padding: 3px 6px;
		border-radius: 4px;
		text-transform: uppercase;
		letter-spacing: 0.5px;
	}
	.badge.backend {
		background: var(--color-bg-hover);
		color: var(--color-text-secondary);
	}
	.badge.backend[data-backend='nativewg'] {
		color: var(--color-info);
	}
	.badge.sig {
		color: var(--color-text-muted);
		border: 1px solid var(--color-border-hover);
	}
	.sub {
		font-size: 11px;
		color: var(--color-text-muted);
	}

	.cell-status {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}
	.status-label {
		font: 500 12px/1 var(--font-mono);
		color: var(--color-text-muted);
		text-transform: lowercase;
	}

	.cell-rate {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.rate-text {
		font: 500 11px/1.4 var(--font-mono);
		color: var(--color-text-secondary);
	}
	.rate-text .up {
		color: var(--color-success);
	}
	.handshake {
		font-size: 12px;
		color: var(--color-text-secondary);
	}
	.muted {
		color: var(--color-text-muted);
	}
	.cell-backend {
		font: 600 13px/1 var(--font-sans);
		color: var(--color-text-secondary);
		text-align: right;
		text-transform: uppercase;
		letter-spacing: 0.5px;
	}

	.row-actions {
		display: flex;
		gap: 6px;
		justify-content: flex-end;
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

	.empty-row {
		padding: 32px 20px;
		text-align: center;
	}
	.placeholder {
		padding: 24px;
		text-align: center;
	}

	.system-row {
		margin-top: 20px;
		padding: 14px 20px;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.system-line {
		display: flex;
		flex-wrap: wrap;
		gap: 24px;
		font: 400 12px/1.5 var(--font-mono);
		color: var(--color-text-secondary);
	}
	.system-line .muted {
		color: var(--color-text-muted);
	}
	.system-line .ok {
		color: var(--color-success);
		margin-left: 4px;
	}

	/* Centered toggle in tunnel rows: hide spinner-slot (the asymmetric pre-slider
	   placeholder) so the slider sits flush in its column. Loading state will
	   show the spinner where the slider was — acceptable tradeoff for alignment.
	   Scoped via :global() because Svelte's scoped CSS doesn't reach into
	   third-party Toggle internals. */
	.row :global(.toggle-spinner-slot) {
		display: none;
	}
	.row :global(.toggle-container) {
		justify-self: center;
	}

	.row.divider {
		padding: 8px 20px;
		background: var(--color-bg-secondary);
		border-bottom: 1px solid var(--color-border);
	}
	.divider-label {
		font: 600 11px/1.4 var(--font-sans);
		letter-spacing: 1.5px;
		text-transform: uppercase;
		color: var(--color-text-muted);
	}
	.row.external .adopt-action {
		grid-column: 7 / span 2;
		display: flex;
		justify-content: flex-end;
	}
	.row.external .muted-name {
		color: var(--color-text-muted);
		font-weight: 500;
	}
	.badge.external-badge {
		background: transparent;
		color: var(--color-text-muted);
		border: 1px dashed var(--color-border-hover);
	}
</style>
