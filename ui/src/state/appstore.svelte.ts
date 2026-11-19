// The App Store's catalogue and running downloads, kept outside the window
// so closing it does not lose an install in progress.

export type StoreApp = {
	id: string;
	package: string;
	title: string;
	summary: string;
	description: string;
	locales?: Record<string, { title?: string; summary?: string; description?: string }>;
	categories: string[];
	author: string;
	version: string;
	published_at: string;
	updated_at: string;
	license: string;
	source_repo: string;
	permissions?: Record<string, boolean>;
	icon: string;
	screenshots?: string[];
	size: number;
	installed?: string;
	update?: boolean;
	desktop_id?: string;
};

export type StoreTask = { pct: number; phase: 'auth' | 'waiting' | 'progress' };

export const store_state = $state({
	loaded: false,
	apps: [] as StoreApp[],
	tasks: {} as Record<string, StoreTask>,
});
