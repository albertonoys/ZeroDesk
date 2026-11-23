<script lang="ts">
	import { device_icons, icon_url } from '🍎/lib/icons.svelte.ts';
	import { interpolate } from 'popmotion';
	import { onMount, untrack } from 'svelte';
	import { spring } from 'svelte/motion';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import { elevation } from '🍎/actions';
	import { post } from '🍎/lib/api.ts';
	import { apps, native_of, open_app } from '🍎/state/apps.svelte.ts';
	import { FILE_MIME, finder_go, TRASH_LOCATION } from '🍎/state/finder_nav.svelte.ts';
	import AppIcon from '🍎/components/SVG/AppIcon.svelte';
	import DeviceIcon, { OFFICIAL_ART } from '🍎/components/SVG/DeviceIcon.svelte';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { show_context_menu } from '🍎/state/contextmenu.svelte.ts';
	import { DRAG_MIME } from '🍎/state/launchpad.svelte.ts';

	// Trash at the end of the Dock, like macOS. Click opens it in Finder;
	// files dropped on it are moved to the Trash. Apps dropped on it are not
	// deleted: ZeroDesk leaves that to the Cardputer Zero and says so.
	const { mouse_x, base = 50 }: { mouse_x: number | null; base?: number } = $props();

	let full = $state(false);
	let hot = $state(false);
	let confirm_empty = $state(false);
	let emptying = $state(false);
	let no_delete = $state<{ id: string; name: string } | null>(null);

	// right click: the Dock's Trash menu, as on a Mac
	function menu(e: MouseEvent) {
		show_context_menu(e, [
			{ label: t('trash.open'), action: open_trash, break_after: true },
			{ label: t('trash.empty_trash'), action: () => (confirm_empty = true), disabled: !full },
		]);
	}

	async function empty_trash() {
		emptying = true;
		try {
			await post('/api/trash/op', { op: 'empty' });
		} finally {
			emptying = false;
			confirm_empty = false;
			window.dispatchEvent(new Event('zerodesk-trash-changed'));
		}
	}

	// the same magnification as the app icons (DockItem.svelte)
	const LIMIT = $derived(base * 6);
	const width_at = $derived(
		interpolate(
			[-LIMIT, -LIMIT / 1.25, -LIMIT / 2, 0, LIMIT / 2, LIMIT / 1.25, LIMIT],
			[base, base * 1.1, base * 1.414, base * 2, base * 1.414, base * 1.1, base],
		),
	);
	const width_px = spring(untrack(() => base), { damping: 0.47, stiffness: 0.12 });
	let bin_el = $state<HTMLElement>();
	let raf = 0;

	$effect(() => {
		mouse_x;
		base;
		if (preferences.reduced_motion || apps.is_being_dragged) return;
		raf = requestAnimationFrame(() => {
			let d = LIMIT + 1;
			if (bin_el && mouse_x !== null) {
				const r = bin_el.getBoundingClientRect();
				d = mouse_x - (r.left + r.width / 2);
			}
			untrack(() => ($width_px = Math.abs(d) > LIMIT ? base : width_at(d)));
		});
		return () => cancelAnimationFrame(raf);
	});

	async function refresh() {
		try {
			const res = await fetch('/api/trash');
			if (res.ok) full = (await res.json()).length > 0;
		} catch {
			/* ignore */
		}
	}

	onMount(() => {
		refresh();
		// the Dock forwards a pinned app let go of on the Trash
		const on_app = (e: Event) => (no_delete = (e as CustomEvent<{ id: string; name: string }>).detail);
		window.addEventListener('zerodesk-trash-changed', refresh);
		window.addEventListener('zerodesk-app-on-trash', on_app);
		return () => {
			window.removeEventListener('zerodesk-trash-changed', refresh);
			window.removeEventListener('zerodesk-app-on-trash', on_app);
		};
	});

	function open_trash() {
		open_app('finder');
		finder_go(TRASH_LOCATION);
	}

	function accepts(e: DragEvent) {
		const types = e.dataTransfer?.types ?? [];
		return types.includes(FILE_MIME) || types.includes(DRAG_MIME);
	}

	async function on_drop(e: DragEvent) {
		hot = false;
		if (!accepts(e)) return;
		e.preventDefault();
		e.stopPropagation();
		const file = e.dataTransfer!.getData(FILE_MIME);
		if (file) {
			try {
				await post('/api/fs/op', { op: 'trash', path: file });
			} catch {
				/* Finder shows errors for its own deletes */
			}
			window.dispatchEvent(new Event('zerodesk-trash-changed'));
			return;
		}
		try {
			const app = JSON.parse(e.dataTransfer!.getData(DRAG_MIME));
			// tell the Dock this drop was handled, so a pinned app is not also unpinned
			window.dispatchEvent(new Event('zerodesk-dock-drop'));
			no_delete = { id: app.id, name: app.name };
		} catch {
			/* ignore */
		}
	}
</script>

<button
	class="trash"
	class:hot
	aria-label={t('trash.title')}
	onclick={open_trash}
	oncontextmenu={menu}
	ondragover={(e) => accepts(e) && (e.preventDefault(), e.stopPropagation(), (hot = true))}
	ondragleave={() => (hot = false)}
	ondrop={on_drop}
