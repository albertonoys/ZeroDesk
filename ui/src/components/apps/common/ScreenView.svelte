<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { ARROWS, DEVICE_ROWS, KEY_BY_NAME, KEYCODES } from '🍎/lib/keymap.ts';
	import { ScreenStream, type ScreenStatus } from '🍎/lib/screen.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';

	// A live 320x170 screen with keyboard capture, wheel-to-arrow scrolling and
	// an optional on-screen keyboard. Used for the mirrored LCD and for apps
	// running on a virtual screen.
	let {
		path,
		show_keys = false,
		status = $bindable<ScreenStatus>('connecting'),
	}: { path: string; show_keys?: boolean; status?: ScreenStatus } = $props();

	let canvas = $state<HTMLCanvasElement>();
	let stage = $state<HTMLElement>();
	let stream: ScreenStream | undefined;
	let size = $state({ w: 320, h: 170 });
	let stage_box = $state({ w: 0, h: 0 });
	let focused = $state(false);
	let flash = $state(false);
	const pressed = new Set<number>();
	let held = $state<Record<number, boolean>>({});

	const scale = $derived.by(() => {
		const pad = 72;
		const keys_h = show_keys ? 196 : 0;
		const s = Math.min((stage_box.w - pad) / size.w, (stage_box.h - pad - keys_h) / size.h);
		if (!Number.isFinite(s) || s <= 0) return 1;
		// pixel mode stays on half-integer steps so pixels keep equal size
		return preferences.screen_smooth ? s : s >= 2 ? Math.floor(s * 2) / 2 : s;
	});

	onMount(() => {
		stream = new ScreenStream(
			canvas!,
			(s) => (status = s),
			(w, h) => (size = { w, h }),
			path,
		);
		stream.set_smooth(preferences.screen_smooth);
		const ro = new ResizeObserver(([e]) => {
			stage_box = { w: e.contentRect.width, h: e.contentRect.height };
		});
		ro.observe(stage!);
		return () => ro.disconnect();
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

	// The device has no touch screen: scrolling (wheel / trackpad) becomes
	// arrow keys, one step per ~40 px of travel.
	let wheel_acc = 0;
	function on_wheel(e: WheelEvent) {
		e.preventDefault();
		wheel_acc += e.deltaY;
		const step = 40;
		while (Math.abs(wheel_acc) >= step) {
			const code = wheel_acc > 0 ? 108 : 103; // down : up
			stream?.tap(code);
			wheel_acc -= Math.sign(wheel_acc) * step;
		}
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
	<div class="bezel" class:focused class:flash style:--s={scale}>
		<canvas
			bind:this={canvas}
			style:width="{size.w * scale}px"
			style:height="{size.h * scale}px"
			class:dim={status !== 'live'}
		></canvas>
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

	canvas {
		display: block;
		border-radius: calc(2px + var(--s) * 0.6px);
		background: #000;
		transition: opacity 300ms ease;

		&.dim {
			opacity: 0.35;
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
