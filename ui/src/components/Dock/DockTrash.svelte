<script lang="ts">
	import { icon_url } from '🍎/lib/icons.svelte.ts';
	import { interpolate } from 'popmotion';
	import { onMount, untrack } from 'svelte';
	import { spring } from 'svelte/motion';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import { elevation } from '🍎/actions';
	import { post } from '🍎/lib/api.ts';
	import { apps, open_app } from '🍎/state/apps.svelte.ts';
	import { APPS_LOCATION, FILE_MIME, finder_go, TRASH_LOCATION } from '🍎/state/finder_nav.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { DRAG_MIME } from '🍎/state/launchpad.svelte.ts';

	// Trash at the end of the Dock, like macOS. Click opens it in Finder;
	// files dropped on it are moved to the Trash, apps dropped on it are
	// offered for uninstall.
	const { mouse_x }: { mouse_x: number | null } = $props();

	let full = $state(false);
	let hot = $state(false);

	// the same magnification as the app icons (DockItem.svelte)
	const BASE = 50;
	const LIMIT = BASE * 6;
	const width_at = interpolate(
		[-LIMIT, -LIMIT / 1.25, -LIMIT / 2, 0, LIMIT / 2, LIMIT / 1.25, LIMIT],
		[BASE, BASE * 1.1, BASE * 1.414, BASE * 2, BASE * 1.414, BASE * 1.1, BASE],
	);
	const width_px = spring(BASE, { damping: 0.47, stiffness: 0.12 });
	let bin_el = $state<HTMLElement>();
	let raf = 0;

	$effect(() => {
		mouse_x;
		if (preferences.reduced_motion || apps.is_being_dragged) return;
		raf = requestAnimationFrame(() => {
			let d = LIMIT + 1;
			if (bin_el && mouse_x !== null) {
				const r = bin_el.getBoundingClientRect();
				d = mouse_x - (r.left + r.width / 2);
			}
			untrack(() => ($width_px = width_at(d)));
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
		window.addEventListener('zerodesk-trash-changed', refresh);
		return () => window.removeEventListener('zerodesk-trash-changed', refresh);
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
			open_app('finder');
			finder_go(APPS_LOCATION);
			setTimeout(() => window.dispatchEvent(new CustomEvent('zerodesk-uninstall', { detail: app.id })), 450);
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
