<script lang="ts">
	// An app's own web UI in a ZeroDesk window (docs/APP-SPEC.md).
	// The page runs in an opaque-origin sandbox; everything it may do on the
	// desktop goes through this postMessage bridge, checked against the
	// permissions in its manifest.
	import { onDestroy, onMount } from 'svelte';
	import { api, ApiError } from '🍎/lib/api.ts';
	import { apps, close_app, open_app, web_apps, web_windows, window_controls, type AppID } from '🍎/state/apps.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { notify } from '🍎/state/notifications.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';
	import Glyph from '../common/Glyph.svelte';

	const { window_id, is_being_dragged }: { window_id: AppID; is_being_dragged: boolean } = $props();

	const app_id = window_id.slice(4);
	const app = $derived(web_apps[app_id]);
	const title = $derived(web_windows[window_id]?.title || app?.name || '');

	let frame = $state<HTMLIFrameElement>();
	let src = $state('');
	let capability = '';
	let failure = $state<'' | 'device' | 'error'>('');
	let page_dragging = $state(false);

	onMount(async () => {
		try {
			const r = await api<{ base: string; start: string }>(`/api/webapps/${encodeURIComponent(app_id)}/session`, { method: 'POST' });
			capability = r.base.split('/')[2];
			src = r.base + r.start;
		} catch (e) {
			failure = e instanceof ApiError && e.status === 409 ? 'device' : 'error';
		}
	});

	onDestroy(() => {
		if (capability) fetch(`/api/webapps/session/${capability}`, { method: 'DELETE', credentials: 'same-origin' }).catch(() => {});
	});

	// ---- bridge ----

	function send(msg: Record<string, unknown>) {
		// $state proxies cannot be structured-cloned
		frame?.contentWindow?.postMessage($state.snapshot({ zd: 1, ...msg }), '*');
	}

	function init_data() {
		return { app: app_id, theme: preferences.theme.scheme, lang: preferences.lang, permissions: app?.permissions ?? [] };
	}

	$effect(() => {
		const scheme = preferences.theme.scheme;
		send({ event: 'theme', data: scheme });
	});
	$effect(() => {
		const lang = preferences.lang;
		send({ event: 'lang', data: lang });
	});

	let storage: Record<string, unknown> | null = null;
	async function load_storage() {
		storage ??= await api<Record<string, unknown>>(`/api/webapps/${encodeURIComponent(app_id)}/storage`);
		return storage;
	}

	/** Paths the page may touch: inside the device user's home. */
	function home_path(p: unknown): string {
		const home = session.home.replace(/\/$/, '');
		let s = String(p ?? '');
		if (!s.startsWith('/')) s = `${home}/${s}`;
		const parts: string[] = [];
		for (const seg of s.split('/')) {
			if (!seg || seg === '.') continue;
			if (seg === '..') throw new Error('path outside home');
			parts.push(seg);
		}
		const full = '/' + parts.join('/');
		if (full !== home && !full.startsWith(home + '/')) throw new Error('path outside home');
		return full;
	}

	function need(permission: string) {
		if (!app?.permissions.includes(permission)) throw new Error(`permission: ${permission}`);
	}

	async function handle(method: string, p: Record<string, unknown>): Promise<unknown> {
		const ctl = window_controls[window_id];
		switch (method) {
			case 'hello':
				send({ event: 'init', data: init_data() });
				return;
			case 'window.focus':
				if (apps.active !== window_id) open_app(window_id);
				return;
			case 'window.setTitle':
				if (web_windows[window_id]) web_windows[window_id].title = String(p.title ?? '').slice(0, 80);
				return;
			case 'window.resize':
				ctl?.resize(Number(p.width) || 0, Number(p.height) || 0);
				return;
			case 'window.close':
				close_app(window_id);
				return;
			case 'window.dragStart':
				page_dragging = true;
				if (apps.active !== window_id) open_app(window_id);
				return;
			case 'window.dragMove':
				ctl?.move_by(Number(p.dx) || 0, Number(p.dy) || 0);
				return;
			case 'window.dragEnd':
				page_dragging = false;
				return;
			case 'window.zoom':
				ctl?.zoom();
				return;
			case 'storage.get': {
				need('storage');
				const s = await load_storage();
				return s[String(p.key)] ?? null;
			}
			case 'storage.set': {
				need('storage');
				const key = String(p.key);
				await api(`/api/webapps/${encodeURIComponent(app_id)}/storage`, {
					method: 'PUT',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ [key]: p.value ?? null }),
				}).catch((e) => {
					// 204 has no JSON body
					if (!(e instanceof SyntaxError)) throw e;
				});
				const s = await load_storage();
				if (p.value == null) delete s[key];
				else s[key] = p.value;
				return;
			}
			case 'notify':
				need('notifications');
				notify(window_id, String(p.title ?? ''), String(p.body ?? ''));
				return;
			case 'files.list': {
				need('files.read');
				const r = await api<{ entries: { name: string; path: string; dir: boolean; size: number; modified: string }[] }>(
					`/api/fs/list?path=${encodeURIComponent(home_path(p.path))}`,
				);
				return r.entries.map((e) => ({ name: e.name, path: e.path, dir: e.dir, size: e.size, modified: e.modified }));
			}
			case 'files.read': {
				need('files.read');
				const res = await fetch(`/api/fs/get?path=${encodeURIComponent(home_path(p.path))}`, { credentials: 'same-origin' });
				if (!res.ok) throw new Error(res.statusText);
				const blob = await res.blob();
				if (blob.size > 16 << 20) throw new Error('file too large');
				const bytes = new Uint8Array(await blob.arrayBuffer());
				let bin = '';
				for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
				return { data: btoa(bin), type: blob.type };
			}
			case 'files.write': {
				need('files.write');
				const full = home_path(p.path);
				const dir = full.slice(0, full.lastIndexOf('/')) || '/';
				const name = full.slice(full.lastIndexOf('/') + 1);
				const bin = atob(String(p.data ?? ''));
				const bytes = new Uint8Array(bin.length);
				for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
				const form = new FormData();
				form.append('file', new Blob([bytes]), name);
				await api(`/api/fs/upload?dir=${encodeURIComponent(dir)}`, { method: 'POST', body: form });
				return;
			}
			case 'device.info':
				need('device.info');
				return api('/api/sys');
			default:
				throw new Error(`unknown method: ${method}`);
		}
	}

	function on_message(e: MessageEvent) {
		if (!frame || e.source !== frame.contentWindow) return;
		const m = e.data as { zd?: number; id?: number; method?: string; params?: Record<string, unknown> };
		if (!m || m.zd !== 1 || typeof m.method !== 'string') return;
		handle(m.method, m.params ?? {}).then(
			(result) => m.id && send({ id: m.id, result: result ?? null }),
			(err: Error) => m.id && send({ id: m.id, error: err?.message || 'error' }),
		);
	}
