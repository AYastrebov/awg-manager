import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import RulesSubTab from './RulesSubTab.svelte';

// Hoist Svelte writables that the mocked store will expose. Tests mutate
// the rules store directly to drive the rendered table.
const { statusStore, rulesStore, ruleSetsStore, optionsStore, pageStore } = vi.hoisted(() => {
	const { writable } = require('svelte/store') as typeof import('svelte/store');
	return {
		statusStore: writable<unknown>(null),
		rulesStore: writable<unknown[]>([]),
		ruleSetsStore: writable<unknown[]>([]),
		optionsStore: writable<unknown[]>([]),
		pageStore: writable<{ url: URL }>({ url: new URL('http://localhost/routing?sub=rules') }),
	};
});

vi.mock('$lib/stores/singboxRouter', () => ({
	singboxRouter: {
		status: { subscribe: statusStore.subscribe },
		rules: { subscribe: rulesStore.subscribe },
		ruleSets: { subscribe: ruleSetsStore.subscribe },
		options: { subscribe: optionsStore.subscribe },
		loadAll: vi.fn().mockResolvedValue(undefined),
	},
}));

vi.mock('$lib/api/client', () => ({
	api: {
		singboxRouterMoveRule: vi.fn().mockResolvedValue(undefined),
	},
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('$app/stores', () => ({
	page: { subscribe: pageStore.subscribe },
}));

vi.mock('$lib/stores/notifications', () => ({
	notifications: {
		success: vi.fn(),
		error: vi.fn(),
		warning: vi.fn(),
		info: vi.fn(),
	},
}));

// Rule shape mirrors SingboxRouterRule. Subset that RulesSubTab reads.
type R = {
	action?: string;
	outbound?: string;
	protocol?: string;
	domain_suffix?: string[];
	ip_cidr?: string[];
	ip_is_private?: boolean;
};

// Standard ordering: 3 system rules (sniff, hijack-dns, ip_is_private) at
// top, then user rules. firstUserRuleIndex = 3 in this layout.
const systemRules: R[] = [
	{ action: 'sniff' },
	{ action: 'hijack-dns', protocol: 'dns' },
	{ ip_is_private: true, outbound: 'direct' },
];

function buildRules(userRules: R[]): R[] {
	return [...systemRules, ...userRules];
}

beforeEach(() => {
	vi.clearAllMocks();
	statusStore.set({ final: 'direct' });
	ruleSetsStore.set([]);
	optionsStore.set([]);
	pageStore.set({ url: new URL('http://localhost/routing?sub=rules') });
});

describe('RulesSubTab — rule ordering controls (PR #101 commit #3)', () => {
	it('renders no move/drag/edit controls for system rules (sniff, hijack-dns, ip_is_private)', () => {
		rulesStore.set(buildRules([{ action: 'route', outbound: 'veesp' }]));
		const { container } = render(RulesSubTab);

		const rows = container.querySelectorAll('.t-row');
		expect(rows.length).toBe(4); // 3 system + 1 user

		// All 3 system rows must NOT contain drag-handle or move-btn.
		for (let i = 0; i < 3; i++) {
			expect(rows[i].querySelector('.drag-handle')).toBeNull();
			expect(rows[i].querySelector('.move-btn')).toBeNull();
		}

		// User row must have BOTH controls.
		const userRow = rows[3];
		expect(userRow.querySelector('.drag-handle')).toBeTruthy();
		expect(userRow.querySelectorAll('.move-btn').length).toBe(2);
	});

	it('renders ip_is_private system rule with BYPASS label and direct outbound', () => {
		rulesStore.set(buildRules([]));
		const { container } = render(RulesSubTab);

		const rows = container.querySelectorAll('.t-row');
		expect(rows.length).toBe(3); // 3 system + 0 user

		// rows[2] is ip_is_private. Should render BYPASS badge and
		// "direct" outbound, even though `action` is omitted.
		const ipPrivateRow = rows[2];
		const actionBadge = ipPrivateRow.querySelector('.col-action');
		expect(actionBadge?.textContent).toContain('BYPASS');

		const outboundCol = ipPrivateRow.querySelector('.col-out');
		expect(outboundCol?.textContent).toContain('direct');
	});

	it('disables ↑ on the first user rule (cannot escape system zone)', () => {
		rulesStore.set(buildRules([
			{ action: 'route', outbound: 'veesp' },
			{ action: 'route', outbound: 'awg-1' },
		]));
		const { container } = render(RulesSubTab);

		const rows = container.querySelectorAll('.t-row');
		// rows[3] is the first user rule (firstUserRuleIndex == 3).
		const firstUserBtns = rows[3].querySelectorAll('.move-btn');
		const upBtn = firstUserBtns[0] as HTMLButtonElement;
		const downBtn = firstUserBtns[1] as HTMLButtonElement;
		expect(upBtn.disabled).toBe(true); // i <= firstUserRuleIndex
		expect(downBtn.disabled).toBe(false); // can go down
	});

	it('enables ↑ on a middle user rule (room to move up within user zone)', () => {
		rulesStore.set(buildRules([
			{ action: 'route', outbound: 'veesp' },
			{ action: 'route', outbound: 'awg-1' },
			{ action: 'route', outbound: 'sub-1' },
		]));
		const { container } = render(RulesSubTab);

		const rows = container.querySelectorAll('.t-row');
		// rows[4] is the middle user rule (index 4, firstUserRuleIndex == 3).
		const midBtns = rows[4].querySelectorAll('.move-btn');
		expect((midBtns[0] as HTMLButtonElement).disabled).toBe(false);
		expect((midBtns[1] as HTMLButtonElement).disabled).toBe(false);
	});

	it('disables ↓ on the last rule (no room to go further down)', () => {
		rulesStore.set(buildRules([
			{ action: 'route', outbound: 'veesp' },
			{ action: 'route', outbound: 'awg-1' },
		]));
		const { container } = render(RulesSubTab);

		const rows = container.querySelectorAll('.t-row');
		const lastBtns = rows[rows.length - 1].querySelectorAll('.move-btn');
		expect((lastBtns[1] as HTMLButtonElement).disabled).toBe(true); // i >= rules.length-1
	});

	it('clicking ↓ on a user rule calls api.singboxRouterMoveRule(i, i+1)', async () => {
		rulesStore.set(buildRules([
			{ action: 'route', outbound: 'veesp' },
			{ action: 'route', outbound: 'awg-1' },
		]));
		const { container } = render(RulesSubTab);
		const { api } = await import('$lib/api/client');

		const rows = container.querySelectorAll('.t-row');
		// rows[3] = first user rule (index 3). Click its ↓.
		const downBtn = rows[3].querySelectorAll('.move-btn')[1] as HTMLButtonElement;
		await fireEvent.click(downBtn);

		expect(api.singboxRouterMoveRule).toHaveBeenCalledOnce();
		expect(api.singboxRouterMoveRule).toHaveBeenCalledWith(3, 4);
	});

	it('clicking ↑ on a movable rule calls api.singboxRouterMoveRule(i, i-1)', async () => {
		rulesStore.set(buildRules([
			{ action: 'route', outbound: 'veesp' },
			{ action: 'route', outbound: 'awg-1' },
		]));
		const { container } = render(RulesSubTab);
		const { api } = await import('$lib/api/client');

		const rows = container.querySelectorAll('.t-row');
		// rows[4] = second user rule (index 4). Click its ↑.
		const upBtn = rows[4].querySelectorAll('.move-btn')[0] as HTMLButtonElement;
		await fireEvent.click(upBtn);

		expect(api.singboxRouterMoveRule).toHaveBeenCalledOnce();
		expect(api.singboxRouterMoveRule).toHaveBeenCalledWith(4, 3);
	});
});
