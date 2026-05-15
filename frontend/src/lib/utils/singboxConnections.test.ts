import { describe, it, expect } from 'vitest';
import {
	chainOutboundLabel,
	parseSnapshot,
	matchFilters,
	aggregateBy,
	isPublicIP,
} from './singboxConnections';
import type {
	ClashConnectionsRaw,
	Connection,
	ConnectionFilters,
} from '$lib/types/singboxConnections';

describe('chainOutboundLabel', () => {
	it('returns "—" for empty chains', () => {
		expect(chainOutboundLabel([])).toBe('—');
	});
	it('translates DIRECT to a Russian label', () => {
		expect(chainOutboundLabel(['DIRECT'])).toBe('Прямое');
	});
	it('translates REJECT to a Russian label', () => {
		expect(chainOutboundLabel(['REJECT'])).toBe('Отклонено');
	});
	it('returns chains[0] for everything else', () => {
		expect(chainOutboundLabel(['vless-1', 'auto'])).toBe('vless-1');
	});
});

describe('parseSnapshot', () => {
	const baseRaw: ClashConnectionsRaw = {
		downloadTotal: 1234,
		uploadTotal: 567,
		connections: [
			{
				id: 'a',
				metadata: {
					network: 'tcp',
					type: 'Tun',
					sourceIP: '192.168.1.5',
					sourcePort: '53412',
					destinationIP: '142.250.74.110',
					destinationPort: '443',
					host: 'youtube.com',
				},
				upload: 100,
				download: 800,
				start: '2026-05-02T10:00:00Z',
				chains: ['vless-1'],
				rule: 'DOMAIN-SUFFIX',
				rulePayload: 'youtube.com',
			},
		],
	};

	it('enriches clientName from IP map (case-insensitive lookup)', () => {
		const clients = new Map([['192.168.1.5', 'iPhone']]);
		const snap = parseSnapshot(baseRaw, clients);
		expect(snap.connections[0].clientName).toBe('iPhone');
	});

	it('lowercases sourceIP for lookup', () => {
		const raw = structuredClone(baseRaw);
		raw.connections![0].metadata.sourceIP = 'FE80::1';
		const clients = new Map([['fe80::1', 'ipv6']]);
		const snap = parseSnapshot(raw, clients);
		expect(snap.connections[0].clientName).toBe('ipv6');
	});

	it('leaves clientName undefined when no match', () => {
		const snap = parseSnapshot(baseRaw, new Map());
		expect(snap.connections[0].clientName).toBeUndefined();
	});

	it('computes outboundLabel from chains[0]', () => {
		const snap = parseSnapshot(baseRaw, new Map());
		expect(snap.connections[0].outboundLabel).toBe('vless-1');
	});

	it('handles empty connections array', () => {
		const snap = parseSnapshot({ connections: [], downloadTotal: 0, uploadTotal: 0 }, new Map());
		expect(snap.connections).toEqual([]);
		expect(snap.connectionsTotal).toBe(0);
	});

	it('handles missing connections field', () => {
		const snap = parseSnapshot({}, new Map());
		expect(snap.connections).toEqual([]);
	});

	it('passes through totals', () => {
		const snap = parseSnapshot(baseRaw, new Map());
		expect(snap.downloadTotal).toBe(1234);
		expect(snap.uploadTotal).toBe(567);
		expect(snap.connectionsTotal).toBe(1);
	});
});

function makeConn(
	over: Omit<Partial<Connection>, 'metadata'> & {
		metadata?: Partial<Connection['metadata']>;
	} = {},
): Connection {
	const meta = {
		network: 'tcp' as const,
		type: 'Tun',
		sourceIP: '192.168.1.5',
		sourcePort: '53000',
		destinationIP: '1.1.1.1',
		destinationPort: '443',
		host: 'example.com',
		...(over.metadata ?? {}),
	};
	const { metadata: _ignored, ...rest } = over;
	return {
		id: 'x',
		upload: 0,
		download: 0,
		start: '2026-05-02T10:00:00Z',
		chains: ['vless-1'],
		rule: 'DOMAIN',
		rulePayload: '',
		outboundLabel: 'vless-1',
		...rest,
		metadata: meta,
	};
}

const empty: ConnectionFilters = { search: '', outbound: '', network: 'all', rule: '' };

