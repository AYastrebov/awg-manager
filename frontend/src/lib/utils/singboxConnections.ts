// frontend/src/lib/utils/singboxConnections.ts

import type {
	ClashConnectionsRaw,
	Connection,
	ConnectionBucket,
	ConnectionFilters,
	ConnectionsSnapshot,
} from '$lib/types/singboxConnections';

const OUTBOUND_LABELS: Record<string, string> = {
	DIRECT: 'Прямое',
	REJECT: 'Отклонено',
};

export function chainOutboundLabel(chains: string[]): string {
	if (chains.length === 0) return '—';
	const first = chains[0];
	return OUTBOUND_LABELS[first] ?? first;
}

/**
 * isPublicIP returns true when ip is OUTSIDE every range that a LAN
 * client could legitimately source from (RFC1918 + loopback +
 * link-local + IPv6 ULA / link-local / loopback).
 *
 * Motivation: on Keenetic + Mediatek hardware, UDP flows from
 * policy-bound LAN devices reach sing-box AFTER kernel MASQUERADE
 * has rewritten the source to the router's WAN IP — kernel fast-path
 * applies the conntrack NAT mapping before sing-box's TPROXY socket
 * can read the original IP header. The result is a "client" bucket
 * keyed by a public IP that's actually the router itself. We can't
 * detect "router's WAN IP" without an extra round-trip API, but ANY
 * non-private source IP appearing as a "client" is — for a LAN-side
 * tproxy use case — necessarily post-NAT traffic. TCP via REDIRECT
 * preserves the source through conntrack-aware reverse, so this only
 * fires in practice for UDP (QUIC).
 *
 * False positives: a LAN device on CG-NAT (100.64.0.0/10) would be
 * misclassified. That range is normally on the ISP side, not the
 * home LAN — accepting the mislabel.
 */
export function isPublicIP(ip: string): boolean {
	if (!ip) return false;

	// IPv6
	if (ip.includes(':')) {
		const norm = ip.toLowerCase();
		if (norm === '::1' || norm === '::') return false;
		// Link-local fe80::/10 — the first 10 bits are 1111111010, so
		// the first hex block is fe80-febf. Match the common forms.
		if (/^fe[89ab][0-9a-f]:/i.test(norm)) return false;
		// ULA fc00::/7 — first 7 bits 1111110, first hex block fc__ or fd__.
		if (/^f[cd][0-9a-f]{2}:/i.test(norm)) return false;
		return true;
	}

	// IPv4
	const parts = ip.split('.').map(Number);
	if (parts.length !== 4 || parts.some((n) => !Number.isFinite(n) || n < 0 || n > 255)) {
		return false;
	}
	const [a, b] = parts;
	if (a === 10) return false;
	if (a === 172 && b >= 16 && b <= 31) return false;
	if (a === 192 && b === 168) return false;
	if (a === 127) return false;
	if (a === 169 && b === 254) return false;
	return true;
}

export function parseSnapshot(
	raw: ClashConnectionsRaw,
	clientsByIP: Map<string, string>,
): ConnectionsSnapshot {
	const rawConns = raw.connections ?? [];
	const connections: Connection[] = rawConns.map((c) => {
		const ip = c.metadata.sourceIP.toLowerCase();
		let clientName = clientsByIP.get(ip);
		if (!clientName && isPublicIP(ip)) {
			clientName = c.metadata.network === 'udp' ? 'UDP (post-NAT)' : 'TCP (post-NAT)';
		}
		return {
			...c,
			clientName,
			outboundLabel: chainOutboundLabel(c.chains),
		};
	});
	return {
		connections,
		downloadTotal: raw.downloadTotal ?? 0,
		uploadTotal: raw.uploadTotal ?? 0,
		connectionsTotal: connections.length,
	};
}

export function matchFilters(c: Connection, f: ConnectionFilters): boolean {
	if (f.network !== 'all' && c.metadata.network !== f.network) return false;
	if (f.outbound && (c.chains[0] ?? '') !== f.outbound) return false;
	if (f.rule && c.rule !== f.rule) return false;
	if (f.search) {
		const needle = f.search.toLowerCase();
		const hay = [
			c.metadata.host,
			c.metadata.sourceIP,
			c.metadata.destinationIP,
			c.clientName ?? '',
		]
			.join(' ')
			.toLowerCase();
		if (!hay.includes(needle)) return false;
	}
	return true;
}

export function aggregateBy(
	conns: Connection[],
	keyFn: (c: Connection) => string,
): ConnectionBucket[] {
	const acc = new Map<string, ConnectionBucket>();
	let totalDown = 0;
	for (const c of conns) {
		const k = keyFn(c);
		totalDown += c.download;
		const cur = acc.get(k) ?? { key: k, upload: 0, download: 0, count: 0, pct: 0 };
		cur.upload += c.upload;
		cur.download += c.download;
		cur.count += 1;
		acc.set(k, cur);
	}
	const out = Array.from(acc.values());
	for (const b of out) {
		b.pct = totalDown > 0 ? Math.round((b.download / totalDown) * 100) : 0;
	}
	out.sort((a, b) => b.download - a.download);
	return out;
}
