<script lang="ts">
	import {
		bounds,
		BoundsFrom,
		Compartment,
		disabled,
		draggable,
		events,
		position,
		unstable_definePlugin,
	} from '@neodrag/svelte';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { cubicOut } from 'svelte/easing';
	import { apps, close_app, config_of, minimize_app, open_app, open_ids, window_controls, type AppID } from '🍎/state/apps.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';

	import AppNexus from '../../apps/AppNexus.svelte';
	import TrafficLights from './TrafficLights.svelte';

	const { app_id }: { app_id: AppID } = $props();

	const config = $derived(config_of(app_id));
	const MENUBAR = 28;
	const DOCK = 80;

	// Initial size: the configured size, but never more than 86% of the space
	// between the menu bar and the Dock, so a window always opens with margins.
	const FIT = 0.86;
	let width = $state(Math.min(config_of(app_id).width, Math.round(innerWidth * FIT)));
	let height = $state(Math.min(config_of(app_id).height, Math.round((innerHeight - MENUBAR - DOCK) * FIT)));

	// Cascade new windows from the centre like macOS does, starting over after
	// six steps, and never past the right edge or behind the Dock.
	const step = (open_ids().indexOf(app_id) % 6) - 2;
	const default_position = {
		x: Math.max(8, Math.min(innerWidth - width - 8, Math.round((innerWidth - width) / 2) + step * 26)),
		y: Math.max(
			MENUBAR + 6,
			Math.min(innerHeight - DOCK - 6 - height, MENUBAR + Math.round((innerHeight - MENUBAR - DOCK - height) / 2) + step * 18),
		),
	};

	// Window position, kept in step with neodrag so resizing from the top or
	// left edge can move the window while the opposite edge stays put.
	let pos = $state({ ...default_position });
	const position_comp = Compartment.of(() => position({ current: pos, default: default_position }));

	let dragging_enabled = $state(true);
	let is_maximized = $state(false);
	let restore = { width: 0, height: 0, transform: '' };
	let window_el = $state<HTMLElement>();
	let resizing = $state(false);

	const minimized = $derived(apps.minimized[app_id]);
	const disabled_comp = Compartment.of(() => disabled(!dragging_enabled));

	// Only the title bar moves the window, never its content or the buttons in
	// it. Decided per press from the event target, so content that loads later
	// is covered too (neodrag's `controls` measures zones once, at setup).
	const title_bar_only = unstable_definePlugin(() => ({
		name: 'zerodesk:title-bar-only',
		setup() {},
		shouldStart(_ctx: unknown, _state: unknown, event: PointerEvent) {
			const target = event.target as HTMLElement | null;
			return !!target?.closest('.app-window-drag-handle') && !target.closest('button, input, label, a, select');
		},
	}));

	$effect(() => {
		apps.active_z_index;
		if (apps.active === app_id) {
			untrack(() => (apps.z_indices[app_id] = apps.active_z_index));
		}
	});

	function focus_app() {
		if (apps.active !== app_id) open_app(app_id);
	}

	/** Open: rise and settle with a slight overshoot. */
	function window_in(_: HTMLElement): SvelteTransitionReturnType {
		return {
			duration: preferences.reduced_motion ? 0 : 340,
			css: (t) => {
				// critically-damped-ish spring approximation with ~3% overshoot
				const s = 1 - Math.exp(-6 * t) * Math.cos(5.2 * t);
				const scale = 0.9 + 0.1 * s;
				return `opacity: ${Math.min(1, t * 2.4)}; transform: translateY(${(1 - s) * 10}px) scale(${scale})`;
			},
		};
	}

	/** Close: quick shrink and fade, like macOS. */
	function window_out(_: HTMLElement): SvelteTransitionReturnType {
		return {
			duration: preferences.reduced_motion ? 0 : 180,
			easing: cubicOut,
			css: (t) => `opacity: ${t}; transform: scale(${0.94 + 0.06 * t})`,
		};
	}

	async function toggle_maximize() {
		if (!window_el) return;
		// neodrag keeps the window position in the CSS `translate` property
		if (!preferences.reduced_motion) {
			window_el.style.transition =
				'width 0.42s var(--zd-spring), height 0.42s var(--zd-spring), translate 0.42s var(--zd-spring)';
		}
		if (!is_maximized) {
			restore = { width, height, transform: window_el.style.translate };
			dragging_enabled = false;
			window_el.style.translate = `0px ${MENUBAR}px`;
			width = innerWidth;
			height = innerHeight - MENUBAR;
		} else {
			dragging_enabled = true;
			window_el.style.translate = restore.transform;
			width = restore.width;
			height = restore.height;
		}
		is_maximized = !is_maximized;
		apps.fullscreen[app_id] = is_maximized;
		setTimeout(() => window_el && (window_el.style.transition = ''), 450);
	}

	function on_drag_start() {
		focus_app();
		apps.is_being_dragged = true;
	}

	function on_drag_end(data: { offset: { x: number; y: number } }) {
		apps.is_being_dragged = false;
		pos = { x: data.offset.x, y: data.offset.y };
	}

	type Dir = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw';
	const DIRS: Dir[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'];

	/** Resize from any edge or corner with pointer capture; one rAF-coalesced write per frame. */
	function start_resize(e: PointerEvent, dir: Dir) {
		if (!config.resizable || is_maximized) return;
		e.preventDefault();
		e.stopPropagation();
		focus_app();
		const target = e.currentTarget as HTMLElement;
		target.setPointerCapture(e.pointerId);
		const start_x = e.clientX;
		const start_y = e.clientY;
		const start = { w: width, h: height, x: pos.x, y: pos.y };
		let frame = 0;
		let next = { ...start };
		resizing = true;
		apps.is_being_dragged = true;

		const move = (ev: PointerEvent) => {
			const dx = ev.clientX - start_x;
			const dy = ev.clientY - start_y;
			next = { ...start };
			if (dir.includes('e')) next.w = Math.max(config.min_width, start.w + dx);
			if (dir.includes('w')) {
				next.w = Math.max(config.min_width, start.w - dx);
				next.x = start.x + start.w - next.w;
			}
			if (dir.includes('s')) next.h = Math.max(config.min_height, start.h + dy);
			if (dir.includes('n')) {
				// the top edge stops at the menu bar
				const bottom = start.y + start.h;
				next.y = Math.max(MENUBAR, Math.min(bottom - config.min_height, start.y + dy));
				next.h = bottom - next.y;
			}
			if (!frame)
				frame = requestAnimationFrame(() => {
					frame = 0;
					width = next.w;
					height = next.h;
					if (next.x !== pos.x || next.y !== pos.y) pos = { x: next.x, y: next.y };
				});
		};
		const up = () => {
			target.removeEventListener('pointermove', move);
			target.removeEventListener('pointerup', up);
			target.removeEventListener('pointercancel', up);
			resizing = false;
			apps.is_being_dragged = false;
		};
		target.addEventListener('pointermove', move);
		target.addEventListener('pointerup', up);
		target.addEventListener('pointercancel', up);
	}

	onMount(() => {
		// neodrag only records the default position internally until the first
		// drag; apply it to the element so the window opens in the right place.
		if (window_el && !window_el.style.translate) {
			window_el.style.translate = `${default_position.x}px ${default_position.y}px`;
		}
		window_el?.focus();
		// for embedded pages (web apps) that draw their own title bar
		window_controls[app_id] = {
			move_by(dx, dy) {
				if (is_maximized) return;
				pos = {
					x: Math.max(-width + 80, Math.min(innerWidth - 80, pos.x + dx)),
					y: Math.max(MENUBAR, Math.min(innerHeight - 40, pos.y + dy)),
				};
			},
			resize(w, h) {
				if (is_maximized) return;
				width = Math.max(config.min_width, Math.min(innerWidth, Math.round(w)));
				height = Math.max(config.min_height, Math.min(innerHeight - MENUBAR, Math.round(h)));
			},
			zoom() {
				if (config.expandable) toggle_maximize();
			},
		};
	});

	onDestroy(() => delete window_controls[app_id]);
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<section
	role="application"
	class="window"
	class:active={apps.active === app_id}
	class:minimized
	class:maximized={is_maximized}
	class:resizing
	style:width="{width}px"
	style:height="{height}px"
	style:z-index={apps.z_indices[app_id]}
	tabindex="-1"
	bind:this={window_el}
	{@attach draggable(() => [
		title_bar_only(),
		bounds(BoundsFrom.viewport({ bottom: -6000, top: MENUBAR, left: -6000, right: -6000 })),
		disabled_comp,
		position_comp,
		events({ onDragStart: on_drag_start, onDragEnd: on_drag_end }),
	])}
	onpointerdown={focus_app}
	ondblclick={(e) => {
		if ((e.target as HTMLElement).closest('.app-window-drag-handle') && !(e.target as HTMLElement).closest('button, input')) {
			if (config.expandable) toggle_maximize();
		}
	}}
	in:window_in
	out:window_out
>
	<div class="surface">
		<div class="tl-container">
			<TrafficLights
				{app_id}
				on_maximize_click={toggle_maximize}
				on_close_app={() => close_app(app_id)}
				on_minimize_app={() => minimize_app(app_id)}
			/>
		</div>

		<AppNexus {app_id} is_being_dragged={apps.is_being_dragged} />
	</div>

	{#if config.resizable && !is_maximized}
		{#each DIRS as dir (dir)}
			<div class="resize {dir}" onpointerdown={(e) => start_resize(e, dir)}></div>
		{/each}
	{/if}
</section>

<style>
	.window {
		--shadow: 0 0 0 0.5px hsla(0, 0%, 0%, 0.18), 0 8px 24px hsla(0, 0%, 0%, 0.14),
			0 24px 60px hsla(0, 0%, 0%, 0.16);

		position: absolute;
		top: 0;
		left: 0;
		border-radius: var(--zd-radius-window);
		box-shadow: var(--shadow);
		transform-origin: 50% 60%;
		contain: layout style;

		&.active {
			--shadow: 0 0 0 0.5px hsla(0, 0%, 0%, 0.22), 0 12px 32px hsla(0, 0%, 0%, 0.22),
				0 36px 90px hsla(0, 0%, 0%, 0.28);
		}

		&.maximized {
			border-radius: 0;

			.surface {
				border-radius: 0;
			}
		}

		/* minimize: shrink towards the dock (transform only; neodrag owns translate) */
		&.minimized {
			pointer-events: none;
			opacity: 0;
			transform: translateY(55vh) scale(0.18);
			transform-origin: 50% 100%;
			transition:
				opacity 300ms ease,
				transform 380ms var(--zd-ease-out);
		}

		&:not(.minimized) {
			transition:
				opacity 220ms ease,
				transform 460ms var(--zd-spring),
				box-shadow 200ms ease;
		}

		&.resizing {
			transition: none;
		}
	}

	:global(body.dark) .window {
		--shadow: 0 0 0 0.5px hsla(0, 0%, 0%, 0.8), inset 0 0 0 0.5px hsla(0, 0%, 100%, 0.12),
			0 12px 40px hsla(0, 0%, 0%, 0.5);
	}

	.surface {
		position: relative;
		width: 100%;
		height: 100%;
		overflow: hidden;
		border-radius: inherit;
		background: var(--zd-window-bg);
		color: var(--zd-text);
		display: grid;
		grid-template-rows: 1fr;
	}

	:global(body.dark) .surface {
		box-shadow: inset 0 0 0 0.5px hsla(0, 0%, 100%, 0.12);
	}

	.tl-container {
		position: absolute;
		top: 0;
		left: 0;
		height: var(--zd-titlebar);
		padding-left: 19px;
		display: flex;
		align-items: center;
		z-index: 10;
	}

	.resize {
		position: absolute;
		z-index: 20;

		/* edges: 8px bands that stop short of the rounded corners */
		&.n,
		&.s {
			left: 14px;
			right: 14px;
			height: 8px;
			cursor: ns-resize;
		}

		&.n {
			top: -4px;
		}

		&.s {
			bottom: -4px;
		}

		&.e,
		&.w {
			top: 14px;
			bottom: 14px;
			width: 8px;
			cursor: ew-resize;
		}

		&.e {
			right: -4px;
		}

		&.w {
			left: -4px;
		}

		/* corners: 18px squares over the rounded corners */
		&.ne,
		&.nw,
		&.se,
		&.sw {
			width: 18px;
			height: 18px;
		}

		&.nw {
			top: -5px;
			left: -5px;
			cursor: nwse-resize;
		}

		&.se {
			right: -5px;
			bottom: -5px;
			cursor: nwse-resize;
		}

		&.ne {
			top: -5px;
			right: -5px;
			cursor: nesw-resize;
		}

		&.sw {
			bottom: -5px;
			left: -5px;
			cursor: nesw-resize;
		}
	}

</style>
