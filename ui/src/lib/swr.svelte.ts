// Stale-while-revalidate data for app windows: a window paints its last
// known data at once and refreshes in the background, like a macOS app that
// reopens with its previous state. Shared across window opens, kept in
// sessionStorage so a reload paints instantly too, and prefetched when the
// pointer reaches an app's icon.

const PREFIX = 'zerodesk:swr:';
const MAX_ENTRY = 256 * 1024; // keep sessionStorage small

type Entry = { data: unknown; at: number };

const mem = new Map<string, Entry>();
const inflight = new Map<string, Promise<unknown>>();
/** reactive version per key, so views re-read after a refresh */
const versions = $state<Record<string, number>>({});

function read_storage(key: string): Entry | undefined {
	try {
		const raw = sessionStorage.getItem(PREFIX + key);
		return raw ? (JSON.parse(raw) as Entry) : undefined;
	} catch {
		return undefined;
	}
}

function write_storage(key: string, e: Entry) {
	try {
		const raw = JSON.stringify(e);
		if (raw.length <= MAX_ENTRY) sessionStorage.setItem(PREFIX + key, raw);
	} catch {
		/* private mode or full: memory only */
	}
}

/** The last data for key, if any (memory first, then this tab's storage). */
export function cached<T>(key: string): T | undefined {
	void versions[key];
	let e = mem.get(key);
	if (!e) {
		e = read_storage(key);
		if (e) mem.set(key, e);
	}
	return e?.data as T | undefined;
}

/** Age of the cached data in ms (Infinity when there is none). */
export function age(key: string): number {
	const e = mem.get(key) ?? read_storage(key);
	return e ? Date.now() - e.at : Infinity;
}

export function store<T>(key: string, data: T) {
	const e = { data, at: Date.now() };
	mem.set(key, e);
	write_storage(key, e);
	versions[key] = (versions[key] ?? 0) + 1;
}

/** Fetch now (one request per key at a time) and remember the result. */
export function revalidate<T>(key: string, fetcher: () => Promise<T>): Promise<T> {
	let p = inflight.get(key) as Promise<T> | undefined;
	if (!p) {
		p = fetcher()
			.then((d) => (store(key, d), d))
			.finally(() => inflight.delete(key));
		inflight.set(key, p);
	}
	return p;
}

/** Warm the cache unless it is fresher than max_age ms. Errors are ignored. */
export function prefetch<T>(key: string, fetcher: () => Promise<T>, max_age = 5000) {
	if (age(key) < max_age || inflight.has(key)) return;
	revalidate(key, fetcher).catch(() => {});
}
