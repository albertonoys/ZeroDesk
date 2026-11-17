<script lang="ts">
	import { elevation } from '🍎/actions';
	import Menu from '🍎/components/TopBar/Menu.svelte';
	import { context_menu, hide_context_menu } from '🍎/state/contextmenu.svelte.ts';
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && hide_context_menu()} onblur={hide_context_menu} />
<svelte:body onpointerdown={(e) => !(e.target as HTMLElement).closest('.ctx-host') && hide_context_menu()} />

{#if context_menu.items}
	<div class="ctx-host" style:transform="translate({context_menu.x}px, {context_menu.y}px)" use:elevation={'context-menu'}>
		<Menu items={context_menu.items} on_pick={hide_context_menu} />
	</div>
{/if}

<style>
	.ctx-host {
		position: fixed;
		top: 0;
		left: 0;
	}
</style>