describe('matchFilters', () => {
	it('matches everything when filters empty', () => {
		const c = makeConn();
		expect(matchFilters(c, empty)).toBe(true);
	});
	it('search matches host (case-insensitive)', () => {
		const c = makeConn({ metadata: { host: 'YouTube.com' } });
		expect(matchFilters(c, { ...empty, search: 'youtube' })).toBe(true);
	});
	it('search matches sourceIP', () => {
		const c = makeConn({ metadata: { sourceIP: '10.0.0.1' } });
		expect(matchFilters(c, { ...empty, search: '10.0.' })).toBe(true);
	});
	it('search matches destinationIP', () => {
		const c = makeConn({ metadata: { destinationIP: '8.8.8.8' } });
		expect(matchFilters(c, { ...empty, search: '8.8.8' })).toBe(true);
	});
	it('search matches clientName', () => {
		const c = makeConn({ clientName: 'Anyas-iPhone' });
		expect(matchFilters(c, { ...empty, search: 'anyas' })).toBe(true);
	});
	it('search misses → false', () => {
		const c = makeConn({ metadata: { host: 'example.com' } });
		expect(matchFilters(c, { ...empty, search: 'youtube' })).toBe(false);
	});
	it('outbound exact-matches chains[0]', () => {
		const c = makeConn({ chains: ['vless-1'] });
		expect(matchFilters(c, { ...empty, outbound: 'vless-1' })).toBe(true);
		expect(matchFilters(c, { ...empty, outbound: 'vless-2' })).toBe(false);
	});
	it('outbound empty chains never matches non-empty filter', () => {
		const c = makeConn({ chains: [] });
		expect(matchFilters(c, { ...empty, outbound: 'vless-1' })).toBe(false);
	});
	it('network filter tcp/udp/all', () => {
		const tcp = makeConn({ metadata: { network: 'tcp' } });
		const udp = makeConn({ metadata: { network: 'udp' } });
		expect(matchFilters(tcp, { ...empty, network: 'tcp' })).toBe(true);
		expect(matchFilters(udp, { ...empty, network: 'tcp' })).toBe(false);
		expect(matchFilters(tcp, { ...empty, network: 'all' })).toBe(true);
	});
	it('rule exact-matches', () => {
		const c = makeConn({ rule: 'RULE-SET' });
		expect(matchFilters(c, { ...empty, rule: 'RULE-SET' })).toBe(true);
		expect(matchFilters(c, { ...empty, rule: 'GEOIP' })).toBe(false);
	});
});

describe('aggregateBy', () => {
	it('returns [] for empty input', () => {
		expect(aggregateBy([], (c) => c.outboundLabel)).toEqual([]);
	});
	it('groups + sums + sorts desc by download', () => {
		const conns = [
			makeConn({ id: '1', outboundLabel: 'A', download: 100, upload: 10 }),
			makeConn({ id: '2', outboundLabel: 'A', download: 200, upload: 20 }),
			makeConn({ id: '3', outboundLabel: 'B', download: 50, upload: 5 }),
		];
		const buckets = aggregateBy(conns, (c) => c.outboundLabel);
		expect(buckets[0].key).toBe('A');
		expect(buckets[0].download).toBe(300);
		expect(buckets[0].upload).toBe(30);
		expect(buckets[0].count).toBe(2);
		expect(buckets[1].key).toBe('B');
		expect(buckets[1].download).toBe(50);
	});
	it('pct rounds against total download', () => {
		const conns = [
			makeConn({ id: '1', outboundLabel: 'A', download: 750 }),
			makeConn({ id: '2', outboundLabel: 'B', download: 250 }),
		];
		const [a, b] = aggregateBy(conns, (c) => c.outboundLabel);
		expect(a.pct).toBe(75);
		expect(b.pct).toBe(25);
	});
	it('pct=0 when all downloads zero', () => {
		const conns = [makeConn({ id: '1', outboundLabel: 'A', download: 0 })];
		const [a] = aggregateBy(conns, (c) => c.outboundLabel);
		expect(a.pct).toBe(0);
	});
});

describe('isPublicIP', () => {
	it('classifies RFC1918 IPv4 as private', () => {
		expect(isPublicIP('10.10.10.50')).toBe(false);
		expect(isPublicIP('10.0.0.1')).toBe(false);
		expect(isPublicIP('172.16.0.1')).toBe(false);
		expect(isPublicIP('172.31.255.255')).toBe(false);
		expect(isPublicIP('192.168.1.1')).toBe(false);
	});

	it('respects 172.16/12 boundaries (15 and 32 are PUBLIC)', () => {
		expect(isPublicIP('172.15.0.1')).toBe(true);
		expect(isPublicIP('172.32.0.1')).toBe(true);
	});

	it('classifies loopback + link-local IPv4 as private', () => {
		expect(isPublicIP('127.0.0.1')).toBe(false);
		expect(isPublicIP('127.255.255.254')).toBe(false);
		expect(isPublicIP('169.254.1.1')).toBe(false);
	});

	it('classifies real public IPv4 as public', () => {
		expect(isPublicIP('178.205.128.207')).toBe(true); // router WAN from production
		expect(isPublicIP('157.240.0.63')).toBe(true); // Instagram
		expect(isPublicIP('8.8.8.8')).toBe(true);
		expect(isPublicIP('1.1.1.1')).toBe(true);
		expect(isPublicIP('100.64.0.1')).toBe(true); // CG-NAT — treated as public by design
	});

	it('classifies IPv6 loopback + link-local + ULA as private', () => {
		expect(isPublicIP('::1')).toBe(false);
		expect(isPublicIP('::')).toBe(false);
		expect(isPublicIP('fe80::1')).toBe(false);
		expect(isPublicIP('fe80::abcd:1234:5678:9abc')).toBe(false);
		expect(isPublicIP('FE80::1')).toBe(false); // case-insensitive
		expect(isPublicIP('fc00::1')).toBe(false);
		expect(isPublicIP('fd12:3456:789a::1')).toBe(false);
	});

	it('classifies global IPv6 as public', () => {
		expect(isPublicIP('2001:db8::1')).toBe(true);
		expect(isPublicIP('2606:4700::1111')).toBe(true);
	});

	it('returns false for malformed input', () => {
		expect(isPublicIP('')).toBe(false);
		expect(isPublicIP('not-an-ip')).toBe(false);
		expect(isPublicIP('256.0.0.1')).toBe(false);
		expect(isPublicIP('1.2.3')).toBe(false);
		expect(isPublicIP('1.2.3.4.5')).toBe(false);
	});
});

