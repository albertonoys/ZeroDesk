<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { ARROWS, DEVICE_ROWS, KEY_BY_NAME, KEYCODES } from '🍎/lib/keymap.ts';
	import { ScreenStream, type ScreenStatus } from '🍎/lib/screen.ts';
	import type { UpscaleBackend } from '🍎/lib/upscale.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';

	// A live 320x170 screen with keyboard capture, wheel/trackpad scrolling,
	// mouse touch (apps on a virtual screen) and an optional on-screen
	// keyboard. Used for the mirrored LCD and for apps on a virtual screen.
	let {
		path,
		show_keys = false,
		pointer = false,
		status = $bindable<ScreenStatus>('connecting'),
	}: { path: string; show_keys?: boolean; pointer?: boolean; status?: ScreenStatus } = $props();

	let canvas = $state<HTMLCanvasElement>();
	let sr_canvas = $state<HTMLCanvasElement>();
	let backend = $state<UpscaleBackend>('sharp');
	let bezel = $state<HTMLElement>();
	let snap = $state({ x: 0, y: 0 });
	let stage = $state<HTMLElement>();
	let stream: ScreenStream | undefined;
	let size = $state({ w: 320, h: 170 });
	let stage_box = $state({ w: 0, h: 0 });
	let focused = $state(false);
	let flash = $state(false);
	const pressed = new Set<number>();
	let held = $state<Record<number, boolean>>({});

	let dpr = $state(window.devicePixelRatio || 1);
	const scale = $derived.by(() => {
		const pad = 72;
		const keys_h = show_keys ? 196 : 0;
		const s = Math.min((stage_box.w - pad) / size.w, (stage_box.h - pad - keys_h) / size.h);
		if (!Number.isFinite(s) || s <= 0) return 1;
		// pixel: a whole number of device pixels per LCD pixel, every pixel the
		// same size; smooth: fill the space, sized to whole device pixels
		return preferences.screen_smooth
			? Math.max(1, Math.floor(size.w * s * dpr)) / (size.w * dpr)
			: Math.max(1, Math.floor(s * dpr)) / dpr;
	});
	const css_w = $derived(Math.round(size.w * scale * dpr) / dpr);
	const css_h = $derived(Math.round(size.h * scale * dpr) / dpr);

	// Centring can leave the canvas half a device pixel off the grid, which
	// makes the browser resample it. Nudge it back onto whole device pixels.
	function snap_to_pixels() {
		if (!canvas) return;
		const r = canvas.getBoundingClientRect();
		const fx = (r.left - snap.x) * dpr;
		const fy = (r.top - snap.y) * dpr;
		const next = { x: (Math.round(fx) - fx) / dpr, y: (Math.round(fy) - fy) / dpr };
		if (Math.abs(next.x - snap.x) > 0.01 || Math.abs(next.y - snap.y) > 0.01) snap = next;
	}
	$effect(() => {
		css_w;
		css_h;
		stage_box;
		requestAnimationFrame(snap_to_pixels);
	});

	onMount(() => {
		stream = new ScreenStream(
			canvas!,
			(s) => (status = s),
			(w, h) => (size = { w, h }),
			path,
			sr_canvas,
			(b) => (backend = b),
		);
		const mq = matchMedia(`(resolution: ${dpr}dppx)`);
		const on_dpr = () => (dpr = window.devicePixelRatio || 1);
		mq.addEventListener('change', on_dpr);
		// windows move by whole CSS pixels; re-check the grid after a drag ends
		const on_up = () => requestAnimationFrame(snap_to_pixels);
		window.addEventListener('pointerup', on_up);
		stream.set_smooth(preferences.screen_smooth);
		const ro = new ResizeObserver(([e]) => {
			stage_box = { w: e.contentRect.width, h: e.contentRect.height };
		});
		ro.observe(stage!);
		return () => {
			ro.disconnect();
			mq.removeEventListener('change', on_dpr);
			window.removeEventListener('pointerup', on_up);
		};
	});

	onDestroy(() => stream?.close());

	$effect(() => {
		stream?.set_smooth(preferences.screen_smooth);
	});

	export async function snapshot() {
		const blob = await stream?.snapshot();
		if (!blob) return;
		flash = true;
		setTimeout(() => (flash = false), 220);
		return blob;
	}

	export function focus() {
		stage?.focus();
	}

	function on_key(e: KeyboardEvent, down: boolean) {
		if (e.metaKey) return; // keep ⌘ shortcuts for the computer
		// some remote-desktop / IME setups send an empty `code`; fall back to the key name
		const code = KEYCODES[e.code] ?? KEY_BY_NAME[e.key];
		if (!code) return;
		e.preventDefault();
		if (down) {
			if (e.repeat) return stream?.key(code, 2);
			pressed.add(code);
			stream?.key(code, 1);
		} else {
			pressed.delete(code);
			stream?.key(code, 0);
		}
	}

	function release_all() {
		for (const c of pressed) stream?.key(c, 0);
		pressed.clear();
		focused = false;
	}

	// Apps are driven by the keyboard, so scrolling becomes arrow keys:
	// a mouse wheel moves one item per notch; a trackpad accumulates pixels
	// (one item per ~36 px) in both directions; a gesture that pauses starts
	// fresh, like a native list that follows the fingers.
	let acc = { x: 0, y: 0 };
	let last_wheel = 0;
	function on_wheel(e: WheelEvent) {
		if (e.ctrlKey) return; // pinch: let the browser zoom
		e.preventDefault();
		const now = performance.now();
		if (now - last_wheel > 220) acc = { x: 0, y: 0 };
		last_wheel = now;
		// a notched wheel reports lines, or ±100-ish pixels without fine steps
		const notched = e.deltaMode === 1 || (Math.abs(e.deltaY) >= 50 && Number.isInteger(e.deltaY) && e.deltaX === 0);
		const k = e.deltaMode === 1 ? 36 : 1;
		acc.x += e.deltaX * k;
		acc.y += e.deltaY * k;
		const step = notched ? Math.max(1, Math.abs(e.deltaY * k)) : 36;
		while (Math.abs(acc.y) >= step) {
			stream?.tap(acc.y > 0 ? 108 : 103); // down : up
			acc.y -= Math.sign(acc.y) * step;
		}
		while (Math.abs(acc.x) >= 36) {
			stream?.tap(acc.x > 0 ? 106 : 105); // right : left
			acc.x -= Math.sign(acc.x) * 36;
		}
	}

	// Mouse and trackpad clicks/drags become touches on the app's screen.
	let touching = false;
	let move_raf = 0;
	let last_pt = { x: 0, y: 0 };
	function screen_point(e: PointerEvent) {
		const r = canvas!.getBoundingClientRect();
		return { x: ((e.clientX - r.left) / r.width) * size.w, y: ((e.clientY - r.top) / r.height) * size.h };
	}
	function on_pointerdown(e: PointerEvent) {
		stage?.focus();
		if (!pointer || e.button !== 0) return;
		e.preventDefault();
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		touching = true;
		last_pt = screen_point(e);
		stream?.pointer(last_pt.x, last_pt.y, true);
	}
	function on_pointermove(e: PointerEvent) {
		if (!touching) return;
		last_pt = screen_point(e);
		move_raf ||= requestAnimationFrame(() => {
			move_raf = 0;
			if (touching) stream?.pointer(last_pt.x, last_pt.y, true);
		});
	}
	function on_pointerup(e: PointerEvent) {
		if (!touching) return;
		touching = false;
		last_pt = screen_point(e);
		stream?.pointer(last_pt.x, last_pt.y, false);
	}

	function vkey_down(code: number) {
		held = { ...held, [code]: true };
		stream?.key(code, 1);
	}

	function vkey_up(code: number) {
		if (!held[code]) return;
		const { [code]: _, ...rest } = held;
		held = rest;
		stream?.key(code, 0);
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	class="stage"
	bind:this={stage}
	tabindex="0"
	role="application"
	aria-label={t('device.typing_hint')}
	onfocus={() => (focused = true)}
	onblur={release_all}
	onkeydown={(e) => on_key(e, true)}
	onkeyup={(e) => on_key(e, false)}
	onwheel={on_wheel}
>
	<div class="bezel" class:focused class:flash style:--s={scale} bind:this={bezel}>
		<div
			class="screen"
			class:touch={pointer}
			style:width="{css_w}px"
			style:height="{css_h}px"
			style:translate="{snap.x}px {snap.y}px"
			onpointerdown={on_pointerdown}
			onpointermove={on_pointermove}
			onpointerup={on_pointerup}
			onpointercancel={on_pointerup}
			role="presentation"
		>
			<canvas bind:this={canvas} class:dim={status !== 'live'} class:hidden={backend === 'websr'}></canvas>
			<canvas
				bind:this={sr_canvas}
				class="sr"
				class:dim={status !== 'live'}
				style:visibility={backend === 'websr' ? 'visible' : 'hidden'}
			></canvas>
		</div>
		{#if status !== 'live'}
			<div class="overlay">
				{#if status !== 'nosignal'}<span class="spinner"></span>{/if}
				<span>{status === 'offline' ? t('device.offline') : status === 'nosignal' ? t('device.nosignal') : t('device.connecting')}</span>
			</div>
		{/if}
	</div>


	{#if show_keys}
		<div class="keys" role="group" aria-label={t('device.keyboard')}>
			{#each DEVICE_ROWS as row}
				<div class="row">
					{#each row as [label, code, w]}
						<button
							class="key"
							class:down={held[code]}
							style:flex-grow={w ?? 1}
							tabindex="-1"
							onpointerdown={(e) => (e.preventDefault(), vkey_down(code))}
							onpointerup={() => vkey_up(code)}
							onpointerleave={() => vkey_up(code)}>{label}</button
						>
					{/each}
				</div>
			{/each}
			<div class="row arrows">
				{#each ARROWS as [label, code]}
					<button
						class="key"
						class:down={held[code]}
						tabindex="-1"
						onpointerdown={(e) => (e.preventDefault(), vkey_down(code))}
						onpointerup={() => vkey_up(code)}
						onpointerleave={() => vkey_up(code)}>{label}</button
					>
				{/each}
			</div>
		</div>
	{/if}
</div>

<style>
	.stage {
		position: relative;
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 14px;
		background: hsla(0, 0%, 50%, 0.07);
		cursor: default;
		outline: none;
	}

	.bezel {
		position: relative;
		padding: calc(6px + var(--s) * 3px);
		border-radius: calc(10px + var(--s) * 2.5px);
		background: #1c1c1e;
		box-shadow:
			inset 0 0 0 1px hsla(0, 0%, 100%, 0.06),
			0 1px 2px hsla(0, 0%, 0%, 0.2),
			0 14px 40px hsla(0, 0%, 0%, 0.22);
		transition: box-shadow 240ms ease;

		&.focused {
			box-shadow:
				inset 0 0 0 1px hsla(0, 0%, 100%, 0.08),
				0 0 0 3px hsla(0, 0%, 50%, 0.28),
				0 18px 48px hsla(0, 0%, 0%, 0.28);
		}

		&.flash canvas {
			animation: flash 220ms ease-out;
		}
	}

	.screen {
		position: relative;
		border-radius: calc(2px + var(--s) * 0.6px);
		overflow: hidden;
		background: #000;
		touch-action: none;
	}

	canvas {
		position: absolute;
		inset: 0;
		display: block;
		width: 100%;
		height: 100%;
		transition: opacity 300ms ease;

		&.dim {
			opacity: 0.35;
		}

		&.hidden {
			visibility: hidden;
		}
	}

	.overlay {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 10px;
		color: hsla(0, 0%, 100%, 0.85);
		font-size: 13px;
	}

	.spinner {
		width: 14px;
		height: 14px;
		border-radius: 50%;
		border: 1.6px solid hsla(0, 0%, 100%, 0.25);
		border-top-color: hsla(0, 0%, 100%, 0.9);
		animation: spin 0.9s linear infinite;
	}

	.keys {
		width: min(640px, calc(100% - 40px));
		display: flex;
		flex-direction: column;
		gap: 5px;
		padding: 10px;
		border-radius: var(--zd-radius-panel);
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);
		animation: rise 320ms var(--zd-spring);
	}

	.row {
		display: flex;
		gap: 5px;

		&.arrows {
			justify-content: flex-end;

			.key {
				flex: 0 0 52px;
			}
		}
	}

	.key {
		flex: 1 1 0;
		min-width: 0;
		height: 30px;
		border-radius: var(--zd-radius-control);
		background: var(--zd-window-bg);
		box-shadow:
			0 0.5px 0 hsla(0, 0%, 0%, 0.18),
			inset 0 0 0 0.5px var(--zd-hairline);
		font-size: 12px;
		color: var(--zd-text);
		transition:
			transform 160ms var(--zd-spring),
			background-color 100ms ease;

		&.down {
			transform: scale(0.94);
			background: var(--zd-selected);
		}
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	@keyframes flash {
		0% {
			filter: brightness(2.2);
		}
	}

	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(10px);
		}
	}
</style>
