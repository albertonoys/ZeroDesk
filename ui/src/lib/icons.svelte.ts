// Icons paint in the first frame of the Dock and Apps: every URL is
// versioned (so the browser keeps it for good), the device tells how to draw
// each app icon up front, and all of them are decoded before Apps opens.

declare const __ICON_V__: string;

/** A built-in icon (ui/public/icons/<name>.webp), cached for good per build. */
export const icon_url = (name: string) => `/icons/${name}.webp?v=${__ICON_V__}`;

export type DeviceAppIcon = { id: string; icon?: string; iconOpaque?: boolean };

/** id -> how to draw a device app's own icon (from /api/apps) */
export const device_icons = $state<Record<string, { src: string; opaque: boolean }>>({});

export function learn_device_icons(apps: DeviceAppIcon[]) {
	for (const a of apps) {
		if (a.icon) device_icons[a.id] = { src: a.icon, opaque: a.iconOpaque !== false };
	}
}

const decoded = new Map<string, Promise<void>>();
const done = new Set<string>();

/** Already downloaded and decoded (paints in the next frame). */
export const is_decoded = (src: string) => done.has(src);

/** Download and decode once; resolves even on failure. */
export function decode(src: string): Promise<void> {
	let p = decoded.get(src);
	if (!p) {
		const img = new Image();
		img.decoding = 'async';
		img.src = src;
		p = img
			.decode()
			.catch(() => {})
			.then(() => void done.add(src));
		decoded.set(src, p);
	}
	return p;
}

/** Decode all, but never wait longer than max_ms. */
export function decode_all(srcs: string[], max_ms = 1500): Promise<void> {
	return Promise.race([Promise.all(srcs.map(decode)).then(() => {}), new Promise<void>((r) => setTimeout(r, max_ms))]);
}
