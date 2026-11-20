<svelte:options runes={true} />

<script lang="ts">
	import { flip } from 'svelte/animate';
	import { elevation } from '🍎/actions';
	import { apps_config } from '🍎/configs/apps/apps-config';
	import { apps, device_windows, web_apps, web_windows } from '🍎/state/apps.svelte';
	import { DRAG_MIME, pin, pinned } from '🍎/state/launchpad.svelte.ts';
	import { DOCK_MIME, dock_order, dock_room, is_dock_hidden } from '🍎/state/dock.svelte';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import DockItem from './DockItem.svelte';
	import DockTrash from './DockTrash.svelte';
	import { untrack } from 'svelte';

	let dock_mouse_x = $state<number | null>(null);

	const HIDDEN_DOCK_THRESHOLD = 30;
	let bodyHeight = $state(0);
	let bodyWidth = $state(innerWidth);
	let mouseY = $state(0);

	let dockContainerEl = $state<HTMLElement>();
	let dockEl = $state<HTMLElement>();

	// ---- what the Dock keeps, in the user's order ----

	const builtin = Object.entries(apps_config)
		.filter(([, c]) => !c.dock_when_running)
		.map(([id]) => id);

	// the saved order, then anything new (a built-in app added in an update, a pin from elsewhere)
	const kept = $derived.by(() => {
		const valid = new Set([...builtin, ...pinned.apps.map((a) => `dev:${a.id}`)]);
		const order = dock_order.ids.filter((k) => valid.has(k));
		return [...order, ...[...valid].filter((k) => !order.includes(k))];
	});

	// keep the saved order complete, so a pin can be placed by index
	$effect(() => {
		const k = kept;
		untrack(() => {
			if (k.join() !== dock_order.ids.join()) dock_order.ids = k;
		});
	});

	const kept_device = $derived(new Set(pinned.apps.map((a) => a.id)));

	// running apps that are not kept: device windows, native rewrites and web-app windows
	const running = $derived([
		...Object.entries(apps_config)
			.filter(([id, c]) => c.dock_when_running && apps.open[id] && !(c.native_for && kept_device.has(c.native_for)))
			.map(([id]) => id),
		...Object.keys(web_windows).filter((w) => {
			const dev = web_apps[w.slice(4)]?.device_app;
			return !(dev && kept_device.has(dev));
		}),
		...Object.keys(device_windows).filter((w) => !kept_device.has(w.slice(4))),
	]);

	// ---- reordering by drag ----

	const NEW = '__new__'; // the gap opened for an app dragged in from Launchpad or Finder
	let dragging = $state<string | null>(null);
	let preview = $state<string[] | null>(null);
	const shown = $derived(preview ?? kept);

	const removable = (k: string) => k.startsWith('dev:');

	function insert(list: string[], at: number, key: string) {
		const i = Math.max(0, Math.min(list.length, at));
		return [...list.slice(0, i), key, ...list.slice(i)];
	}

	// how many kept icons (other than the dragged one) sit left of the pointer
	function index_at(x: number) {
		const slots = [...(dockEl?.querySelectorAll<HTMLElement>('[data-dock-key]') ?? [])].filter(
			(el) => el.dataset.dockKey !== dragging && el.dataset.dockKey !== NEW,
		);
		return slots.filter((el) => {
			const r = el.getBoundingClientRect();
			return r.left + r.width / 2 < x;
		}).length;
	}

	function on_drag_start(e: DragEvent) {
		const key = (e.target as HTMLElement).closest<HTMLElement>('[data-dock-key]')?.dataset.dockKey;
		if (key) dragging = key;
	}

	function on_drag_over(e: DragEvent) {
		const types = e.dataTransfer?.types ?? [];
		if (!types.includes(DOCK_MIME) && !types.includes(DRAG_MIME)) return;
		e.preventDefault();
		const others = kept.filter((k) => k !== dragging);
		const at = index_at(e.clientX);
		if (dragging) {
			e.dataTransfer!.dropEffect = 'move';
			preview = insert(others, at, dragging);
		} else if (full) {
			e.dataTransfer!.dropEffect = 'none';
			preview = null;
		} else {
			e.dataTransfer!.dropEffect = 'copy';
			preview = insert(others, at, NEW);
		}
	}

	function on_drag_leave(e: DragEvent) {
		if (dockEl?.contains(e.relatedTarget as Node)) return;
		// an app that can leave the Dock shows its gap closing; a built-in one stays put
		preview = dragging && removable(dragging) ? kept.filter((k) => k !== dragging) : null;
	}

	function on_drop(e: DragEvent) {
		const types = e.dataTransfer?.types ?? [];
		if (!types.includes(DOCK_MIME) && !types.includes(DRAG_MIME)) return;
		e.preventDefault();
		window.dispatchEvent(new Event('zerodesk-dock-drop'));
		const order = preview;
		preview = null;
		if (dragging) {
			if (order?.includes(dragging)) dock_order.ids = order;
			return;
		}
		try {
			const app = JSON.parse(e.dataTransfer!.getData(DRAG_MIME));
			const at = order ? order.indexOf(NEW) : -1;
			pin({ id: app.id, name: app.name }, at < 0 ? undefined : at);
		} catch {
			/* ignore malformed drops */
		}
	}

	function on_drag_end() {
		dragging = null;
		preview = null;
	}

	// ---- fit the screen: icons shrink as the Dock fills up, and stop at a minimum ----

	const MAX = 50;
	const MIN = 30;
	const GAP = 4;
	const DIVIDER = 13; // 1px line + 12px margin
	const CHROME = 12 + 24; // the Dock's own padding, and some screen edge left over

	const dividers = $derived(running.length ? 2 : 1);
	const count = $derived(shown.length + running.length + 1); // + Trash
	const size_for = (n: number, d: number) => (bodyWidth - CHROME - d * DIVIDER - GAP * (n - 1)) / n;
	const base = $derived(Math.max(MIN, Math.min(MAX, Math.floor(size_for(count, dividers)))));

	// kept apps that fit at the smallest size, next to the running apps and the Trash
	$effect(() => {
		const others = running.length + 1;
		const fit = Math.floor((bodyWidth - CHROME - dividers * DIVIDER + GAP) / (MIN + GAP));
		untrack(() => (dock_room.capacity = Math.max(1, fit - others)));
	});
	const full = $derived(kept.length >= dock_room.capacity);

	let shake = $state(false);
	let full_note = $state(false);
	let note_timer = 0;
	$effect(() => {
		const on_full = () => {
			shake = false;
			requestAnimationFrame(() => (shake = true));
			full_note = true;
			clearTimeout(note_timer);
			note_timer = window.setTimeout(() => (full_note = false), 1800);
		};
		window.addEventListener('zerodesk-dock-full', on_full);
		return () => window.removeEventListener('zerodesk-dock-full', on_full);
	});

	$effect(() => {
		// Due to how pointer-events: none works, if dock auto opens, you move away, it won't close automatically.
		// So close it manually if mouse pointer goes out of the dock area.
		if (Math.abs(mouseY - bodyHeight) > dockContainerEl?.clientHeight) {
			untrack(() => (dock_mouse_x = null));
		}

		/**
		 * if mouseX != null then show the dock. No matter what
		 * When it becomes null, Then use the mouseY and bodyHeight to determine if the dock should be hidden
		 */
		if (dock_mouse_x !== null) {
			untrack(() => (is_dock_hidden.value = false));
			return;
		}

		if (!Object.values(apps.fullscreen).some(Boolean)) {
			untrack(() => (is_dock_hidden.value = false));
			return;
		}

		untrack(() => (is_dock_hidden.value = Math.abs(mouseY - bodyHeight) > HIDDEN_DOCK_THRESHOLD));
	});
