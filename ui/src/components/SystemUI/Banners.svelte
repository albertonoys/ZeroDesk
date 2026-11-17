<script lang="ts">
	import { fly } from 'svelte/transition';
	import { open_app, title_of } from '🍎/state/apps.svelte.ts';
	import { banners, dismiss } from '🍎/state/notifications.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import AppIcon from '../SVG/AppIcon.svelte';
</script>

<!-- macOS notification banners: top-right under the menu bar, newest on top -->
<div class="banners" aria-live="polite">
	{#each banners as b (b.id)}
		<button
			class="banner"
			transition:fly={{ x: preferences.reduced_motion ? 0 : 380, duration: 320, opacity: 1 }}
			onclick={() => (dismiss(b.id), open_app(b.app))}
		>
			<AppIcon id={b.app} size={38} />
			<span class="text">
				<span class="head"><b>{b.title || title_of(b.app)}</b></span>
				{#if b.body}<span class="body">{b.body}</span>{/if}
			</span>
		</button>
	{/each}
</div>

<style>
	.banners {
		position: fixed;
		top: 36px;
		right: 12px;
		z-index: 9000;
		display: flex;
		flex-direction: column;
		gap: 8px;
		width: min(356px, calc(100vw - 24px));
		pointer-events: none;
	}

	.banner {
		pointer-events: auto;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 12px 14px 12px 10px;
		border-radius: 22px;
		text-align: left;
		color: var(--zd-text);
		background: var(--zd-glass);
		backdrop-filter: blur(40px) saturate(1.8);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 8px 28px hsla(0, 0%, 0%, 0.18),
			0 0 0 0.5px hsla(0, 0%, 0%, 0.08);
	}

	.text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		font-size: 13px;
		line-height: 1.3;
	}

	.head b {
		font-weight: 600;
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.body {
		color: var(--zd-text-secondary);
		display: -webkit-box;
		-webkit-line-clamp: 3;
		line-clamp: 3;
		-webkit-box-orient: vertical;
		overflow: hidden;
		overflow-wrap: anywhere;
	}
</style>
