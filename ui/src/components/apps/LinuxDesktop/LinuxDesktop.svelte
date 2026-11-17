<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { ws_url } from '🍎/lib/api.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import Toolbar from '../common/Toolbar.svelte';
	import ToolButton from '../common/ToolButton.svelte';

	let host = $state<HTMLElement>();
	let conn = $state<'connecting' | 'live' | 'failed'>('connecting');
	let screen_size = $state('');
	// noVNC's RFB class (loaded lazily so the rest of the desktop stays light)
	let rfb: any;

	async function connect() {
		conn = 'connecting';
		const { default: RFB } = await import('@novnc/novnc');
		if (!host) return;
		host.replaceChildren();
		rfb = new RFB(host, ws_url('/ws/vnc'), { wsProtocols: ['binary'] });
		rfb.scaleViewport = true;
		rfb.resizeSession = false;
		rfb.background = 'transparent';
		// Cheap for the device: light compression, decent JPEG quality.
		rfb.compressionLevel = 1;
		rfb.qualityLevel = 7;
		rfb.showDotCursor = false;
		rfb.addEventListener('connect', () => {
			conn = 'live';
			rfb.focus();
			// the framebuffer size arrives with the first update
			const read_size = (tries = 0) => {
				const c = host?.querySelector('canvas');
				if (c && c.width > 1) screen_size = `${c.width} × ${c.height}`;
				else if (tries < 20) setTimeout(() => read_size(tries + 1), 150);
			};
			read_size();
		});
		rfb.addEventListener('disconnect', () => (conn = 'failed'));
	}

	onMount(connect);
	onDestroy(() => {
		try {
			rfb?.disconnect();
		} catch {
			/* already closed */
		}
	});
</script>

<div class="desktop">
	<Toolbar title={t('app.desktop')} subtitle={conn === 'live' ? screen_size : t('desktop.connecting')}>
		{#snippet trailing()}
			{#if conn === 'failed'}
				<ToolButton label={t('desktop.retry')} onclick={connect} text>
					<svg viewBox="0 0 20 20" width="15" height="15" aria-hidden="true">
						<path d="M15.5 10a5.5 5.5 0 1 1-1.6-3.9M15.5 3.5v3.2h-3.2" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
				</ToolButton>
			{/if}
		{/snippet}
	</Toolbar>

	<div class="viewport">
		<div class="host" class:hidden={conn !== 'live'} bind:this={host}></div>
		{#if conn !== 'live'}
			<div class="status">
				{#if conn === 'connecting'}
					<span class="spinner"></span>
					<span>{t('desktop.connecting')}</span>
				{:else}
					<span>{t('desktop.failed')}</span>
				{/if}
			</div>
		{/if}
	</div>
</div>

<style>
	.desktop {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}

	.viewport {
		position: relative;
		flex: 1;
		min-height: 0;
		background: #111;
	}

	.host {
		position: absolute;
		inset: 0;
		transition: opacity 400ms ease;

		&.hidden {
			opacity: 0;
		}

		:global(div) {
			background: transparent !important;
		}
	}

	.status {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 10px;
		color: hsla(0, 0%, 100%, 0.7);
		font-size: 13px;
	}

	.spinner {
		width: 14px;
		height: 14px;
		border-radius: 50%;
		border: 1.6px solid hsla(0, 0%, 100%, 0.2);
		border-top-color: hsla(0, 0%, 100%, 0.85);
		animation: spin 0.9s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>
