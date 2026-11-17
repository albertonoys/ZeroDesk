<script lang="ts">
	import type { Snippet } from 'svelte';

	const {
		title,
		subtitle,
		leading,
		trailing,
		inset = 92,
		bordered = true,
	}: {
		title?: string;
		subtitle?: string;
		leading?: Snippet;
		trailing?: Snippet;
		/** room for the traffic lights */
		inset?: number;
		bordered?: boolean;
	} = $props();
</script>

<header class="toolbar app-window-drag-handle" class:bordered style:padding-left="{inset}px">
	{#if leading}<div class="group">{@render leading()}</div>{/if}
	{#if title}
		<div class="titles">
			<span class="title">{title}</span>
			{#if subtitle}<span class="subtitle">{subtitle}</span>{/if}
		</div>
	{/if}
	<span class="spacer"></span>
	{#if trailing}<div class="group">{@render trailing()}</div>{/if}
</header>

<style>
	.toolbar {
		position: relative;
		z-index: 5;
		height: var(--zd-titlebar);
		flex: none;
		display: flex;
		align-items: center;
		gap: 12px;
		padding-right: 12px;
		background: var(--zd-toolbar-bg);
		backdrop-filter: blur(30px) saturate(1.6);

		&.bordered {
			box-shadow: 0 0.5px 0 var(--zd-hairline-strong);
		}
	}

	.group {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.titles {
		display: flex;
		flex-direction: column;
		min-width: 0;
		line-height: 1.2;
	}

	.title {
		font-size: 13px;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.subtitle {
		font-size: 11px;
		color: var(--zd-text-secondary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.spacer {
		flex: 1;
	}
</style>
