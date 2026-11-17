<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import type { SplendidGrandPiano } from 'smplr';
	import { t } from '🍎/state/i18n.svelte.ts';
	import Toolbar from '../common/Toolbar.svelte';

	// Plays on the computer with Web Audio: Splendid Grand Piano samples
	// (smplr, served by the device from /api/piano), and a small synth until
	// they are loaded or if loading fails. Keyboard layout follows
	// GarageBand's Musical Typing: A S D F G H J K L ; are white keys,
	// W E T Y U O P are black keys, Z / X move the octave.

	const NAMES = ['C', 'C♯', 'D', 'D♯', 'E', 'F', 'F♯', 'G', 'G♯', 'A', 'A♯', 'B'];
	const BLACK = new Set([1, 3, 6, 8, 10]);
	const TYPING: Record<string, number> = {
		KeyA: 0, KeyW: 1, KeyS: 2, KeyE: 3, KeyD: 4, KeyF: 5, KeyT: 6, KeyG: 7, KeyY: 8, KeyH: 9,
		KeyU: 10, KeyJ: 11, KeyK: 12, KeyO: 13, KeyL: 14, KeyP: 15, Semicolon: 16, Quote: 17,
	};

	let octave = $state(4); // Musical Typing's A key = C{octave}
	let sustain = $state(false);
	let volume = $state(0.7);
	let width = $state(800);

	// whole octaves that fit, plus a final C, starting one octave below the typing octave
	const octaves = $derived(Math.max(2, Math.min(4, Math.floor(width / 330))));
	const first = $derived((octave - 1) * 12 + 12); // MIDI of the first C
	const notes = $derived(Array.from({ length: octaves * 12 + 1 }, (_, i) => first + i));
	const whites = $derived(notes.filter((n) => !BLACK.has(n % 12)));

	let ctx: AudioContext | null = null;
	let master: GainNode | null = null;
	const voices = new Map<number, { stop: () => void }>();
	let down = $state<Set<number>>(new Set());

	const VELOCITY = 90;
	let piano: SplendidGrandPiano | null = null; // set once its samples are decoded
	let samples = $state<'loading' | 'ready' | 'failed'>('loading');
	let destroyed = false;

	function engine() {
		if (!ctx) {
			ctx = new AudioContext();
			master = ctx.createGain();
			master.gain.value = volume;
			const comp = ctx.createDynamicsCompressor();
			master.connect(comp).connect(ctx.destination);
			load_samples(ctx, master);
		}
		if (ctx.state === 'suspended') ctx.resume().catch(() => {});
		return ctx;
	}

	async function load_samples(c: AudioContext, out: AudioNode) {
		let fetched = 0;
		try {
			const { SplendidGrandPiano } = await import('smplr');
			const p = SplendidGrandPiano(c, {
				baseUrl: '/api/piano',
				destination: out,
				// only the layer that VELOCITY plays, whole 88-key range
				notesToLoad: { notes: Array.from({ length: 88 }, (_, i) => 21 + i), velocityRange: [VELOCITY, VELOCITY] },
				storage: {
					fetch: async (url: string) => {
						const res = await fetch(url);
						if (res.ok) fetched++;
						return res;
					},
				},
			});
			await p.ready;
			if (destroyed) return p.dispose();
			if (fetched === 0) throw new Error('no samples');
			piano = p;
			samples = 'ready';
		} catch {
			samples = 'failed';
		}
	}

	// start decoding as soon as the window opens; the context resumes on the first note
	onMount(engine);

	$effect(() => {
		if (master) master.gain.value = volume;
	});

	function note_on(midi: number) {
		if (down.has(midi)) return;
		engine();
		voices.get(midi)?.stop();
		if (piano) {
			voices.set(midi, { stop: piano.start({ note: midi, velocity: VELOCITY }) });
		} else {
			synth_on(midi);
		}
		down = new Set(down).add(midi);
	}

	// fallback: a soft, piano-ish tone from a few decaying partials
	function synth_on(midi: number) {
		const c = ctx!;
		const f = 440 * 2 ** ((midi - 69) / 12);
		const now = c.currentTime;
		const out = c.createGain();
		const tone = c.createBiquadFilter();
		tone.type = 'lowpass';
		tone.frequency.setValueAtTime(Math.min(12000, f * 9), now);
		tone.frequency.exponentialRampToValueAtTime(Math.max(400, f * 2), now + 1.6);
		out.connect(tone).connect(master!);
		const decay = Math.max(1.2, 4.5 - (midi - 48) / 14);
		out.gain.setValueAtTime(0, now);
		out.gain.linearRampToValueAtTime(0.32, now + 0.006);
		out.gain.exponentialRampToValueAtTime(0.12, now + 0.25);
		out.gain.exponentialRampToValueAtTime(0.0008, now + decay);
		const oscs = [
			[1, 'triangle', 1],
			[2, 'sine', 0.35],
			[3, 'sine', 0.12],
			[1.002, 'sine', 0.4],
		].map(([mul, type, amp]) => {
			const o = c.createOscillator();
			const g = c.createGain();
			o.type = type as OscillatorType;
			o.frequency.value = f * (mul as number);
			g.gain.value = amp as number;
			o.connect(g).connect(out);
			o.start(now);
			o.stop(now + decay + 0.1);
			return o;
		});
		voices.set(midi, {
			stop: () => {
				const t0 = c.currentTime;
				out.gain.cancelScheduledValues(t0);
				out.gain.setValueAtTime(Math.max(0.0008, out.gain.value), t0);
				out.gain.exponentialRampToValueAtTime(0.0008, t0 + 0.18);
				for (const o of oscs) o.stop(t0 + 0.2);
			},
		});
	}

	function note_off(midi: number) {
		if (!down.has(midi)) return;
		const next = new Set(down);
		next.delete(midi);
		down = next;
		if (!sustain) {
			voices.get(midi)?.stop();
			voices.delete(midi);
		}
	}

	$effect(() => {
		if (!sustain) {
			for (const [m, v] of voices) if (!down.has(m)) (v.stop(), voices.delete(m));
		}
	});

	// pointer: press, then slide across keys for a glissando
	let pointer_note: number | null = null;
	function note_at(x: number, y: number): number | null {
		const el = document.elementFromPoint(x, y) as HTMLElement | null;
		const n = el?.closest<HTMLElement>('[data-midi]')?.dataset.midi;
		return n ? +n : null;
	}
	function on_pointerdown(e: PointerEvent) {
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		pointer_note = note_at(e.clientX, e.clientY);
		if (pointer_note !== null) note_on(pointer_note);
	}
	function on_pointermove(e: PointerEvent) {
		if (pointer_note === null && !(e.buttons & 1)) return;
		const n = note_at(e.clientX, e.clientY);
		if (n === pointer_note) return;
		if (pointer_note !== null) note_off(pointer_note);
		pointer_note = n;
		if (n !== null) note_on(n);
	}
	function on_pointerup() {
		if (pointer_note !== null) note_off(pointer_note);
		pointer_note = null;
	}

	const typing_base = $derived(octave * 12 + 12);
	function on_key(e: KeyboardEvent, is_down: boolean) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		if (is_down && e.code === 'KeyZ') return void (octave = Math.max(1, octave - 1));
		if (is_down && e.code === 'KeyX') return void (octave = Math.min(6, octave + 1));
		if (e.code === 'Tab') {
			e.preventDefault();
			sustain = is_down ? !sustain : sustain;
			return;
		}
		const off = TYPING[e.code];
		if (off === undefined) return;
		e.preventDefault();
		if (e.repeat) return;
		if (is_down) note_on(typing_base + off);
		else note_off(typing_base + off);
	}

	onDestroy(() => {
		destroyed = true;
		for (const v of voices.values()) v.stop();
		piano?.dispose();
		ctx?.close();
	});

	const white_index = (n: number) => whites.indexOf(n);
	const label = (n: number) => (n % 12 === 0 ? `C${Math.floor(n / 12) - 1}` : '');
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	class="piano"
	tabindex="0"
	role="application"
	data-samples={samples}
	aria-label={t('app.piano')}
	onkeydown={(e) => on_key(e, true)}
	onkeyup={(e) => on_key(e, false)}
	onblur={() => [...down].forEach(note_off)}
