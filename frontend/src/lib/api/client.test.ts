import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { api } from './client';

describe('ApiClient error shape', () => {
	const originalFetch = globalThis.fetch;

	beforeEach(() => {
		vi.restoreAllMocks();
	});

	afterEach(() => {
		globalThis.fetch = originalFetch;
	});

	it('attaches status and parsed body to the thrown Error on 422', async () => {
		const fakeBody = {
			sbCheck:
				'FATAL[0000] initialize dns router: dns rule[0]: rule-set not found: geosite-google\n: exit status 1',
		};
		globalThis.fetch = vi.fn().mockResolvedValue(
			new Response(JSON.stringify(fakeBody), {
				status: 422,
				headers: { 'Content-Type': 'application/json' },
			}),
		);

		let caught: unknown;
		try {
			await api.singboxRouterStagingApply();
		} catch (e) {
			caught = e;
		}
		expect(caught).toBeInstanceOf(Error);
		const err = caught as Error & { status?: number; body?: unknown };
		expect(err.status).toBe(422);
		expect(err.body).toEqual(fakeBody);
	});

	it('attaches status and body on a standard envelope error too', async () => {
		const fakeBody = { error: true, message: 'тест', code: 'TEST' };
		globalThis.fetch = vi.fn().mockResolvedValue(
			new Response(JSON.stringify(fakeBody), {
				status: 400,
				headers: { 'Content-Type': 'application/json' },
			}),
		);

		let caught: unknown;
		try {
			await api.singboxRouterStagingApply();
		} catch (e) {
			caught = e;
		}
		const err = caught as Error & { status?: number; body?: unknown };
		expect(err.status).toBe(400);
		expect(err.body).toEqual(fakeBody);
		expect(err.message).toBe('тест');
	});
});
