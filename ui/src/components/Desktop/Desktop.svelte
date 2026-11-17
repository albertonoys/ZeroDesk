<script lang="ts">
	import { onMount } from 'svelte';
	import { check_session, session } from '🍎/state/session.svelte.ts';
	import Dock from '../Dock/Dock.svelte';
	import Launchpad from '../Launchpad/Launchpad.svelte';
	import Login from '../SystemUI/Login.svelte';
	import TopBar from '../TopBar/TopBar.svelte';
	import ContextMenu from './ContextMenu.svelte';
	import ContextMenuHost from './ContextMenuHost.svelte';
	import Wallpaper from './Wallpaper.svelte';
	import WindowsArea from './Window/WindowsArea.svelte';

	let main_el = $state<HTMLElement>();

	onMount(check_session);
</script>

<div bind:this={main_el} class="container">
	<Wallpaper />
	{#if session.authed}
		<main>
			<TopBar />
			<WindowsArea />
			<Launchpad />
			<Dock />
		</main>
		<ContextMenu target_element={main_el} />
		<ContextMenuHost />
	{:else if session.authed === false}
		<Login />
	{/if}
</div>

<style>
	.container {
		height: 100%;
		width: 100%;
	}

	main {
		height: 100%;
		width: 100%;
		display: grid;
		grid-template-rows: auto 1fr auto;
	}
</style>
