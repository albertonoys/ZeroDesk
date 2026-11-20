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
		layout = null,
		on_input,
		status = $bindable<ScreenStatus>('connecting'),
	}: {
		path: string;
		show_keys?: boolean;
		pointer?: boolean;
		status?: ScreenStatus;
		/** what is on screen, when known: the launcher's carousel gets tap-to-open */
		layout?: 'launcher' | null;
		/** called after a tap or swipe was turned into keys */
		on_input?: () => void;
	} = $props();

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

	/** Whether the frame on screen is the launcher's home screen (its carousel). */
	export function shows_home(): boolean {
		return stream?.frame_stats().home ?? false;
	}

	/**
	 * Hold a key down for `ms`, with key repeats like a held key. The launcher
	 * leaves the pages it draws itself (CLI, Python, Settings, games) when ESC
	 * is held for 3 seconds.
	 */
	export function hold(code: number, ms: number) {
		stream?.key(code, 1);
		const repeat = setInterval(() => stream?.key(code, 2), 100);
		setTimeout(() => {
			clearInterval(repeat);
			stream?.key(code, 0);
		}, ms);
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
	// The launcher and the official apps only take keys (they ignore touch even
	// with a pointer device), so taps and swipes are turned into keys. On the
	// launcher, whose layout is fixed, a tap opens the icon under it, like a
	// phone's home screen; elsewhere a tap confirms and a swipe moves.
	const KEY = { up: 103, down: 108, left: 105, right: 106, enter: 28, esc: 1, tab: 15 } as const;
	// 'tap' is a plain Enter: any screen other than the launcher's home
	type Zone = 'prev' | 'next' | 'left' | 'center' | 'right' | 'wake' | 'tap' | 'none';
	// the launcher's carousel, in its 320x170 screen pixels
	const ICONS: Record<'left' | 'center' | 'right', [number, number, number, number]> = {
		left: [20, 48, 102, 158],
		center: [108, 28, 212, 158],
		right: [218, 48, 300, 158],
	};
	let gesture: { x: number; y: number } | null = null;
	let hover_zone = $state<Zone>('none');

	function zone_of(e: PointerEvent | MouseEvent): Zone {
		const r = canvas!.getBoundingClientRect();
		const x = ((e.clientX - r.left) / r.width) * 320;
		const y = ((e.clientY - r.top) / r.height) * 170;
		if (layout !== 'launcher') return 'tap';
		// off or locked ("Press TAB to unlock"): any tap wakes and unlocks it
		const f = stream?.frame_stats();
		if (!f || !f.lit || f.locked) return 'wake';
		// CLI, Python, Settings and the games are drawn by the launcher itself,
		// so only the frame tells its home screen apart from them
		if (!f.home) return 'tap';
		if (y < 26) return 'none'; // status bar
		for (const [z, [x0, y0, x1, y1]] of Object.entries(ICONS)) if (x >= x0 && x < x1 && y >= y0 && y < y1) return z as Zone;
		if (y >= 48 && y < 140) return x < 20 ? 'prev' : x >= 300 ? 'next' : 'none';
		return 'none';
	}

	// A tap on the dark or locked screen wakes it and answers the lock screen,
	// which can ask for more than one key in a row ("Press TAB", then "Press
	// ENTER"). The screen can take seconds to light, so Shift is pressed again
	// while it stays dark. Each prompt is read until two readings agree, its key is pressed
	// once, and the next prompt is read after the screen has redrawn. Once the
	// prompt is gone nothing more is pressed, so no key lands on the home
	// screen and opens the app in the middle.
	let unlocking = false;
	function unlock() {
		if (unlocking) return;
		const f = stream?.frame_stats();
		if (!f) return;
		if (f.lit && !f.locked) return;
		unlocking = true;
		if (!f.lit) stream?.tap(42); // Shift only lights the screen
		let last = 0;
		let tries = 0;
		let pressed = 0;
		const check = () => {
			const g = stream?.frame_stats();
			if (g?.lit && !g.locked && pressed > 0) return void (unlocking = false);
			if (g?.locked && g.unlock_key === last) {
				press(g.unlock_key);
				last = 0;
				if (++pressed < 3) return void setTimeout(check, 900);
				return void setTimeout(() => (unlocking = false), 900);
			}
			last = g?.locked ? g.unlock_key : 0;
			// from deep sleep the screen can take seconds to light: ask again
			if (g && !g.lit && tries % 8 === 7) stream?.tap(42);
			if (++tries < 32) setTimeout(check, 250);
			else unlocking = false;
		};
		setTimeout(check, f.lit ? 0 : 400);
	}

	function press(...codes: number[]) {
		codes.forEach((c, i) => setTimeout(() => stream?.tap(c), i * 260)); // let the carousel slide first
		on_input?.();
		hover_zone = 'none'; // the screen is about to change under the mouse
	}

	function on_pointerdown(e: PointerEvent) {
		stage?.focus();
		if (e.button !== 0) return;
		e.preventDefault();
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		if (!pointer) {
			gesture = { x: e.clientX, y: e.clientY };
			return;
		}
		touching = true;
		last_pt = screen_point(e);
		stream?.pointer(last_pt.x, last_pt.y, true);
	}
	function on_pointermove(e: PointerEvent) {
		if (!pointer) {
			if (e.pointerType === 'mouse' && !gesture) hover_zone = status === 'live' ? zone_of(e) : 'none';
			return;
		}
		if (!touching) return;
		last_pt = screen_point(e);
		move_raf ||= requestAnimationFrame(() => {
			move_raf = 0;
			if (touching) stream?.pointer(last_pt.x, last_pt.y, true);
		});
	}
	function on_pointerup(e: PointerEvent) {
		if (!pointer) {
			if (!gesture || e.type === 'pointercancel') return void (gesture = null);
			const r = canvas!.getBoundingClientRect();
			const dx = e.clientX - gesture.x;
			const dy = e.clientY - gesture.y;
			gesture = null;
			const swipe = Math.max(16, r.width * 0.08);
			if (Math.abs(dx) >= swipe || Math.abs(dy) >= swipe) {
				// content follows the finger: swiping left brings in the next item
				if (Math.abs(dx) > Math.abs(dy)) press(dx < 0 ? KEY.right : KEY.left);
				else press(dy < 0 ? KEY.down : KEY.up);
				return;
			}
			const z = zone_of(e);
			if (z === 'wake') unlock();
			else if (z === 'center' || z === 'tap') press(KEY.enter);
			else if (z === 'left') press(KEY.left, KEY.enter);
			else if (z === 'right') press(KEY.right, KEY.enter);
			else if (z === 'prev') press(KEY.left);
			else if (z === 'next') press(KEY.right);
			return;
		}
		if (!touching) return;
		touching = false;
		last_pt = screen_point(e);
		stream?.pointer(last_pt.x, last_pt.y, false);
	}
	function on_contextmenu(e: MouseEvent) {
		if (pointer) return;
		e.preventDefault();
		press(KEY.esc);
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
			class:keys-by-tap={!pointer && status === 'live'}
			style:width="{css_w}px"
			style:height="{css_h}px"
			style:translate="{snap.x}px {snap.y}px"
			onpointerdown={on_pointerdown}
			onpointermove={on_pointermove}
			onpointerup={on_pointerup}
			onpointercancel={on_pointerup}
			onpointerleave={() => (hover_zone = 'none')}
			oncontextmenu={on_contextmenu}
			role="presentation"
		>
			<canvas bind:this={canvas} class:dim={status !== 'live'} class:hidden={backend === 'websr'}></canvas>
			<canvas
				bind:this={sr_canvas}
				class="sr"
				class:dim={status !== 'live'}
				style:visibility={backend === 'websr' ? 'visible' : 'hidden'}
			></canvas>
			{#if !pointer && status === 'live' && layout === 'launcher'}
				<!-- what a tap would open or move to, shown while the mouse is over it -->
				{#each Object.entries(ICONS) as [z, [x0, y0, x1, y1]] (z)}
					<span
						class="hint-box"
						class:on={hover_zone === z}
						style:left="{(x0 / 320) * 100}%"
						style:top="{(y0 / 170) * 100}%"
						style:width="{((x1 - x0) / 320) * 100}%"
						style:height="{((y1 - y0) / 170) * 100}%"
						aria-hidden="true"
					></span>
				{/each}
				<span class="hint left" class:on={hover_zone === 'prev'} aria-hidden="true">
					<svg viewBox="0 0 12 20"><path d="M9 3 3 10l6 7" /></svg>
				</span>
				<span class="hint right" class:on={hover_zone === 'next'} aria-hidden="true">
					<svg viewBox="0 0 12 20"><path d="M3 3l6 7-6 7" /></svg>
				</span>
			{/if}
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

	.keys-by-tap {
		cursor: pointer;
		touch-action: none;
	}

	.hint-box {
		position: absolute;
		border-radius: 10%;
		background: hsla(0, 0%, 100%, 0.1);
		box-shadow: inset 0 0 0 1.5px hsla(0, 0%, 100%, 0.35);
		opacity: 0;
		transition: opacity 140ms ease;
		pointer-events: none;

		&.on {
			opacity: 1;
		}
	}

	.hint {
		position: absolute;
		top: 50%;
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		margin-top: -14px;
		border-radius: 50%;
		background: hsla(0, 0%, 100%, 0.16);
		backdrop-filter: blur(8px);
		opacity: 0;
		transition: opacity 140ms ease;
		pointer-events: none;

		&.left {
			left: 0;
		}

		&.right {
			right: 0;
		}

		&.on {
			opacity: 1;
		}

		svg {
			width: 9px;
			height: 15px;
			fill: none;
			stroke: #fff;
			stroke-width: 2.4;
			stroke-linecap: round;
			stroke-linejoin: round;
		}
	}
</style>