</script>

<svelte:window onmessage={on_message} />

<div class="web" class:app-titlebar={app?.titlebar === 'app'}>
	{#if app?.titlebar !== 'app'}
		<header class="titlebar app-window-drag-handle">
			<span class="title">{title}</span>
		</header>
	{/if}
	{#if failure}
		<div class="empty">
			<Glyph name={failure === 'device' ? 'device_laptop' : 'exclamationmark_triangle'} size={72} />
			<p>{failure === 'device' ? t('web.on_device') : t('web.unavailable')}</p>
		</div>
	{:else if src}
		<iframe
			bind:this={frame}
			{src}
			{title}
			sandbox="allow-scripts allow-forms allow-modals allow-popups allow-downloads allow-pointer-lock"
			allow="fullscreen; clipboard-write"
			referrerpolicy="no-referrer"
			class:no-pointer={is_being_dragged && !page_dragging}
			onload={() => send({ event: 'init', data: init_data() })}
		></iframe>
	{/if}
</div>

<style>
	.web {
		position: relative;
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		background: var(--zd-window-bg);
	}

	.titlebar {
		flex: none;
		height: var(--zd-titlebar);
		display: flex;
		align-items: center;
		padding: 0 16px 0 92px;
		box-shadow: 0 0.5px 0 var(--zd-hairline-strong);
	}

	.title {
		font-size: 15px;
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	iframe {
		flex: 1;
		min-height: 0;
		width: 100%;
		border: 0;
		display: block;
		background: transparent;
		color-scheme: normal;

		&.no-pointer {
			pointer-events: none;
		}
	}

	.empty {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 14px;
		color: var(--zd-text-tertiary);

		p {
			margin: 0;
			font-size: 22px;
			font-weight: 700;
			text-align: center;
			padding: 0 20px;
		}
	}
</style>
