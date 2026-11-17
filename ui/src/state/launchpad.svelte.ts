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

export function pin(app: DeviceAppRef, index?: number) {
	const rest = pinned.apps.filter((a) => a.id !== app.id);
	const at = index === undefined ? rest.length : Math.max(0, Math.min(rest.length, index));
	pinned.apps = [...rest.slice(0, at), app, ...rest.slice(at)];
}

export function unpin(id: string) {
	pinned.apps = pinned.apps.filter((a) => a.id !== id);
}

export const DRAG_MIME = 'application/x-zerodesk-app';

/** Recently opened device apps, newest first (per browser). */
export const recents = persisted('zerodesk:recent-apps', { ids: [] as string[] });

export function note_recent(id: string) {
	recents.ids = [id, ...recents.ids.filter((x) => x !== id)].slice(0, 14);
}
