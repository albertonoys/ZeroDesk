<script lang="ts">
	import type { Snippet } from 'svelte';

	const {
		label,
		onclick,
		disabled = false,
		pressed = false,
		children,
		text,
	}: {
		label: string;
		onclick?: (e: MouseEvent) => void;
		disabled?: boolean;
		pressed?: boolean;
		children?: Snippet;
		/** show the label as text next to the icon */
		text?: boolean;
	} = $props();
</script>

<button class="tool" class:pressed class:with-text={text} {disabled} {onclick} aria-label={label} title={label}>
	{@render children?.()}
	{#if text}<span>{label}</span>{/if}
</button>

<style>
	.tool {
		height: 28px;
		min-width: 30px;
		padding: 0 7px;
		gap: 6px;
		border-radius: var(--zd-radius-control);
		color: var(--zd-text-secondary);
		font-size: 12px;
		font-weight: 500;
		white-space: nowrap;
		transition:
			background-color 120ms ease,
			color 120ms ease,
			transform 260ms var(--zd-spring);

		:global(svg) {
			flex: none;
		}

		&.with-text {
			padding: 0 10px 0 8px;
		}

		&:hover:not(:disabled) {
			background: var(--zd-hover);
			color: var(--zd-text);
		}

		&:active:not(:disabled) {
			transform: scale(0.94);
		}

		&.pressed {
			background: var(--zd-selected);
			color: var(--zd-text);
		}

		&:disabled {
			opacity: 0.35;
		}
	}
</style>