>
	<Toolbar title={t('app.piano')}>
		{#snippet trailing()}
			<div class="ctl">
				<span class="lbl">{t('piano.octave')}</span>
				<div class="stepper">
					<button aria-label="-" onclick={() => (octave = Math.max(1, octave - 1))}>
						<svg viewBox="0 0 12 12" width="10" height="10"><path d="M2.5 6h7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
					</button>
					<span class="val">C{octave}</span>
					<button aria-label="+" onclick={() => (octave = Math.min(6, octave + 1))}>
						<svg viewBox="0 0 12 12" width="10" height="10"><path d="M2.5 6h7M6 2.5v7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
					</button>
				</div>
			</div>
			<div class="ctl">
				<span class="lbl">{t('piano.sustain')}</span>
				<button class="switch" class:on={sustain} role="switch" aria-checked={sustain} aria-label={t('piano.sustain')} onclick={() => (sustain = !sustain)}><span></span></button>
			</div>
			<div class="ctl">
				<svg viewBox="0 0 20 20" width="15" height="15" aria-hidden="true"><path d="M3 8h3l4-3.4v10.8L6 12H3z" fill="currentColor" /><path d="M13 7.2a4 4 0 010 5.6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
				<input type="range" min="0" max="1" step="0.01" bind:value={volume} style:--p="{volume * 100}%" aria-label={t('piano.volume')} />
			</div>
		{/snippet}
	</Toolbar>

	<div class="stage" bind:clientWidth={width}>
		<div
			class="keys"
			role="group"
			style:--whites={whites.length}
			onpointerdown={on_pointerdown}
			onpointermove={on_pointermove}
			onpointerup={on_pointerup}
			onpointercancel={on_pointerup}
		>
			{#each whites as n (n)}
				<div class="white" class:down={down.has(n)} class:typing={n >= typing_base && n <= typing_base + 17} data-midi={n}>
					<span>{label(n)}</span>
				</div>
			{/each}
			{#each notes.filter((n) => BLACK.has(n % 12)) as n (n)}
				<div
					class="black"
					class:down={down.has(n)}
					data-midi={n}
					style:left="calc({white_index(n - 1) + 1} * 100% / var(--whites))"
					title={NAMES[n % 12]}
				></div>
			{/each}
		</div>
	</div>
</div>

<style>
	.piano {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		outline: none;
		user-select: none;
	}

	.ctl {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-left: 12px;
		font-size: 12px;
		color: var(--zd-text-secondary);

		input {
			width: 90px;
		}
	}

	.stepper {
		display: flex;
		align-items: center;
		height: 24px;
		border-radius: var(--zd-radius-control);
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);

		button {
			width: 24px;
			height: 24px;
			color: var(--zd-text);
		}

		.val {
			min-width: 28px;
			text-align: center;
			font-variant-numeric: tabular-nums;
			color: var(--zd-text);
		}
	}

	.switch {
		position: relative;
		width: 32px;
		height: 18px;
		border-radius: 9px;
		background: var(--zd-hairline-strong);
		transition: background-color 180ms ease;

		span {
			position: absolute;
			top: 2px;
			left: 2px;
			width: 14px;
			height: 14px;
			border-radius: 50%;
			background: #fff;
			box-shadow: 0 0.5px 2px hsla(0, 0%, 0%, 0.3);
			transition: transform 220ms var(--zd-spring);
		}

		&.on {
			background: var(--zd-green);

			span {
				transform: translateX(14px);
			}
		}
	}

	input[type='range'] {
		appearance: none;
		height: 4px;
		border-radius: 2px;
		background: linear-gradient(to right, var(--zd-text-secondary) var(--p), var(--zd-hairline-strong) var(--p));

		&::-webkit-slider-thumb {
			appearance: none;
			width: 14px;
			height: 14px;
			border-radius: 50%;
			background: #fff;
			box-shadow: 0 0.5px 2px hsla(0, 0%, 0%, 0.35);
		}
	}

	@container (max-width: 640px) {
		.lbl,
		.ctl:last-child {
			display: none;
		}
	}

	.stage {
		flex: 1;
		min-height: 0;
		padding: 16px 20px 20px;
		display: flex;
		background: var(--zd-hover);
	}

	.keys {
		position: relative;
		flex: 1;
		display: flex;
		gap: 0;
		touch-action: none;
		border-radius: 8px;
		overflow: hidden;
		box-shadow:
			0 1px 3px hsla(0, 0%, 0%, 0.18),
			0 0 0 0.5px hsla(0, 0%, 0%, 0.2);
	}

	.white {
		flex: 1;
		display: flex;
		align-items: flex-end;
		justify-content: center;
		padding-bottom: 10px;
		background: linear-gradient(#fafafa, #f0f0f0);
		box-shadow: inset -0.5px 0 0 hsla(0, 0%, 0%, 0.18);
		transition: background 60ms ease;

		span {
			font-size: 10px;
			color: hsla(0, 0%, 0%, 0.4);
		}

		&.typing {
			background: linear-gradient(#fdfdfd, #f3f3f3);
		}

		&.down {
			background: linear-gradient(#e2e2e4, #d6d6d8);
		}
	}

	.black {
		position: absolute;
		top: 0;
		width: calc(100% / var(--whites) * 0.58);
		height: 60%;
		transform: translateX(-50%);
		border-radius: 0 0 4px 4px;
		background: linear-gradient(#3a3a3c, #1c1c1e);
		box-shadow:
			inset 0 -3px 0 hsla(0, 0%, 100%, 0.08),
			0 2px 3px hsla(0, 0%, 0%, 0.3);
		transition: background 60ms ease;

		&.down {
			background: linear-gradient(#2a2a2c, #111);
			box-shadow: inset 0 -1px 0 hsla(0, 0%, 100%, 0.06);
		}
	}
</style>
