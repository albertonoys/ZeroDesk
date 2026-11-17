import { session } from '🍎/state/session.svelte.ts';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
	) {
		super(message);
	}
}

async function parse_error(res: Response): Promise<string> {
	try {
		const body = await res.json();
		return body?.error ?? res.statusText;
	} catch {
		return res.statusText;
	}
}

export async function api<T = unknown>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(path, { credentials: 'same-origin', ...init });
	if (res.status === 401) {
		session.authed = false;
		throw new ApiError(401, 'unauthorized');
	}
	if (!res.ok) throw new ApiError(res.status, await parse_error(res));
	return res.json() as Promise<T>;
}

export function post<T = unknown>(path: string, body: unknown): Promise<T> {
	return api<T>(path, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body),
	});
}

export function ws_url(path: string): string {
	const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
	return `${proto}//${location.host}${path}`;
}
