import { error } from '@sveltejs/kit';

export const prerender = false;
export const ssr = false;

export function load() {
	error(404, 'Moved to /api-docs');
}
