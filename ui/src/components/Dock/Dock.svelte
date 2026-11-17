<svelte:options runes={true} />

<script lang="ts">
	import { elevation } from '🍎/actions';
	import { apps_config } from '🍎/configs/apps/apps-config';
	import { apps, device_windows, web_apps, web_windows } from '🍎/state/apps.svelte';
	import { DRAG_MIME, pin, pinned } from '🍎/state/launchpad.svelte.ts';
	import { is_dock_hidden } from '🍎/state/dock.svelte';
	import DockItem from './DockItem.svelte';
	import DockTrash from './DockTrash.svelte';
	import { untrack } from 'svelte';

	let dock_mouse_x = $state<number | null>(null);

	const HIDDEN_DOCK_THRESHOLD = 30;
	let bodyHeight = $state(0);
	let mouseY = $state(0);

	let dockContainerEl = $state<HTMLElement>();

	// Pinned device apps first (in pin order), then any other running ones.
	const device_ids = $derived([
		...pinned.apps.map((a) => `dev:${a.id}`),
		...Object.keys(device_windows).filter((w) => !pinned.apps.some((a) => `dev:${a.id}` === w)),
	]);

	// native rewrites of device apps sit with the running apps, only while open
	const running_native = $derived([
		...Object.entries(apps_config)
			.filter(([id, c]) => (c as { dock_when_running?: boolean }).dock_when_running && apps.open[id])
			.map(([id]) => id),
		// web-app windows, unless their device app is pinned (that item shows them)
		...Object.keys(web_windows).filter((w) => {
			const dev = web_apps[w.slice(4)]?.device_app;
			return !(dev && pinned.apps.some((a) => a.id === dev));
		}),
	]);

	let drop_target = $state(false);

	function accepts(e: DragEvent) {
		return !!e.dataTransfer?.types.includes(DRAG_MIME);
	}

	function on_drag_over(e: DragEvent) {
		if (!accepts(e)) return;
		e.preventDefault();
		e.dataTransfer!.dropEffect = 'copy';
		drop_target = true;
	}

	function on_drop(e: DragEvent) {
		drop_target = false;
		if (!accepts(e)) return;
		e.preventDefault();
		try {
			const app = JSON.parse(e.dataTransfer!.getData(DRAG_MIME));
			window.dispatchEvent(new Event('zerodesk-dock-drop'));
			pin({ id: app.id, name: app.name });
		} catch {
			/* ignore malformed drops */
		}
	}

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


<svelte:window bind:innerHeight={bodyHeight} />

<section
	class="dock-container"
	class:dock-hidden={is_dock_hidden.value}
	bind:this={dockContainerEl}
	use:elevation={'dock'}
>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="dock-el"
		class:hidden={is_dock_hidden.value}
		class:drop-target={drop_target}
		role="toolbar"
		tabindex="-1"
		ondragover={on_drag_over}
		ondragleave={() => (drop_target = false)}
		ondrop={on_drop}
		onmousemove={(event) => (dock_mouse_x = event.x)}
		onmouseleave={() => (dock_mouse_x = null)}
	>
		{#each Object.entries(apps_config) as [appID, config]}
			{#if !config.dock_when_running}
				{#if config.dock_breaks_before}
					<div class="divider" aria-hidden="true"></div>
				{/if}

				<DockItem mouse_x={dock_mouse_x} app_id={appID} />
			{/if}
		{/each}

		{#if running_native.length || device_ids.length}
			<div class="divider" aria-hidden="true"></div>
			{#each running_native as nid (nid)}
				<DockItem mouse_x={dock_mouse_x} app_id={nid} />
			{/each}
			{#each device_ids as wid (wid)}
				<DockItem mouse_x={dock_mouse_x} app_id={wid} />
			{/each}
		{/if}

		<div class="divider" aria-hidden="true"></div>
		<DockTrash />
	</div>
</section>

<style>
	.dock-container {
		position: fixed;
		left: 0;
		bottom: 0;

		width: 100%;
		height: 80px;

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

	.divider {
		align-self: center;
		height: 70%;
		width: 1px;

		background-color: hsla(var(--system-color-dark-hsl), 0.18);

		margin: 0 6px;
	}
</style>
