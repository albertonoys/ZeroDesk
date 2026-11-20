<script module>
	// magnification: icons within this many base widths of the pointer grow
	const REACH = 6;
</script>

<script lang="ts">
	import { preload_app, preload_data, preload_icons } from '🍎/state/app_preload.ts';
	import { interpolate } from 'popmotion';
	import { onDestroy, untrack } from 'svelte';
	import { sineInOut } from 'svelte/easing';
	import { spring, tweened } from 'svelte/motion';
	import { elevation } from '🍎/actions';
	import AppIcon from '🍎/components/SVG/AppIcon.svelte';
	import DeviceIcon from '🍎/components/SVG/DeviceIcon.svelte';
	import { apps, close_app, config_of, device_windows, is_device_window, native_of, open_app, open_device_app, title_of, web_apps, web_for_device, type AppID } from '🍎/state/apps.svelte.ts';
	import { show_context_menu } from '🍎/state/contextmenu.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { launchpad, pin, unpin } from '🍎/state/launchpad.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';

	const {
		mouse_x,
		app_id,
		base = 50,
		kept = false,
		removable = false,
	}: {
		mouse_x: number | null;
		app_id: AppID;
		/** resting icon size; the Dock shrinks it to fit the screen */
		base?: number;
		/** kept in the Dock (can be dragged to reorder) */
		kept?: boolean;
		/** can leave the Dock (dragged out or Remove from Dock) */
		removable?: boolean;
	} = $props();

	let image_el = $state<HTMLElement>();

	const limit = $derived(base * REACH);
	let distance = $state(Infinity);

	const width_px = spring(untrack(() => base), {
		damping: 0.47,
		stiffness: 0.12,
	});

	const get_width_from_distance = $derived(
		interpolate(
			[-limit, -limit / 1.25, -limit / 2, 0, limit / 2, limit / 1.25, limit],
			[base, base * 1.1, base * 1.414, base * 2, base * 1.414, base * 1.1, base],
		),
	);

	$effect(() => {
		const w = Math.abs(distance) > limit ? base : get_width_from_distance(distance);
		untrack(() => ($width_px = w));
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

		distance = Infinity;
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
	const web_window = $derived(device_app ? web_for_device(device_app) : undefined);
	const native_window = $derived(device_app ? native_of[device_app] : undefined);
	const is_open = $derived(
		app_id === 'launchpad'
			? launchpad.open
			: !!apps.open[app_id] || (!!web_window && !!apps.open[`web:${web_window.id}`]) || (!!native_window && !!apps.open[native_window]),
	);

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

	// get the window's code and first data ready before the click lands
	function warm() {
		if (app_id === 'launchpad') {
			preload_data('launchpad');
			preload_icons();
		} else preload_app(device_app ? (web_window ? `web:${web_window.id}` : (native_of[device_app] ?? `dev:${device_app}`)) : app_id);
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

	/** the device app this item stands for, when it can be kept in the Dock */
	const keep_target = $derived.by(() => {
		if (device_app) return device_app;
		if (app_id.startsWith('web:')) return web_apps[app_id.slice(4)]?.device_app;
		return (config_of(app_id) as { native_for?: string } | undefined)?.native_for;
	});

	// right click, as on a Mac: Keep in Dock / Remove from Dock, Quit
	function menu(e: MouseEvent) {
		if (app_id === 'launchpad') return;
		const items = [];
		if (!is_open) items.push({ label: t('apps.open'), action: openApp });
		if (!kept && keep_target) items.push({ label: t('apps.keep_in_dock'), action: () => pin({ id: keep_target, name: title }) });
		if (kept && removable && device_app) items.push({ label: t('dock.remove'), action: () => unpin(device_app) });
		if (is_open && app_id !== 'finder') {
			const open_id = apps.open[app_id] ? app_id : web_window && apps.open[`web:${web_window.id}`] ? `web:${web_window.id}` : native_window;
			if (open_id) items.push({ label: t('dock.quit'), action: () => close_app(open_id) });
		}
		if (items.length) show_context_menu(e, items);
	}

	onDestroy(() => {
		cancelAnimationFrame(raf);
	});
</script>

<button
	onpointerenter={warm}
	onpointerdown={warm}
	onclick={openApp}
	aria-label={title}
	class="dock-open-app-button {app_id}"
	draggable="false"
	oncontextmenu={menu}
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
