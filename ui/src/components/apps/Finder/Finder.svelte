<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { api, ApiError, post } from '🍎/lib/api.ts';
	import { format_bytes, format_date, t, type MsgKey } from '🍎/state/i18n.svelte.ts';
	import Toolbar from '../common/Toolbar.svelte';
	import ToolButton from '../common/ToolButton.svelte';
	import { show_context_menu } from '🍎/state/contextmenu.svelte.ts';
	import { APPS_LOCATION, FILE_MIME, finder_nav, TRASH_LOCATION } from '🍎/state/finder_nav.svelte.ts';
	import AppsView from './AppsView.svelte';
	import FileGlyph from './FileGlyph.svelte';
	import TrashView from './TrashView.svelte';
	import InstallSheet from './InstallSheet.svelte';
	import QuickLook from './QuickLook.svelte';

	type Entry = { name: string; path: string; dir: boolean; size: number; modified: string; hidden?: boolean };

	import { session } from '🍎/state/session.svelte.ts';

	const HOME = session.home;
	const places: { key: MsgKey; path: string; icon: 'apps' | 'home' | 'desk' | 'doc' | 'down' | 'pic' | 'music' | 'disk' }[] = [
		{ key: 'finder.applications', path: APPS_LOCATION, icon: 'apps' },
		{ key: 'finder.home', path: HOME, icon: 'home' },
		{ key: 'finder.desktop', path: `${HOME}/Desktop`, icon: 'desk' },
		{ key: 'finder.documents', path: `${HOME}/Documents`, icon: 'doc' },
		{ key: 'finder.downloads', path: `${HOME}/Downloads`, icon: 'down' },
		{ key: 'finder.pictures', path: `${HOME}/Pictures`, icon: 'pic' },
		{ key: 'finder.music', path: `${HOME}/Music`, icon: 'music' },
	];
	const devices: { key: MsgKey; path: string }[] = [
		{ key: 'finder.root', path: '/' },
		{ key: 'finder.usb', path: '/media' },
	];

	let path = $state(HOME);
	let entries = $state<Entry[]>([]);
	let loading = $state(false);
	let error = $state('');
	let selected = $state<string | null>(null);
	let renaming = $state<string | null>(null);
	let rename_value = $state('');
	let show_hidden = $state(false);
	let back = $state<string[]>([]);
	let fwd = $state<string[]>([]);
	let drag_over = $state(false);
	let upload_pct = $state<number | null>(null);
	let confirm_delete = $state<Entry | null>(null);
	let preview = $state<Entry | null>(null);
	let installing = $state<Entry | null>(null);
	let file_input = $state<HTMLInputElement>();
	let direction = $state<1 | -1>(1);

	const visible = $derived(show_hidden ? entries : entries.filter((e) => !e.hidden));
	const sel = $derived(entries.find((e) => e.path === selected) ?? null);
	const crumbs = $derived(
		path === '/' ? ['/'] : ['/', ...path.split('/').filter(Boolean)].map((_, i, a) => (i === 0 ? '/' : '/' + a.slice(1, i + 1).join('/'))),
	);
	const folder_name = $derived(
		path === APPS_LOCATION ? t('finder.applications') : path === TRASH_LOCATION ? t('trash.title') : path === '/' ? '/' : path.split('/').pop()!,
	);

	const is_virtual = (p: string) => p === APPS_LOCATION || p === TRASH_LOCATION;
	let trash_view = $state<TrashView>();
	let trash_count = $state(0);
	let search = $state('');

	async function load(p: string) {
		if (is_virtual(p)) {
			path = p;
			entries = [];
			selected = null;
			error = '';
			return;
		}
		loading = true;
		error = '';
		try {
			const res = await api<{ path: string; entries: Entry[] }>(`/api/fs/list?path=${encodeURIComponent(p)}`);
			path = res.path;
			entries = res.entries;
			selected = null;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	function go(p: string, push = true) {
		if (p === path) return;
		if (push) {
			back = [...back, path];
			fwd = [];
		}
		direction = p.length >= path.length ? 1 : -1;
		load(p);
	}

	function go_back() {
		const prev = back.at(-1);
		if (!prev) return;
		back = back.slice(0, -1);
		fwd = [path, ...fwd];
		direction = -1;
		load(prev);
	}

	function go_fwd() {
		const next = fwd[0];
		if (!next) return;
		fwd = fwd.slice(1);
		back = [...back, path];
		direction = 1;
		load(next);
	}

	function open(e: Entry) {
		if (e.dir) return go(e.path);
		if (e.name.toLowerCase().endsWith('.deb')) return (installing = e);
		preview = e;
	}

	function join(dir: string, name: string) {
		return dir === '/' ? `/${name}` : `${dir}/${name}`;
	}

	async function new_folder() {
		let name = t('finder.untitled');
		let i = 2;
		while (entries.some((e) => e.name === name)) name = `${t('finder.untitled')} ${i++}`;
		try {
			await post('/api/fs/op', { op: 'mkdir', path: join(path, name) });
			await load(path);
			selected = join(path, name);
			start_rename();
		} catch (e) {
			error = String((e as Error).message);
		}
	}

	async function start_rename() {
		if (!sel) return;
		renaming = sel.path;
		rename_value = sel.name;
		await tick();
		const input = document.querySelector<HTMLInputElement>('.finder .rename');
		input?.focus();
		const dot = sel.dir ? -1 : rename_value.lastIndexOf('.');
		input?.setSelectionRange(0, dot > 0 ? dot : rename_value.length);
	}

	async function commit_rename() {
		const target = entries.find((e) => e.path === renaming);
		renaming = null;
		const name = rename_value.trim();
		if (!target || !name || name === target.name || name.includes('/')) return;
		try {
			await post('/api/fs/op', { op: 'rename', path: target.path, to: join(path, name) });
			await load(path);
			selected = join(path, name);
		} catch (e) {
			error = String((e as Error).message);
		}
	}

	/** ⌘⌫ / toolbar / menu: move to Trash, like macOS. Falls back to asking for
	 *  a permanent delete when the item is on another filesystem (USB stick). */
	async function move_to_trash(target: Entry) {
		error = '';
		try {
			await post('/api/fs/op', { op: 'trash', path: target.path });
			await load(path);
			window.dispatchEvent(new Event('zerodesk-trash-changed'));
		} catch (e) {
			if (e instanceof ApiError && e.status === 409) confirm_delete = target;
			else error = String((e as Error).message);
		}
	}

	function row_menu(ev: MouseEvent, e: Entry) {
		selected = e.path;
		show_context_menu(ev, [
			{ label: t('apps.open'), action: () => open(e) },
			...(e.dir ? [] : [{ label: t('finder.quick_look'), action: () => (preview = e) }]),
			{ label: t('finder.rename'), action: () => start_rename(), break_after: true },
			...(e.dir ? [] : [{ label: t('finder.download'), action: () => download(e), break_after: true }]),
			{ label: t('apps.move_to_trash'), action: () => move_to_trash(e) },
		]);
	}

	function row_drag(ev: DragEvent, e: Entry) {
		ev.dataTransfer?.setData(FILE_MIME, e.path);
		ev.dataTransfer?.setData('text/plain', e.name);
		ev.dataTransfer!.effectAllowed = 'move';
	}

	// The Dock's Trash icon (or anything else) can steer Finder.
	let last_nav = finder_nav.seq;
	$effect(() => {
		if (finder_nav.seq !== last_nav) {
			last_nav = finder_nav.seq;
			if (finder_nav.target) go(finder_nav.target);
		}
	});

	function on_external_trash() {
		if (path === TRASH_LOCATION) trash_view?.load();
		else if (!is_virtual(path)) load(path);
	}

	async function do_delete() {
		const target = confirm_delete;
		confirm_delete = null;
		if (!target) return;
		try {
			await post('/api/fs/op', { op: 'delete', path: target.path });
			await load(path);
		} catch (e) {
			error = String((e as Error).message);
		}
	}

	function download(e: Entry) {
		const a = document.createElement('a');
		a.href = `/api/fs/get?download=1&path=${encodeURIComponent(e.path)}`;
		a.download = e.name;
		a.click();
	}

	/** Upload with progress (XHR, streamed to disk on the device). */
	function upload(files: File[]) {
		if (!files.length) return;
		const fd = new FormData();
		for (const f of files) fd.append('file', f, (f as File & { webkitRelativePath?: string }).webkitRelativePath || f.name);
		const xhr = new XMLHttpRequest();
		upload_pct = 0;
		xhr.upload.onprogress = (e) => e.lengthComputable && (upload_pct = Math.round((e.loaded / e.total) * 100));
		xhr.onloadend = () => {
			upload_pct = null;
			if (xhr.status !== 200) error = xhr.responseText || `HTTP ${xhr.status}`;
			load(path);
		};
		xhr.open('POST', `/api/fs/upload?dir=${encodeURIComponent(path)}`);
		xhr.send(fd);
	}

	function on_drop(e: DragEvent) {
		e.preventDefault();
		drag_over = false;
		upload([...(e.dataTransfer?.files ?? [])]);
	}

	function on_keydown(e: KeyboardEvent) {
		if (renaming || preview || installing || confirm_delete || is_virtual(path)) return;
		const list = visible;
		const idx = list.findIndex((x) => x.path === selected);
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			const n = Math.max(0, Math.min(list.length - 1, idx + (e.key === 'ArrowDown' ? 1 : -1)));
			selected = list[n]?.path ?? null;
			document.querySelector(`.finder [data-path="${CSS.escape(selected ?? '')}"]`)?.scrollIntoView({ block: 'nearest' });
		} else if (e.key === 'Enter' && sel) {
			e.preventDefault();
			start_rename();
		} else if (e.key === ' ' && sel && !sel.dir) {
			e.preventDefault();
			preview = sel;
		} else if ((e.key === 'Backspace' && e.metaKey) || e.key === 'Delete') {
			if (sel) move_to_trash(sel);
		} else if (e.key === 'ArrowUp' && e.metaKey) {
			go(crumbs.at(-2) ?? '/');
		} else if ((e.key === 'ArrowDown' && e.metaKey) || (e.key === 'o' && e.metaKey)) {
			if (sel) open(sel);
		}
	}

	onMount(() => {
		load(finder_nav.target || HOME);
		last_nav = finder_nav.seq;
		window.addEventListener('zerodesk-trash-changed', on_external_trash);
		return () => window.removeEventListener('zerodesk-trash-changed', on_external_trash);
	});
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="finder" tabindex="0" role="application" onkeydown={on_keydown}>
	<aside class="sidebar">
		<div class="sidebar-pad app-window-drag-handle"></div>
		<p class="section">{t('finder.favorites')}</p>
		{#each places as p}
			<button class="place" class:active={path === p.path} onclick={() => go(p.path)}>
				<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true">
					{#if p.icon === 'apps'}
						<path d="M10 2.8l2 4.6 4.8.4-3.7 3.2 1.2 4.8L10 13.2l-4.3 2.6 1.2-4.8-3.7-3.2 4.8-.4z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
					{:else if p.icon === 'home'}
						<path d="M3.5 9.2L10 3.8l6.5 5.4M5.5 8v7.7h9V8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
					{:else if p.icon === 'desk'}
						<rect x="2.8" y="4" width="14.4" height="9.6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><path d="M7 16.4h6M10 13.6v2.8" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
					{:else if p.icon === 'doc'}
						<path d="M5.6 2.8h5.8l3 3v10.4c0 .6-.4 1-1 1H5.6c-.6 0-1-.4-1-1V3.8c0-.6.4-1 1-1z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><path d="M7.4 10h5.2M7.4 13h3.6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
					{:else if p.icon === 'down'}
						<circle cx="10" cy="10" r="7" fill="none" stroke="currentColor" stroke-width="1.4" /><path d="M10 6.4v6.6M7.2 10.4L10 13.2l2.8-2.8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
					{:else if p.icon === 'pic'}
						<rect x="3" y="4" width="14" height="12" rx="2" fill="none" stroke="currentColor" stroke-width="1.4" /><path d="M3.5 14l4-4 3 3 2-2 4 4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><circle cx="13" cy="7.6" r="1.2" fill="currentColor" />
					{:else if p.icon === 'music'}
						<path d="M8 14.6V5.2l8-1.6v9.4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><circle cx="6.2" cy="14.6" r="1.9" fill="none" stroke="currentColor" stroke-width="1.4" /><circle cx="14.2" cy="13" r="1.9" fill="none" stroke="currentColor" stroke-width="1.4" />
					{/if}
				</svg>
				<span>{t(p.key)}</span>
			</button>
		{/each}
		<p class="section">{t('finder.device')}</p>
		{#each devices as d}
			<button class="place" class:active={path === d.path} onclick={() => go(d.path)}>
				<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true">
					<rect x="2.8" y="5.5" width="14.4" height="9" rx="2" fill="none" stroke="currentColor" stroke-width="1.4" />
					<circle cx="14" cy="10" r="1" fill="currentColor" />
				</svg>
				<span>{t(d.key)}</span>
			</button>
		{/each}
	</aside>

	<section
		class="main"
		class:drag-over={drag_over}
		role="region"
		ondragover={(e) => {
			if (is_virtual(path) || !e.dataTransfer?.types.includes('Files')) return;
			e.preventDefault();
			drag_over = true;
		}}
		ondragleave={(e) => !(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node) && (drag_over = false)}
		ondrop={on_drop}
	>
		<Toolbar
			inset={12}
			title={folder_name}
			subtitle={path === TRASH_LOCATION ? t('finder.items', { n: trash_count }) : path === APPS_LOCATION ? '' : t('finder.items', { n: visible.length })}
		>
			{#snippet leading()}
				<ToolButton label="←" onclick={go_back} disabled={!back.length}>
					<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="M12 4.5L6.5 10l5.5 5.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
				</ToolButton>
				<ToolButton label="→" onclick={go_fwd} disabled={!fwd.length}>
					<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="M8 4.5l5.5 5.5L8 15.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
				</ToolButton>
			{/snippet}
			{#snippet trailing()}
				{#if path === TRASH_LOCATION}
					<ToolButton label={t('trash.empty_action')} onclick={() => trash_view?.ask_empty()} disabled={!trash_count} text>
						<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="M4.5 6h11M8 6V4.5h4V6M6 6l.7 9.6c.1.8.7 1.4 1.5 1.4h3.6c.8 0 1.4-.6 1.5-1.4L14 6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /></svg>
					</ToolButton>
				{:else if path === APPS_LOCATION}
					<label class="search">
						<svg viewBox="0 0 20 20" width="13" height="13" aria-hidden="true"><circle cx="8.6" cy="8.6" r="5.2" fill="none" stroke="currentColor" stroke-width="1.6" /><path d="M12.6 12.6L16.5 16.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
						<input bind:value={search} placeholder={t('launchpad.search')} spellcheck="false" />
					</label>
				{:else}
				{#if sel && !sel.dir}
					{#if sel.name.toLowerCase().endsWith('.deb')}
						<ToolButton label={t('finder.install')} onclick={() => (installing = sel)} text>
							<svg viewBox="0 0 20 20" width="15" height="15" aria-hidden="true"><path d="M10 3.5v9M6.5 9.5L10 13l3.5-3.5M4 16h12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
						</ToolButton>
					{/if}
					<ToolButton label={t('finder.download')} onclick={() => sel && download(sel)}>
						<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="M10 3.5v9M6.5 9.5L10 13l3.5-3.5M4 16h12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
					</ToolButton>
				{/if}
				{#if sel}
					<ToolButton label={t('apps.move_to_trash')} onclick={() => sel && move_to_trash(sel)}>
						<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="M4.5 6h11M8 6V4.5h4V6M6 6l.7 9.6c.1.8.7 1.4 1.5 1.4h3.6c.8 0 1.4-.6 1.5-1.4L14 6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /></svg>
					</ToolButton>
				{/if}
				<ToolButton label={t('finder.new_folder')} onclick={new_folder}>
					<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true"><path d="M2.8 6c0-.8.6-1.4 1.4-1.4h3.4l1.5 1.5h6.7c.8 0 1.4.6 1.4 1.4v7.3c0 .8-.6 1.4-1.4 1.4H4.2c-.8 0-1.4-.6-1.4-1.4z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><path d="M10 9v4.4M7.8 11.2h4.4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
				</ToolButton>
				<ToolButton label={t('finder.upload')} onclick={() => file_input?.click()}>
					<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><path d="M10 13.5v-9M6.5 8L10 4.5 13.5 8M4 16h12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
				</ToolButton>
				<ToolButton label={t('finder.hidden')} pressed={show_hidden} onclick={() => (show_hidden = !show_hidden)}>
					<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true"><path d="M2.5 10s2.8-5 7.5-5 7.5 5 7.5 5-2.8 5-7.5 5-7.5-5-7.5-5z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><circle cx="10" cy="10" r="2.3" fill="none" stroke="currentColor" stroke-width="1.4" /></svg>
				</ToolButton>
				{/if}
			{/snippet}
		</Toolbar>

		<input type="file" multiple hidden bind:this={file_input} onchange={(e) => upload([...((e.currentTarget as HTMLInputElement).files ?? [])])} />

		{#if path === APPS_LOCATION}
			<AppsView query={search} />
		{:else if path === TRASH_LOCATION}
			<TrashView bind:this={trash_view} bind:count={trash_count} />
		{:else}
		<div class="header-row">
			<span class="c-name">{t('finder.name')}</span>
			<span class="c-date">{t('finder.modified')}</span>
			<span class="c-size">{t('finder.size')}</span>
		</div>

		{#key path}
			<div class="list" class:loading style:--dir={direction}>
				{#each visible as e (e.path)}
					<!-- svelte-ignore a11y_click_events_have_key_events -->
					<div
						class="row"
						class:selected={selected === e.path}
						data-path={e.path}
						role="row"
						tabindex="-1"
						draggable={renaming !== e.path}
						onclick={() => (selected = e.path)}
						ondblclick={() => open(e)}
						oncontextmenu={(ev) => row_menu(ev, e)}
						ondragstart={(ev) => row_drag(ev, e)}
					>
						<span class="c-name">
							<FileGlyph name={e.name} dir={e.dir} />
							{#if renaming === e.path}
								<input
									class="rename"
									bind:value={rename_value}
									onkeydown={(ev) => {
										ev.stopPropagation();
										if (ev.key === 'Enter') commit_rename();
										if (ev.key === 'Escape') renaming = null;
									}}
									onblur={commit_rename}
								/>
							{:else}
								<span class="name">{e.name}</span>
							{/if}
						</span>
						<span class="c-date">{format_date(e.modified)}</span>
						<span class="c-size">{e.dir ? '—' : format_bytes(e.size)}</span>
					</div>
				{:else}
					{#if !loading}
						<p class="empty">{error || t('finder.empty')}</p>
					{/if}
				{/each}
			</div>
		{/key}

		<footer class="pathbar">
			{#each crumbs as c, i}
				{#if i > 0}<span class="sep">›</span>{/if}
				<button onclick={() => go(c)}>{c === '/' ? '/' : c.split('/').pop()}</button>
			{/each}
		</footer>
		{/if}

		{#if drag_over}
			<div class="drop-hint"><span>{t('finder.drop')}</span></div>
		{/if}
		{#if upload_pct !== null}
			<div class="progress">
				<span>{t('finder.uploading', { p: upload_pct })}</span>
				<span class="bar"><span style:width="{upload_pct}%"></span></span>
			</div>
		{/if}
	</section>

	{#if confirm_delete}
		<div class="sheet-backdrop">
			<div class="sheet">
				<FileGlyph name={confirm_delete.name} dir={confirm_delete.dir} size={44} />
				<h3>{t('finder.confirm_delete', { name: confirm_delete.name })}</h3>
				<p>{t('finder.confirm_delete_hint')}</p>
				<div class="buttons">
					<button class="btn" onclick={() => (confirm_delete = null)}>{t('finder.cancel')}</button>
					<button class="btn destructive" onclick={do_delete}>{t('finder.delete')}</button>
				</div>
			</div>
		</div>
	{/if}

	{#if preview}
		<QuickLook entry={preview} on_close={() => (preview = null)} on_download={() => preview && download(preview)} />
	{/if}

	{#if installing}
		<InstallSheet entry={installing} on_close={() => (installing = null)} />
	{/if}
</div>

<style>
	.finder {
		position: relative;
		height: 100%;
		min-height: 0;
		display: grid;
		grid-template-columns: 188px 1fr;
		outline: none;
	}

	.search {
		display: flex;
		align-items: center;
		gap: 6px;
		width: 180px;
		height: 28px;
		padding: 0 10px;
		border-radius: 8px;
		background: var(--zd-hover);
		color: var(--zd-text-tertiary);

		input {
			flex: 1;
			min-width: 0;
			border: 0;
			outline: 0;
			background: none;
			font-size: 13px;
			color: var(--zd-text);
		}
	}

	.sidebar {
		display: flex;
		flex-direction: column;
		gap: 1px;
		padding: 0 10px 12px;
		background: var(--zd-sidebar-bg);
		backdrop-filter: blur(30px);
		box-shadow: inset -0.5px 0 0 var(--zd-hairline-strong);
		overflow-y: auto;
	}

	.sidebar-pad {
		height: var(--zd-titlebar);
		flex: none;
	}

	.section {
		margin: 12px 8px 4px;
		font-size: 11px;
		font-weight: 600;
		color: var(--zd-text-tertiary);
	}

	.place {
		justify-content: flex-start;
		gap: 8px;
		height: 28px;
		padding: 0 8px;
		border-radius: var(--zd-radius-control);
		font-size: 13px;
		color: var(--zd-text);
		transition: background-color 120ms ease;

		svg {
			color: var(--zd-text-secondary);
			flex: none;
		}

		span {
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}

		&:hover {
			background: var(--zd-hover);
		}

		&.active {
			background: var(--zd-selected);
		}
	}

	.main {
		position: relative;
		display: flex;
		flex-direction: column;
		min-width: 0;
		min-height: 0;
	}

	.header-row,
	.row {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 170px 84px;
		align-items: center;
		gap: 12px;
		padding: 0 16px;
	}

	.header-row {
		height: 28px;
		flex: none;
		font-size: 11px;
		font-weight: 500;
		color: var(--zd-text-secondary);
		box-shadow: 0 0.5px 0 var(--zd-hairline);
	}

	.list {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 4px 8px 8px;
		animation: list-in 300ms var(--zd-ease-out);
		transition: opacity 200ms ease;

		&.loading {
			opacity: 0.5;
		}
	}

	.row {
		height: 30px;
		padding: 0 8px;
		border-radius: var(--zd-radius-control);
		font-size: 13px;
		transition: background-color 80ms ease;

		&:nth-child(even) {
			background: hsla(0, 0%, 50%, 0.035);
		}

		&.selected {
			background: var(--system-color-primary);
			color: var(--system-color-primary-contrast);

			.c-date,
			.c-size {
				color: inherit;
				opacity: 0.75;
			}
		}
	}

	.c-name {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}

	.name {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.c-date,
	.c-size {
		color: var(--zd-text-secondary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.c-size {
		text-align: right;
		font-variant-numeric: tabular-nums;
	}

	.rename {
		flex: 1;
		min-width: 0;
		height: 22px;
		padding: 0 6px;
		border: 0;
		border-radius: 4px;
		background: var(--zd-window-bg);
		color: var(--zd-text);
		box-shadow: 0 0 0 2px hsla(var(--system-color-primary-hsl), 0.35);
	}

	.empty {
		padding: 60px 0;
		text-align: center;
		color: var(--zd-text-tertiary);
	}

	.pathbar {
		flex: none;
		display: flex;
		align-items: center;
		gap: 2px;
		height: 28px;
		padding: 0 12px;
		font-size: 11px;
		color: var(--zd-text-secondary);
		box-shadow: 0 -0.5px 0 var(--zd-hairline);
		overflow: hidden;
		white-space: nowrap;

		button {
			padding: 2px 5px;
			border-radius: 4px;

			&:hover {
				background: var(--zd-hover);
				color: var(--zd-text);
			}
		}

		.sep {
			color: var(--zd-text-tertiary);
		}
	}

	.drop-hint {
		position: absolute;
		inset: var(--zd-titlebar) 8px 8px;
		display: grid;
		place-items: center;
		border-radius: var(--zd-radius-panel);
		background: hsla(var(--system-color-primary-hsl), 0.05);
		box-shadow: inset 0 0 0 2px hsla(var(--system-color-primary-hsl), 0.3);
		pointer-events: none;
		animation: fade 160ms ease;

		span {
			padding: 6px 12px;
			border-radius: 8px;
			background: var(--zd-glass);
			backdrop-filter: blur(20px);
			font-weight: 500;
		}
	}

	.progress {
		position: absolute;
		right: 16px;
		bottom: 40px;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 12px;
		border-radius: var(--zd-radius-panel);
		background: var(--zd-glass);
		backdrop-filter: blur(30px);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 6px 20px hsla(0, 0%, 0%, 0.15);
		font-size: 12px;
		font-variant-numeric: tabular-nums;
		animation: rise 300ms var(--zd-spring);

		.bar {
			width: 120px;
			height: 4px;
			border-radius: 2px;
			background: var(--zd-hover);
			overflow: hidden;

			span {
				display: block;
				height: 100%;
				background: var(--system-color-primary);
				transition: width 200ms ease;
			}
		}
	}

	.sheet-backdrop {
		position: absolute;
		inset: 0;
		z-index: 30;
		display: flex;
		justify-content: center;
		background: hsla(0, 0%, 0%, 0.08);
		animation: fade 160ms ease;
	}

	.sheet {
		margin-top: var(--zd-titlebar);
		width: 280px;
		height: max-content;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		padding: 20px 20px 16px;
		border-radius: 0 0 var(--zd-radius-window) var(--zd-radius-window);
		background: var(--zd-window-bg);
		box-shadow: 0 12px 40px hsla(0, 0%, 0%, 0.25);
		text-align: center;
		animation: sheet-down 340ms var(--zd-spring);

		h3 {
			margin-top: 8px;
			font-size: 13px;
			font-weight: 600;
			word-break: break-all;
		}

		p {
			font-size: 12px;
			color: var(--zd-text-secondary);
		}

		.buttons {
			margin-top: 12px;
			width: 100%;
			display: grid;
			grid-template-columns: 1fr 1fr;
			gap: 8px;
		}
	}

	:global(.finder .btn),
	:global(.zd-btn) {
		height: 28px;
		border-radius: var(--zd-radius-control);
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);
		font-size: 13px;
		font-weight: 500;
		transition: transform 200ms var(--zd-spring);

		&:active {
			transform: scale(0.97);
		}
	}

	.btn.destructive {
		background: var(--zd-red);
		color: #fff;
	}

	@keyframes list-in {
		from {
			opacity: 0;
			transform: translateX(calc(var(--dir) * 12px));
		}
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
	}

	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
	}

	@keyframes sheet-down {
		from {
			transform: translateY(-100%);
		}
	}
</style>
