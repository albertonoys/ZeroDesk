<script lang="ts">
	import type { MenuItem } from '🍎/lib/menus.ts';
	import { menubar_state } from '🍎/state/menubar.svelte.ts';

	const { items, on_pick }: { items: MenuItem[]; on_pick?: () => void } = $props();

	function run(item: MenuItem) {
		if (item.disabled) return;
		menubar_state.active = '';
		on_pick?.();
		item.action?.();
	}
</script>

<section class="menu" role="menu">
	{#each items as item}
		<button class="item" role="menuitem" disabled={item.disabled} onclick={() => run(item)}>
			<span class="label">{item.label}</span>
			{#if item.shortcut}<span class="shortcut">{item.shortcut}</span>{/if}
		</button>
		{#if item.break_after}
			<div class="divider"></div>
		{/if}
	{/each}
</section>

<style>
	.menu {
		min-width: 220px;
		width: max-content;
		padding: 5px;
		border-radius: var(--zd-radius-panel);
		background: var(--zd-glass);
		backdrop-filter: blur(40px) saturate(1.8);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 0 0 0.5px hsla(0, 0%, 0%, 0.12),
			0 10px 30px hsla(0, 0%, 0%, 0.18);
		animation: menu-in 160ms var(--zd-ease-out);
		transform-origin: top left;
	}

	.item {
		display: flex;
		justify-content: space-between;
		gap: 28px;
		width: 100%;
		height: 24px;
		padding: 0 10px;
		border-radius: var(--zd-radius-control);
		font-size: 13px;
		color: var(--zd-text);
		white-space: nowrap;

		&:disabled {
			color: var(--zd-text-tertiary);
		}

		&:not(:disabled):hover {
			background: var(--system-color-primary);
			color: var(--system-color-primary-contrast);

			.shortcut {
				color: inherit;
				opacity: 0.8;
			}
		}
	}

	.shortcut {
		color: var(--zd-text-tertiary);
		letter-spacing: 0.06em;
	}

	.divider {
		height: 1px;
		margin: 5px 10px;
		background: var(--zd-hairline-strong);
	}

	@keyframes menu-in {
		from {
			opacity: 0;
			transform: translateY(-4px) scale(0.98);
		}
	}
</style>
