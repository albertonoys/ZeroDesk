import { dock_order, dock_room } from './dock.svelte.ts';
import { persisted } from './persisted.svelte.ts';

export type DeviceAppRef = { id: string; name: string };

/** Launchpad overlay (not a window, like macOS). */
export const launchpad = $state({
	open: false,
	/** an icon is being dragged out of Launchpad towards the Dock */
	dragging: false,
});

/** Device apps the user pinned to the Dock (per browser). */
export const pinned = persisted('zerodesk:dock-pins', { apps: [] as DeviceAppRef[] });

/**
 * Keep a device app in the Dock, at `index` in the Dock's order (the end if left out).
 * Returns false, and the Dock shakes, when there is no room for another app.
 */
export function pin(app: DeviceAppRef, index?: number): boolean {
	const key = `dev:${app.id}`;
	const known = pinned.apps.some((a) => a.id === app.id);
	if (!known && dock_order.ids.length >= dock_room.capacity) {
		window.dispatchEvent(new Event('zerodesk-dock-full'));
		return false;
	}
	pinned.apps = known ? pinned.apps.map((a) => (a.id === app.id ? app : a)) : [...pinned.apps, app];
	const rest = dock_order.ids.filter((k) => k !== key);
	const at = index === undefined ? rest.length : Math.max(0, Math.min(rest.length, index));
	dock_order.ids = [...rest.slice(0, at), key, ...rest.slice(at)];
	return true;
}

export function unpin(id: string) {
	pinned.apps = pinned.apps.filter((a) => a.id !== id);
	dock_order.ids = dock_order.ids.filter((k) => k !== `dev:${id}`);
}

export const DRAG_MIME = 'application/x-zerodesk-app';

/** Recently opened device apps, newest first (per browser). */
export const recents = persisted('zerodesk:recent-apps', { ids: [] as string[] });

export function note_recent(id: string) {
	recents.ids = [id, ...recents.ids.filter((x) => x !== id)].slice(0, 14);
}
