<script lang="ts">
	import { t } from '🍎/state/i18n.svelte.ts';
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import ToolButton from './ToolButton.svelte';

	// Shared toolbar buttons for any live screen: on-screen keyboard,
	// smooth/pixel scaling and screenshot.
	let {
		show_keys = $bindable(false),
		can_capture,
		on_screenshot,
	}: { show_keys?: boolean; can_capture: boolean; on_screenshot: () => void } = $props();
</script>

<ToolButton label={t('device.keyboard')} pressed={show_keys} onclick={() => (show_keys = !show_keys)}>
	<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true">
		<rect x="2" y="5" width="16" height="10.5" rx="2.2" fill="none" stroke="currentColor" stroke-width="1.4" />
		{#each [0, 1, 2, 3, 4] as i}<rect x={4.6 + i * 2.4} y="7.6" width="1.4" height="1.4" rx="0.4" fill="currentColor" />{/each}
		{#each [0, 1, 2, 3] as i}<rect x={5.8 + i * 2.4} y="10" width="1.4" height="1.4" rx="0.4" fill="currentColor" />{/each}
		<rect x="7" y="12.4" width="6" height="1.3" rx="0.65" fill="currentColor" />
	</svg>
</ToolButton>
<ToolButton
	label={preferences.screen_smooth ? t('device.smooth') : t('device.pixel')}
	pressed={!preferences.screen_smooth}
	onclick={() => (preferences.screen_smooth = !preferences.screen_smooth)}
>
	{#if preferences.screen_smooth}
		<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true">
			<path d="M4 13.5c2.2-5.5 4.2-8 6-8s3.8 2.5 6 8" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
		</svg>
	{:else}
		<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true">
			<path d="M4 14v-3h3V8h3V5h3v3h3v3h-3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
		</svg>
	{/if}
</ToolButton>
<ToolButton label={t('device.screenshot')} onclick={on_screenshot} disabled={!can_capture}>
	<svg viewBox="0 0 20 20" width="17" height="17" aria-hidden="true">
		<path d="M3 7.2c0-1 .8-1.8 1.8-1.8h1.6l1.2-1.6h4.8l1.2 1.6h1.6c1 0 1.8.8 1.8 1.8v6.6c0 1-.8 1.8-1.8 1.8H4.8c-1 0-1.8-.8-1.8-1.8z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" />
		<circle cx="10" cy="10.4" r="2.8" fill="none" stroke="currentColor" stroke-width="1.4" />
	</svg>
</ToolButton>
