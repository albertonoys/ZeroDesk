<script lang="ts">
	import { device_windows, is_device_window, type AppID, type BuiltinID } from '🍎/state/apps.svelte';

	const { app_id }: { app_id: AppID; is_being_dragged: boolean } = $props();

	// Each app is its own chunk, loaded the first time its window opens.
	const loaders = {
		device: () => import('./Device/Device.svelte'),
		desktop: () => import('./LinuxDesktop/LinuxDesktop.svelte'),
		finder: () => import('./Finder/Finder.svelte'),
		terminal: () => import('./Terminal/Terminal.svelte'),
		about: () => import('./About/About.svelte'),
		settings: () => import('./Settings/Settings.svelte'),
		music: () => import('./Music/Music.svelte'),
		piano: () => import('./Piano/Piano.svelte'),
		recorder: () => import('./Recorder/Recorder.svelte'),
	} satisfies Partial<Record<BuiltinID, () => Promise<{ default: unknown }>>>;
</script>

{#if is_device_window(app_id)}
	{#await import('./DeviceApp/DeviceApp.svelte') then { default: DeviceApp }}
		<div class="app-root"><DeviceApp window_id={app_id} app={device_windows[app_id]} /></div>
	{/await}
{:else}
	{#await loaders[app_id as keyof typeof loaders]() then { default: App }}
		<div class="app-root"><App /></div>
	{/await}
{/if}

<style>
	.app-root {
		/* apps adapt to their window's width with @container rules */
		container-type: inline-size;
		height: 100%;
		min-height: 0;
		animation: content-in 220ms ease;
	}

	@keyframes content-in {
		from {
			opacity: 0;
		}
	}
</style>
