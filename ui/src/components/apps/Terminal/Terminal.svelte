<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import '@xterm/xterm/css/xterm.css';
	import { ws_url } from '🍎/lib/api.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';
	import Toolbar from '../common/Toolbar.svelte';

	let host = $state<HTMLElement>();
	let title = $state(`${session.user}@${session.hostname}`);
	let term: import('@xterm/xterm').Terminal | undefined;
	let ws: WebSocket | undefined;
	let ro: ResizeObserver | undefined;

	const themes = {
		light: { background: '#fafafa', foreground: '#1c1c1e', cursor: '#1c1c1e', selectionBackground: '#1c1c1e22', black: '#1c1c1e', brightBlack: '#6e6a66' },
		dark: { background: '#1e1e20', foreground: '#ececec', cursor: '#ececec', selectionBackground: '#ffffff26', black: '#2b2a29', brightBlack: '#8e8a85' },
	};

	$effect(() => {
		if (term) term.options.theme = themes[preferences.theme.scheme];
	});

	onMount(async () => {
		const [{ Terminal }, { FitAddon }] = await Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit')]);
		term = new Terminal({
			// Terminal programs align text by character columns, so this is the one
			// place that uses a monospaced face.
			fontFamily: '"SF Mono", ui-monospace, Menlo, "Noto Sans Mono CJK SC", monospace',
			fontSize: 12.5,
			lineHeight: 1.25,
			cursorBlink: true,
			allowProposedApi: false,
			scrollback: 2000,
			theme: themes[preferences.theme.scheme],
		});
		const fit = new FitAddon();
		term.loadAddon(fit);
		term.open(host!);
		fit.fit();

		const enc = new TextEncoder();
		ws = new WebSocket(ws_url('/ws/term'));
		ws.binaryType = 'arraybuffer';
		ws.onopen = () => {
			ws!.send(JSON.stringify({ t: 'resize', cols: term!.cols, rows: term!.rows }));
			term!.focus();
		};
		ws.onmessage = (e) => term!.write(new Uint8Array(e.data as ArrayBuffer));
		ws.onclose = () => term!.write('\r\n\x1b[2m[closed]\x1b[0m\r\n');
		term.onData((d) => ws?.readyState === 1 && ws.send(enc.encode(d)));
		term.onTitleChange((s) => (title = s));

		let frame = 0;
		ro = new ResizeObserver(() => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				fit.fit();
				if (ws?.readyState === 1) ws.send(JSON.stringify({ t: 'resize', cols: term!.cols, rows: term!.rows }));
			});
		});
		ro.observe(host!);
	});

	onDestroy(() => {
		ro?.disconnect();
		ws?.close();
		term?.dispose();
	});
</script>

<div class="terminal">
	<Toolbar title={t('app.terminal')} subtitle={title} />
	<div class="host" bind:this={host}></div>
</div>

<style>
	.terminal {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}

	.host {
		flex: 1;
		min-height: 0;
		padding: 8px 4px 4px 12px;
		background: #fafafa;
		transition: background-color 260ms ease;

		:global(.xterm-viewport) {
			scrollbar-width: none;
		}
	}

	:global(body.dark) .host {
		background: #1e1e20;
	}
</style>
