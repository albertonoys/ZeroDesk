<script lang="ts">
	import { onMount } from 'svelte';
	import { learn_device_icons } from '🍎/lib/icons.svelte.ts';
	import { cached, revalidate } from '🍎/lib/swr.svelte.ts';
	import { data, type DeviceApp } from '🍎/state/app_preload.ts';
	import { load_web_apps, native_of, open_app, open_device_app, title_of, web_apps } from '🍎/state/apps.svelte.ts';
	import { format_bytes, t } from '🍎/state/i18n.svelte.ts';
	import { phone } from '🍎/state/phone.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';
	import { post } from '🍎/lib/api.ts';
	import { device_icons } from '🍎/lib/icons.svelte.ts';
	import { as_admin, Cancelled } from '🍎/state/admin.svelte.ts';
	import { store_state } from '🍎/state/appstore.svelte.ts';
	import AppIcon from '../SVG/AppIcon.svelte';
	import DeviceIcon from '../SVG/DeviceIcon.svelte';

	// The iOS home screen: pages of app icons (the first one starts with a
	// widget for the Zero itself), page dots, and the Dock.

	type Item = { key: string; name: string; kind: 'builtin' | 'device' | 'web' | 'installing'; id: string; icon?: string; pct?: number };

	const DOCK = ['device', 'finder', 'appstore', 'settings'];
	const SYSTEM = ['terminal', 'desktop', 'about'];
	const WIDGET_SLOTS = 8; // the widget is about as tall as two rows of icons

	let device_list = $state<DeviceApp[]>(cached<DeviceApp[]>('apps') ?? []);
	let pages_el = $state<HTMLElement>();
	let page = $state(0);

	onMount(() => {
		Promise.all([revalidate('apps', data.apps), load_web_apps(true)])
			.then(([a]) => {
				learn_device_icons(a);
				device_list = a;
			})
			.catch(() => {});
		const refresh = () => revalidate('apps', data.apps).then((a) => (device_list = a)).catch(() => {});
		window.addEventListener('zerodesk-apps-changed', refresh);
		return () => window.removeEventListener('zerodesk-apps-changed', refresh);
	});

	const items = $derived.by<Item[]>(() => {
		const system: Item[] = SYSTEM.map((id) => ({ key: id, id, name: title_of(id), kind: 'builtin' }));
		const devices: Item[] = device_list
			.filter((a) => !a.terminal)
			.map((a) => ({ key: a.id, id: a.id, name: a.name, kind: 'device' as const }));
		const web_only: Item[] = Object.values(web_apps)
			.filter((w) => !w.device_app)
			.map((w) => ({ key: `web:${w.id}`, id: `web:${w.id}`, name: w.name, kind: 'web' as const }));
		// apps being installed from the App Store, as on iOS: a grey icon whose ring fills
		const installing: Item[] = Object.entries(store_state.tasks)
			.filter(([, task]) => task.phase !== 'auth')
			.map(([id, task]) => {
				const a = store_state.apps.find((x) => x.id === id);
				return { key: `inst:${id}`, id, name: a?.title ?? '', kind: 'installing' as const, icon: a?.icon, pct: task.pct };
			})
			.filter((it) => !device_list.some((d) => d.name === it.name));
		return [...system, ...[...devices, ...web_only].sort((a, b) => a.name.localeCompare(b.name)), ...installing];
	});

	// ---- long press: Remove App (iOS) ----
	let menu = $state<{ it: Item; x: number; y: number } | null>(null);
	let confirm_remove = $state<Item | null>(null);
	let remove_error = $state('');
	let press: { timer: ReturnType<typeof setTimeout>; x: number; y: number; fired: boolean } | null = null;

	function press_start(e: PointerEvent, it: Item) {
		if (it.kind !== 'device') return;
		const el = e.currentTarget as HTMLElement;
		press = {
			x: e.clientX,
			y: e.clientY,
			fired: false,
			timer: setTimeout(() => {
				if (!press) return;
				press.fired = true;
				const r = el.getBoundingClientRect();
				menu = { it, x: r.left + r.width / 2, y: r.top };
				navigator.vibrate?.(10);
			}, 520),
		};
	}
	function press_move(e: PointerEvent) {
		if (press && Math.hypot(e.clientX - press.x, e.clientY - press.y) > 8) press_cancel();
	}
	function press_cancel() {
		if (press) clearTimeout(press.timer);
		press = null;
	}
	function tap(it: Item) {
		const long = press?.fired;
		press_cancel();
		if (!long && it.kind !== 'installing') launch(it);
	}

	async function remove(it: Item) {
		confirm_remove = null;
		remove_error = '';
		try {
			await as_admin((password) => post('/api/apps/uninstall', { id: it.id, password }), {
				title: t('admin.remove_title', { name: it.name }),
				action: t('admin.remove_ok'),
				app: { name: it.name, icon: device_icons[it.id]?.src },
			});
			device_list = device_list.filter((a) => a.id !== it.id);
			revalidate('apps', data.apps).then((a) => (device_list = a)).catch(() => {});
		} catch (e) {
			if (!(e instanceof Cancelled)) remove_error = (e as Error).message;
		}
	}

	// as many rows as fit the screen (a 96 px row + 6 px gap); the widget
	// takes about two rows on the first page
	let pages_h = $state(640);
	const per_page = $derived(Math.max(3, Math.floor((pages_h - 8) / 102)) * 4);
	const pages = $derived.by<Item[][]>(() => {
		const first = Math.max(4, per_page - WIDGET_SLOTS);
		const out: Item[][] = [items.slice(0, first)];
		for (let i = first; i < items.length; i += per_page) out.push(items.slice(i, i + per_page));
		return out;
	});

	function launch(it: Item) {
		if (it.kind === 'builtin') open_app(it.id);
		else open_device_app({ id: it.id, name: it.name });
		phone.home = false;
	}

	function on_scroll() {
		if (pages_el) page = Math.round(pages_el.scrollLeft / pages_el.clientWidth);
	}

	const battery = $derived(session.sys?.battery?.capacity ? `${session.sys.battery.capacity}%` : '—');
	const address = $derived(session.sys?.addrs?.find((a) => !a.iface.startsWith('tailscale'))?.addr ?? '—');
	const free = $derived(session.sys ? format_bytes(session.sys.diskFree) : '—');