>
	<p class="tooltip" use:elevation={'dock-tooltip'}>{t('trash.title')}</p>
	<!-- empty and full states, generated art (design/icon-gen) -->
	<img
		bind:this={bin_el}
		class="bin"
		src={full ? icon_url('trash-full') : icon_url('trash-empty')}
		alt=""
		width={$width_px}
		height={$width_px}
		draggable="false"
	/>
</button>

{#if confirm_empty}
	<div class="alert-layer" role="presentation">
		<div class="alert" role="alertdialog" aria-label={t('trash.erase_title')}>
			<img src={icon_url('trash-full')} alt="" width="56" height="56" />
			<b>{t('trash.erase_title')}</b>
			<p>{t('trash.erase_body')}</p>
			<div class="alert-buttons">
				<button class="alert-btn" disabled={emptying} onclick={() => (confirm_empty = false)}>{t('admin.cancel')}</button>
				<button class="alert-btn primary" disabled={emptying} onclick={empty_trash}>{t('trash.empty_trash')}</button>
			</div>
		</div>
	</div>
{/if}
{#if no_delete}
	<div class="alert-layer" role="presentation">
		<div class="alert notice" role="alertdialog" aria-label={t('apps.no_delete_title', { name: no_delete.name })}>
			<!-- the app's own icon when it has one; no empty space when it has none -->
			{#if native_of[no_delete.id]}
				<AppIcon id={native_of[no_delete.id]} size={56} />
			{:else if OFFICIAL_ART.has(no_delete.id) || device_icons[no_delete.id]?.src}
				<DeviceIcon id={no_delete.id} size={56} />
			{/if}
			<b>{t('apps.no_delete_title', { name: no_delete.name })}</b>
			<p>{t('apps.no_delete_body')}</p>
			<div class="alert-buttons single">
				<button class="alert-btn primary" onclick={() => (no_delete = null)}>{t('installer.ok')}</button>
			</div>
		</div>
	</div>
{/if}
<svelte:window
	onkeydown={(e) => {
		if (no_delete && (e.key === 'Escape' || e.key === 'Enter')) no_delete = null;
		else if (confirm_empty && !emptying) e.key === 'Escape' ? (confirm_empty = false) : e.key === 'Enter' && empty_trash();
	}}
/>

<style>
	.trash {
		position: relative;
		display: flex;
		align-items: flex-end;
		justify-content: center;
		/* the bin art has 5.3px of transparency under it; this puts its base
		   level with the bottom of the app icon tiles */
		padding-bottom: 3.7px;
		transition: transform 300ms var(--zd-spring);

		&.hot {
			transform: scale(1.18) translateY(-4px);
		}

		&:hover .tooltip {
			opacity: 1;
			visibility: visible;
		}
	}

	.bin {
		display: block;
		filter: drop-shadow(0 1.5px 2px hsla(0, 0%, 0%, 0.25));
		user-select: none;
	}

	/* the bin is frosted glass: in dark mode it picks up the dark Dock */
	:global(body.dark) .bin {
		filter: brightness(0.62) contrast(1.12) drop-shadow(0 1.5px 2px hsla(0, 0%, 0%, 0.4));
	}

	.alert-layer {
		position: fixed;
		inset: 0;
		z-index: 9500;
		display: grid;
		place-items: center;
		padding: 16px;
		background: hsla(0, 0%, 0%, 0.12);
	}

	.alert {
		width: min(260px, 100%);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 22px 18px 18px;
		border-radius: 22px;
		background: var(--zd-window-bg);
		box-shadow:
			0 0 0 0.5px var(--zd-hairline-strong),
			0 20px 60px hsla(0, 0%, 0%, 0.3);
		text-align: center;

		b {
			margin-top: 4px;
			font-size: 13px;
			font-weight: 700;
			line-height: 1.35;
		}

		p {
			margin: 0;
			font-size: 11px;
			color: var(--zd-text-secondary);
		}

		/* a longer explanation than the Trash's own alert: wider, with room between lines */
		&.notice {
			width: min(300px, 100%);
			gap: 10px;
			padding: 24px 22px 18px;

			p {
				font-size: 12px;
				line-height: 1.55;
			}
		}
	}

	.alert-buttons {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
		width: 100%;
		margin-top: 10px;

		&.single {
			grid-template-columns: 1fr;
		}
	}

	.alert-btn {
		height: 28px;
		border-radius: 14px;
		font-size: 13px;
		font-weight: 500;
		color: var(--zd-text);
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);
		white-space: nowrap;

		&.primary {
			color: var(--system-color-primary-contrast);
			background: var(--system-color-primary);
		}
	}

	.tooltip {
		position: absolute;
		left: 50%;
		bottom: calc(100% + 10px);
		transform: translateX(-50%);
		white-space: nowrap;
		padding: 4px 10px;
		border-radius: 7px;
		background: var(--zd-glass);
		backdrop-filter: blur(30px) saturate(1.8);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 0 0 0.5px hsla(0, 0%, 0%, 0.12),
			0 4px 14px hsla(0, 0%, 0%, 0.18);
		color: var(--zd-text);
		font-size: 13px;
		opacity: 0;
		visibility: hidden;
		transition: opacity 120ms ease;
		pointer-events: none;
	}
</style>