</script>

<svelte:body onmousemove={({ y }) => (mouseY = y)} />


<svelte:window bind:innerHeight={bodyHeight} bind:innerWidth={bodyWidth} />

<section
	class="dock-container"
	class:dock-hidden={is_dock_hidden.value}
	style:height="{base + 30}px"
	bind:this={dockContainerEl}
	use:elevation={'dock'}
>
	{#if full_note}
		<p class="full-note" role="status">{t('dock.full')}</p>
	{/if}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="dock-el"
		class:hidden={is_dock_hidden.value}
		class:drop-target={!!preview && !dragging}
		class:shake
		bind:this={dockEl}
		role="toolbar"
		tabindex="-1"
		ondragstart={on_drag_start}
		ondragover={on_drag_over}
		ondragleave={on_drag_leave}
		ondrop={on_drop}
		ondragend={on_drag_end}
		onanimationend={() => (shake = false)}
		onmousemove={(event) => (dock_mouse_x = event.x)}
		onmouseleave={() => (dock_mouse_x = null)}
	>
		{#each shown as key (key)}
			<div class="slot" class:lifted={key === dragging} data-dock-key={key} animate:flip={{ duration: preferences.reduced_motion ? 0 : 220 }}>
				{#if key === NEW}
					<span class="gap" style:width="{base}px"></span>
				{:else}
					<DockItem mouse_x={dragging ? null : dock_mouse_x} app_id={key} {base} kept removable={removable(key)} />
				{/if}
			</div>
		{/each}

		{#if running.length}
			<div class="divider" aria-hidden="true"></div>
			{#each running as id (id)}
				<DockItem mouse_x={dragging ? null : dock_mouse_x} app_id={id} {base} kept={false} removable={false} />
			{/each}
		{/if}

		<div class="divider" aria-hidden="true"></div>
		<DockTrash mouse_x={dragging ? null : dock_mouse_x} {base} />
	</div>
</section>

<style>
	.dock-container {
		position: fixed;
		left: 0;
		bottom: 0;

		width: 100%;
		padding: 6px 6px 8px;

		display: flex;
		justify-content: center;

		&:not(.dock-hidden) {
			pointer-events: none;
		}
	}

	.dock-el {
		background-color: hsla(var(--system-color-light-hsl), 0.38);

		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 0 0 0.5px hsla(0, 0%, 0%, 0.14),
			0 10px 30px hsla(0, 0%, 0%, 0.18);

		position: relative;
		gap: 4px;

		/* equal space above and below the icon tiles; the running dot sits
		   centred in the bottom space, clear of the Dock edge */
		padding: 0 6px 4px;

		border-radius: 20px;

		height: 100%;

		display: flex;
		align-items: flex-end;

		transition: transform 0.45s var(--zd-spring);

		&:not(.hidden) {
			pointer-events: auto;
		}

		&.drop-target {
			transform: scale(1.03);
			box-shadow:
				inset 0 0 0 1px hsla(0, 0%, 100%, 0.7),
				0 0 0 0.5px hsla(0, 0%, 0%, 0.14),
				0 14px 40px hsla(0, 0%, 0%, 0.26);
		}

		&.hidden {
			transform: translate3d(0, 200%, 0);

			&::before {
				width: calc(100% - 2px);
				height: calc(100% - 2px);

				margin-top: 1px;
				margin-left: 1px;
			}
		}

		&::before {
			content: '';

			border-radius: 20px;

			width: 100%;
			height: 100%;

			border: inherit;

			backdrop-filter: blur(30px) saturate(1.8);

			position: absolute;
			top: 0;
			left: 0;

			z-index: -1;
		}
	}

	.slot {
		display: flex;
		align-items: flex-end;
		height: 100%;

		/* the icon being dragged leaves an empty gap where it will land */
		&.lifted {
			opacity: 0;
		}
	}

	.gap {
		display: block;
		height: 1px;
	}

	.shake {
		animation: shake 360ms ease;
	}

	@keyframes shake {
		20%,
		60% {
			translate: -6px 0;
		}
		40%,
		80% {
			translate: 6px 0;
		}
	}

	.full-note {
		position: absolute;
		bottom: calc(100% + 4px);
		left: 50%;
		translate: -50% 0;
		margin: 0;
		padding: 4px 10px;
		border-radius: 7px;
		white-space: nowrap;
		font-size: 13px;
		color: var(--zd-text);
		background: var(--zd-glass);
		backdrop-filter: blur(30px) saturate(1.8);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 4px 14px hsla(0, 0%, 0%, 0.18);
	}

	.divider {
		align-self: center;
		height: 70%;
		width: 1px;

		background-color: hsla(var(--system-color-dark-hsl), 0.18);

		margin: 0 6px;
	}
</style>
