import type { AppID } from './apps.svelte.ts';

/** Banners at the top-right of the desktop (zd.notify from web apps). */
export type Banner = { id: number; app: AppID; title: string; body: string };

export const banners = $state<Banner[]>([]);
let next = 1;

export function notify(app: AppID, title: string, body: string) {
	const b = { id: next++, app, title: title.slice(0, 120), body: body.slice(0, 400) };
	banners.unshift(b);
	if (banners.length > 3) banners.length = 3;
	setTimeout(() => dismiss(b.id), 6000);
}

export function dismiss(id: number) {
	const i = banners.findIndex((b) => b.id === id);
	if (i >= 0) banners.splice(i, 1);
}