describe('parseSnapshot — post-NAT relabel', () => {
	function makeRaw(network: 'tcp' | 'udp', sourceIP: string): ClashConnectionsRaw {
		return {
			downloadTotal: 0,
			uploadTotal: 0,
			connections: [
				{
					id: 'a',
					metadata: {
						network,
						type: 'TProxy',
						sourceIP,
						sourcePort: '43424',
						destinationIP: '157.240.0.63',
						destinationPort: '443',
						host: 'instagram.com',
					},
					upload: 0,
					download: 0,
					start: '2026-05-12T10:00:00Z',
					chains: ['awg-sys-Wireguard0'],
					rule: 'rule_set=geosite-instagram',
					rulePayload: '',
				},
			],
		};
	}

	// Production regression: motorola's UDP flows reach sing-box with
	// the router's WAN IP as source because Mediatek FASTNAT applies
	// MASQUERADE before the kernel hands the packet to the TPROXY
	// socket. Without the relabel, ten such flows appear as a single
	// raw-IP bucket the user mistakes for "another client".
	it('UDP + public source → "UDP (post-NAT)"', () => {
		const snap = parseSnapshot(makeRaw('udp', '178.205.128.207'), new Map());
		expect(snap.connections[0].clientName).toBe('UDP (post-NAT)');
	});

	it('TCP + public source → "TCP (post-NAT)" (rare edge case)', () => {
		const snap = parseSnapshot(makeRaw('tcp', '178.205.128.207'), new Map());
		expect(snap.connections[0].clientName).toBe('TCP (post-NAT)');
	});

	it('private source with no clientsByIP match → undefined (raw IP shown in UI)', () => {
		const snap = parseSnapshot(makeRaw('udp', '10.10.10.50'), new Map());
		expect(snap.connections[0].clientName).toBeUndefined();
	});

	it('explicit clientsByIP wins over post-NAT relabel for private source', () => {
		const snap = parseSnapshot(
			makeRaw('tcp', '10.10.10.50'),
			new Map([['10.10.10.50', 'motorola-razr-40-ultra']]),
		);
		expect(snap.connections[0].clientName).toBe('motorola-razr-40-ultra');
	});

	// Practical scenario: 10 UDP flows from various Instagram CDN IPs
	// all show up as the router's WAN — after relabel they bucket
	// into a single "UDP (post-NAT)" entry instead of crowding the
	// "By Client" panel with one raw-IP entry per flow.
	it('multiple public-source UDP flows collapse into one bucket', () => {
		const raw: ClashConnectionsRaw = {
			downloadTotal: 0,
			uploadTotal: 0,
			connections: [1, 2, 3, 4, 5].map((i) => ({
				id: `c${i}`,
				metadata: {
					network: 'udp' as const,
					type: 'TProxy',
					sourceIP: '178.205.128.207',
					sourcePort: `${40000 + i}`,
					destinationIP: '157.240.0.63',
					destinationPort: '443',
					host: 'instagram.com',
				},
				upload: 0,
				download: 1000,
				start: '2026-05-12T10:00:00Z',
				chains: ['awg-sys-Wireguard0'],
				rule: 'rule_set=geosite-instagram',
				rulePayload: '',
			})),
		};
		const snap = parseSnapshot(raw, new Map());
		const buckets = aggregateBy(
			snap.connections,
			(c) => c.clientName || c.metadata.sourceIP,
		);
		expect(buckets).toHaveLength(1);
		expect(buckets[0].key).toBe('UDP (post-NAT)');
		expect(buckets[0].count).toBe(5);
	});
});
