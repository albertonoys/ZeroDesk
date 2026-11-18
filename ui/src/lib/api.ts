import { session } from '🍎/state/session.svelte.ts';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
		/** the action needs the device user's password (agent/privileged.go) */
		public need_password = false,
	) {
		super(message);
	}
}

export async function api_error(res: Response): Promise<ApiError> {
	try {
		const body = await res.json();
		return new ApiError(res.status, body?.error ?? res.statusText, !!body?.need_password);
	} catch {
		return new ApiError(res.status, res.statusText);
	}
}

export async function api<T = unknown>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(path, { credentials: 'same-origin', ...init });
	if (res.status === 401) {
		session.authed = false;
		throw new ApiError(401, 'unauthorized');
	}
	if (!res.ok) throw await api_error(res);
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
