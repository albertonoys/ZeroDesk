<script lang="ts">
	import type { Component } from 'svelte';
	import { device_windows, is_device_window, is_web_window, type AppID, type BuiltinID } from '🍎/state/apps.svelte';
	import { app_loaders, loaded } from '🍎/state/app_preload.ts';

	const { app_id, is_being_dragged }: { app_id: AppID; is_being_dragged: boolean } = $props();

	// Each app is its own chunk, loaded the first time its window opens (and
	// usually preloaded long before: state/app_preload.ts).
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const loaders = app_loaders as unknown as Partial<Record<BuiltinID, () => Promise<{ default: Component<any> }>>>;
	const key = $derived(is_web_window(app_id) ? 'web' : is_device_window(app_id) ? 'dev' : app_id);
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const ready = $derived(loaded[key]?.default as any);
</script>

{#if ready && is_web_window(app_id)}
	{@const C = ready}
	<div class="app-root"><C window_id={app_id} {is_being_dragged} /></div>
{:else if ready && is_device_window(app_id)}
	{@const C = ready}
	<div class="app-root"><C window_id={app_id} app={device_windows[app_id]} /></div>
{:else if ready}
	{@const C = ready}
	<div class="app-root"><C /></div>
{:else if is_web_window(app_id)}
	{#await app_loaders.web() then { default: WebApp }}
		<div class="app-root"><WebApp window_id={app_id} {is_being_dragged} /></div>
	{/await}
{:else if is_device_window(app_id)}
	{#await app_loaders.dev() then { default: DeviceApp }}
		<div class="app-root"><DeviceApp window_id={app_id} app={device_windows[app_id]} /></div>
	{/await}
{:else}
	{#await loaders[app_id as BuiltinID]!() then { default: App }}
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
