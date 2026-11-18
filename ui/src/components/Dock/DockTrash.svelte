<script lang="ts">
	import { icon_url } from '🍎/lib/icons.svelte.ts';
	import { onMount } from 'svelte';
	import { elevation } from '🍎/actions';
	import { post } from '🍎/lib/api.ts';
	import { open_app } from '🍎/state/apps.svelte.ts';
	import { APPS_LOCATION, FILE_MIME, finder_go, TRASH_LOCATION } from '🍎/state/finder_nav.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { DRAG_MIME } from '🍎/state/launchpad.svelte.ts';

	// Trash at the end of the Dock, like macOS. Click opens it in Finder;
	// files dropped on it are moved to the Trash, apps dropped on it are
	// offered for uninstall.
	let full = $state(false);
	let hot = $state(false);

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
	<img class="bin" src={full ? icon_url('trash-full') : icon_url('trash-empty')} alt="" width="50" height="50" draggable="false" />
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