</script>

{#snippet icon(it: Item, size: number)}
	{#if it.kind === 'installing'}
		<span class="inst" style:--p={it.pct ?? 0}>
			{#if it.icon}<img src={it.icon} alt="" width={size} height={size} />{/if}
			<svg viewBox="0 0 40 40" width="30" height="30" aria-hidden="true"><circle cx="20" cy="20" r="16" class="pie" /></svg>
		</span>
	{:else if it.kind === 'builtin'}
		<AppIcon id={it.id} {size} />
	{:else if it.kind === 'web'}
		<AppIcon id={it.id} {size} />
	{:else if native_of[it.id]}
		<AppIcon id={native_of[it.id]} {size} />
	{:else}
		<DeviceIcon id={it.id} {size} />
	{/if}
{/snippet}

{#snippet tile(it: Item)}
	<button
		class="tile"
		class:lifted={menu?.it.key === it.key}
		onpointerdown={(e) => press_start(e, it)}
		onpointermove={press_move}
		onpointercancel={press_cancel}
		onclick={() => tap(it)}
		oncontextmenu={(e) => e.preventDefault()}
	>
		<span class="icon">{@render icon(it, 62)}</span>
		<span class="label">{it.kind === 'installing' ? (it.pct ? t('phone.installing') : t('phone.waiting')) : it.name}</span>
	</button>
{/snippet}

<div class="home">
	<div class="pages" bind:this={pages_el} bind:clientHeight={pages_h} onscroll={on_scroll}>
		{#each pages as list, i (i)}
			<div class="page">
				{#if i === 0}
					<button class="widget" onclick={() => (open_app('device'), (phone.home = false))}>
						<span class="w-head">
							<AppIcon id="device" size={34} />
							<span class="w-name">
								<b>{session.sys?.hostname ?? session.hostname}</b>
								<small>Cardputer Zero</small>
							</span>
						</span>
						<span class="w-grid">
							<span><small>{t('phone.battery')}</small><b>{battery}</b></span>
							<span><small>{t('phone.address')}</small><b>{address}</b></span>
							<span><small>{t('phone.free')}</small><b>{free}</b></span>
							<span><small>{t('phone.temp')}</small><b>{session.sys ? `${Math.round(session.sys.tempC)}°` : '—'}</b></span>
						</span>
					</button>
				{/if}
				<div class="grid">{#each list as it (it.key)}{@render tile(it)}{/each}</div>
			</div>
		{/each}
	</div>

	{#if pages.length > 1}
		<div class="dots" aria-hidden="true">
			{#each pages as _, i (i)}<span class:on={i === page}></span>{/each}
		</div>
	{/if}

	{#if menu}
		<!-- iOS context menu: everything else blurs, the menu sits under the icon -->
		<div class="menu-layer" role="presentation" onclick={() => (menu = null)}>
			<div class="menu" style:left="{Math.min(Math.max(menu.x - 120, 12), innerWidth - 252)}px" style:top="{menu.y + 100}px">
				<button class="danger" onclick={(e) => (e.stopPropagation(), (confirm_remove = menu!.it), (menu = null))}>
					<span>{t('phone.remove_app')}</span>
					<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true"><path d="M4 6h12M8 6V4h4v2m-6 0 .7 10h6.6L14 6" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
				</button>
			</div>
		</div>
	{/if}

	{#if confirm_remove || remove_error}
		<div class="alert-layer" role="presentation">
			<div class="alert" role="alertdialog">
				<b>{remove_error ? t('installer.error_title') : t('phone.delete_title', { name: confirm_remove!.name })}</b>
				<p>{remove_error || t('phone.delete_body')}</p>
				<div class="alert-buttons">
					{#if remove_error}
						<button onclick={() => (remove_error = '')}>{t('installer.ok')}</button>
					{:else}
						<button class="danger" onclick={() => remove(confirm_remove!)}>{t('phone.delete')}</button>
						<button onclick={() => (confirm_remove = null)}>{t('admin.cancel')}</button>
					{/if}
				</div>
			</div>
		</div>
	{/if}

	<nav class="dock">
		{#each DOCK as id (id)}
			<button class="dock-item" aria-label={title_of(id)} onclick={() => (open_app(id), (phone.home = false))}>
				<AppIcon {id} size={62} />
			</button>
		{/each}
	</nav>
</div>

<style>
	.home {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		padding-top: calc(env(safe-area-inset-top) + 18px);
		padding-bottom: calc(env(safe-area-inset-bottom) + 10px);
	}

	.pages {
		flex: 1;
		min-height: 0;
		display: flex;
		overflow-x: auto;
		overflow-y: hidden;
		scroll-snap-type: x mandatory;
		scrollbar-width: none;
		overscroll-behavior-x: contain;

		&::-webkit-scrollbar {
			display: none;
		}
	}

	.page {
		flex: none;
		width: 100%;
		scroll-snap-align: start;
		padding: 4px 18px 0;
	}

	.grid {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		grid-auto-rows: 96px;
		row-gap: 6px;
	}

	.tile {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		min-width: 0;
		padding-top: 4px;
		transition: transform 160ms var(--zd-spring);

		&:active {
			transform: scale(0.92);
		}
	}

	.icon {
		width: 62px;
		height: 62px;
		filter: drop-shadow(0 2px 6px hsla(0, 0%, 0%, 0.18));
	}

	.tile.lifted {
		position: relative;
		z-index: 31;
		transform: scale(1.08);
	}

	/* installing: the App Store icon greyed, with a pie that fills */
	.inst {
		position: relative;
		display: grid;
		place-items: center;
		width: 62px;
		height: 62px;
		border-radius: 14px;
		overflow: hidden;
		background: var(--zd-hover);

		img {
			position: absolute;
			inset: 0;
			width: 100%;
			height: 100%;
			filter: grayscale(1) brightness(0.55);
		}

		svg {
			position: relative;
		}

		.pie {
			fill: none;
			stroke: hsla(0, 0%, 100%, 0.92);
			stroke-width: 32;
			stroke-dasharray: 100.5;
			stroke-dashoffset: calc(100.5 * (1 - var(--p)));
			transform: rotate(-90deg) scale(0.5);
			transform-origin: center;
			transition: stroke-dashoffset 250ms linear;
		}
	}

	.menu-layer {
		position: fixed;
		inset: 0;
		z-index: 30;
		background: hsla(0, 0%, 0%, 0.18);
		backdrop-filter: blur(14px);
		animation: fade 160ms ease;
	}

	.menu {
		position: absolute;
		width: 240px;
		border-radius: 14px;
		overflow: hidden;
		background: var(--zd-glass);
		backdrop-filter: blur(30px) saturate(1.6);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 12px 40px hsla(0, 0%, 0%, 0.25);
		animation: pop 220ms var(--zd-spring);

		button {
			display: flex;
			align-items: center;
			justify-content: space-between;
			width: 100%;
			height: 46px;
			padding: 0 16px;
			font-size: 16px;
		}
	}

	.danger {
		color: var(--zd-red);
	}

	.alert-layer {
		position: fixed;
		inset: 0;
		z-index: 40;
		display: grid;
		place-items: center;
		padding: 24px;
		background: hsla(0, 0%, 0%, 0.25);
		animation: fade 160ms ease;
	}

	.alert {
		width: min(280px, 100%);
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 20px 16px 12px;
		border-radius: 24px;
		text-align: center;
		color: var(--zd-text);
		background: var(--zd-window-bg);
		box-shadow: 0 16px 50px hsla(0, 0%, 0%, 0.3);
		animation: pop 220ms var(--zd-spring);

		b {
			font-size: 17px;
			font-weight: 600;
		}

		p {
			margin: 0 0 10px;
			font-size: 13px;
			color: var(--zd-text-secondary);
		}
	}

	.alert-buttons {
		display: flex;
		flex-direction: column;
		gap: 8px;

		button {
			height: 44px;
			border-radius: 22px;
			font-size: 16px;
			font-weight: 600;
			background: var(--zd-hover);
		}
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
	}

	@keyframes pop {
		from {
			opacity: 0;
			transform: scale(0.94);
		}
	}

	.label {
		max-width: 100%;
		font-size: 11.5px;
		font-weight: 500;
		color: #fff;
		text-shadow: 0 1px 3px hsla(0, 0%, 0%, 0.45);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	/* a medium widget: two rows of four icons */
	.widget {
		width: calc(100% - 12px);
		display: flex;
		flex-direction: column;
		gap: 18px;
		margin: 6px 6px 18px;
		padding: 16px 18px;
		border-radius: 26px;
		text-align: left;
		color: var(--zd-text);
		background: var(--zd-glass);
		backdrop-filter: blur(30px) saturate(1.6);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 8px 30px hsla(0, 0%, 0%, 0.18);
	}

	.w-head {
		display: flex;
		align-items: center;
		gap: 10px;
	}

	.w-name {
		display: flex;
		flex-direction: column;
		min-width: 0;

		b {
			font-size: 15px;
			font-weight: 700;
		}

		small {
			font-size: 11px;
			color: var(--zd-text-secondary);
		}
	}

	.w-grid {
		display: grid;
		/* the address needs room for a full IPv4 address */
		grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.6fr) minmax(0, 1fr) minmax(0, 0.8fr);
		gap: 8px;

		span {
			display: flex;
			flex-direction: column;
			gap: 2px;
			min-width: 0;
		}

		small {
			font-size: 10px;
			color: var(--zd-text-secondary);
		}

		b {
			font-size: 13px;
			font-weight: 600;
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}
	}

	.dots {
		display: flex;
		justify-content: center;
		gap: 8px;
		padding: 8px 0 12px;

		span {
			width: 7px;
			height: 7px;
			border-radius: 50%;
			background: hsla(0, 0%, 100%, 0.4);
			transition: background 200ms ease;

			&.on {
				background: #fff;
			}
		}
	}

	.dock {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		justify-items: center;
		margin: 0 12px;
		padding: 14px 8px;
		border-radius: 34px;
		background: var(--zd-glass);
		backdrop-filter: blur(30px) saturate(1.6);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 10px 30px hsla(0, 0%, 0%, 0.18);
	}

	.dock-item {
		transition: transform 160ms var(--zd-spring);

		&:active {
			transform: scale(0.92);
		}
	}
</style>
