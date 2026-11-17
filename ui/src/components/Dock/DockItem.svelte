<script module>
	const baseWidth = 50;
	const distanceLimit = baseWidth * 6;
	const beyond_the_distance_limit = distanceLimit + 1;
	const distanceInput = [
		-distanceLimit,
		-distanceLimit / 1.25,
		-distanceLimit / 2,
		0,
		distanceLimit / 2,
		distanceLimit / 1.25,
		distanceLimit,
	];
	const widthOutput = [
		baseWidth,
		baseWidth * 1.1,
		baseWidth * 1.414,
		baseWidth * 2,
		baseWidth * 1.414,
		baseWidth * 1.1,
		baseWidth,
	];
</script>

<script lang="ts">
	import { interpolate } from 'popmotion';
	import { onDestroy, untrack } from 'svelte';
	import { sineInOut } from 'svelte/easing';
	import { spring, tweened } from 'svelte/motion';
	import { elevation } from '🍎/actions';
	import AppIcon from '🍎/components/SVG/AppIcon.svelte';
	import DeviceIcon from '🍎/components/SVG/DeviceIcon.svelte';
	import { apps, config_of, device_windows, is_device_window, open_app, open_device_app, title_of, type AppID } from '🍎/state/apps.svelte.ts';
	import { DRAG_MIME, launchpad, pinned, unpin } from '🍎/state/launchpad.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';

	const {
		mouse_x,
		app_id,
	}: {
		mouse_x: number | null;
		app_id: AppID;
	} = $props();

	let image_el = $state<HTMLElement>();

	let distance = $state(beyond_the_distance_limit);

	const width_px = spring(baseWidth, {
		damping: 0.47,
		stiffness: 0.12,
	});

	const get_width_from_distance = interpolate(distanceInput, widthOutput);

	$effect(() => {
		distance;

		untrack(() => ($width_px = get_width_from_distance(distance)));
	});

	let raf: number;
	function animate() {
		if (image_el && mouse_x !== null) {
			const rect = image_el.getBoundingClientRect();

			// get the x coordinate of the img DOMElement's center
			// the left x coordinate plus the half of the width
			const img_center_x = rect.left + rect.width / 2;

			// difference between the x coordinate value of the mouse pointer
			// and the img center x coordinate value
			const distance_delta = mouse_x - img_center_x;
			distance = distance_delta;
			return;
		}

		distance = beyond_the_distance_limit;
	}

	$effect(() => {
		mouse_x;
		if (preferences.reduced_motion || apps.is_being_dragged) return;

		raf = requestAnimationFrame(animate);
	});

	const title = $derived(title_of(app_id));
	const device_app = $derived(is_device_window(app_id) ? app_id.slice(4) : undefined);
	/** icon source: the device app itself, or the device app a native window rewrites */
	const icon_app = $derived(device_app);
	const is_pinned = $derived(!!device_app && pinned.apps.some((a) => a.id === device_app));
	const is_open = $derived(app_id === 'launchpad' ? launchpad.open : !!apps.open[app_id]);

	// Launch bounce
	const appOpenIconBounceTransform = tweened(0, {
		duration: 400,
		easing: sineInOut,
	});

	async function bounceEffect() {
		if (preferences.reduced_motion) return;
		await appOpenIconBounceTransform.set(-28);
		appOpenIconBounceTransform.set(0);
	}

	function openApp() {
		if (app_id === 'launchpad') {
			launchpad.open = !launchpad.open;
			return;
		}
		const was_open = apps.open[app_id] && !apps.minimized[app_id];
		if (device_app && !device_windows[app_id]) {
			open_device_app({ id: device_app, name: title });
		} else {
			open_app(app_id);
		}
		if (!was_open) bounceEffect();
	}

	// Pinned device apps can be dragged out of the Dock to remove them.
	let dropped_in_dock = false;
	function on_drag_start(e: DragEvent) {
		if (!is_pinned || !device_app) return e.preventDefault();
		dropped_in_dock = false;
		e.dataTransfer?.setData(DRAG_MIME, JSON.stringify({ id: device_app, name: title, from_dock: true }));
		e.dataTransfer!.effectAllowed = 'move';
		window.addEventListener('zerodesk-dock-drop', mark_dropped, { once: true });
	}
	function mark_dropped() {
		dropped_in_dock = true;
	}
	function on_drag_end() {
		window.removeEventListener('zerodesk-dock-drop', mark_dropped);
		if (!dropped_in_dock && device_app) unpin(device_app);
	}

	onDestroy(() => {
		cancelAnimationFrame(raf);
	});
</script>

<button
	onclick={openApp}
	aria-label={title}
	class="dock-open-app-button {app_id}"
	draggable={is_pinned}
	ondragstart={on_drag_start}
	ondragend={on_drag_end}
>
	<p
		class="tooltip"
		class:tooltip-enabled={!apps.is_being_dragged}
		style:transform="translate(-50%, {$appOpenIconBounceTransform}px)"
		use:elevation={'dock-tooltip'}
	>
		{title}
	</p>

	<span style:transform="translate(0, {$appOpenIconBounceTransform}px)">
		<span class="icon" bind:this={image_el} style:width="{$width_px}px" style:height="{$width_px}px">
			{#if icon_app}
				<DeviceIcon id={icon_app} size={$width_px} />
			{:else}
				<AppIcon id={app_id} size={$width_px} />
			{/if}
		</span>
	</span>

	<div class="dot" style:--opacity={app_id === 'launchpad' ? 0 : +is_open}></div>
</button>

<style>
	.icon {
		display: grid;
		place-items: center;
		will-change: width, height;
	}


	button {
		display: flex;
		flex-direction: column;
		justify-content: flex-end;
		align-items: center;
		position: relative;

		&:hover .tooltip.tooltip-enabled,
		&:focus-visible .tooltip.tooltip-enabled {
			opacity: 1;
			visibility: visible;
		}

		& > span {
			display: flex;
			justify-content: center;
			align-items: center;
		}
	}

	.tooltip {
		position: absolute;
		left: 50%;
		bottom: calc(100% + 10px);
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

	.dot {
		height: 4px;
		width: 4px;
		margin-top: 0;
		border-radius: 50%;
		background-color: var(--system-color-dark);
		opacity: calc(var(--opacity) * 0.7);
		transition: opacity 300ms ease;
	}
</style>
