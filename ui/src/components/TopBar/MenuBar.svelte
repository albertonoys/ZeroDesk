<script lang="ts">
	import { click_outside, elevation, focus_outside } from '🍎/actions';
	import { build_menus } from '🍎/lib/menus.ts';
	import { menubar_state } from '🍎/state/menubar.svelte';
	import Menu from './Menu.svelte';

	const menus = $derived(build_menus());
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && (menubar_state.active = '')} />

<div
	class="container"
	use:click_outside={() => (menubar_state.active = '')}
	use:focus_outside={() => (menubar_state.active = '')}
>
	{#each menus as menu (menu.id)}
		<div class="slot">
			<button
				class="menu-button"
				class:emphasis={menu.emphasis}
				class:logo={menu.logo}
				class:open={menubar_state.active === menu.id}
				onclick={() => (menubar_state.active = menubar_state.active === menu.id ? '' : menu.id)}
				onmouseover={() => menubar_state.active && (menubar_state.active = menu.id)}
				onfocus={() => menubar_state.active && (menubar_state.active = menu.id)}
			>
				{#if menu.logo}
					<svg viewBox="0 0 20 20" width="15" height="15" aria-label="ZeroDesk">
						<rect x="2" y="3" width="16" height="14" rx="3.5" fill="none" stroke="currentColor" stroke-width="1.6" />
						<rect x="5" y="6" width="10" height="5" rx="1.2" fill="currentColor" />
						<path d="M5.5 14h9" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
					</svg>
				{:else}
					{menu.title}
				{/if}
			</button>

			{#if menubar_state.active === menu.id}
				<div class="menu-parent" use:elevation={'menubar-menu-parent'}>
					<Menu items={menu.items} />
				</div>
			{/if}
		</div>
	{/each}
</div>

<style>
	.container {
		height: 100%;
		display: flex;
		position: relative;
		padding-left: 8px;
	}

	.slot {
		height: 100%;
		position: relative;
	}

	.menu-parent {
		position: absolute;
		top: calc(100% + 2px);
		left: 0;
	}

	.menu-button {
		height: 100%;
		padding: 0 9px;
		border-radius: 5px;
		font-size: 13px;
		font-weight: 500;
		white-space: nowrap;
		transition: background-color 120ms ease;

		&.emphasis {
			font-weight: 700;
		}

		&.logo {
			padding: 0 11px;
		}

		&.open {
			background: hsla(var(--system-color-light-contrast-hsl), 0.12);
		}
	}
</style>
