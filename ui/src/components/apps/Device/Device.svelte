<script lang="ts">
	import { onMount } from 'svelte';
	import { api, post } from '🍎/lib/api.ts';
	import { download_blob, stamp } from '🍎/lib/download.ts';
	import type { ScreenStatus } from '🍎/lib/screen.ts';
	import { apps } from '🍎/state/apps.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import ScreenTools from '../common/ScreenTools.svelte';
	import ScreenView from '../common/ScreenView.svelte';
	import Toolbar from '../common/Toolbar.svelte';
	import ToolButton from '../common/ToolButton.svelte';

	// Mirror of the Zero's own LCD: what you see here is what the device shows.
	let view = $state<ScreenView>();
	let status = $state<ScreenStatus>('connecting');
	let show_keys = $state(false);
	let foreground = $state('');
	let on_home = $state(false);
	let app_names: Record<string, string> = {};

	type Fg = { launcher: boolean; appId?: string };

	async function poll_foreground() {
		if (document.visibilityState !== 'visible' || apps.minimized.device) return;
		try {
			const fg = await api<Fg>('/api/apps/foreground');
			on_home = fg.launcher;
			// CLI, Python, Settings and the games are drawn by the launcher itself,
			// so it is "in front" for them too; only the frame shows its home screen
			foreground = fg.launcher ? (view?.shows_home() ? t('device.home') : '') : (fg.appId && app_names[fg.appId]) || '';
		} catch {
			/* ignore */
		}
	}

	onMount(() => {
		api<{ id: string; name: string }[]>('/api/apps')
			.then((list) => {
				app_names = Object.fromEntries(list.map((a) => [a.id, a.name]));
				poll_foreground();
			})
			.catch(() => {});
		const timer = setInterval(poll_foreground, 3000);
		return () => clearInterval(timer);
	});

	async function screenshot() {
		const blob = await view?.snapshot();
		if (blob) download_blob(blob, `cardputer-${stamp()}.png`);
	}

	async function go_home() {
		// a page inside the launcher: no app to stop, hold ESC as on the device
		if (on_home && !view?.shows_home()) {
			view?.hold(1, 3600);
			setTimeout(poll_foreground, 4200);
			return;
		}
		try {
			await post('/api/apps/home', {});
		} catch {
			/* ignore */
		}
	}

	const subtitle = $derived(
		status === 'live'
			? foreground
			: status === 'nosignal'
				? t('device.nosignal')
				: status === 'offline'
					? t('device.offline')
					: t('device.connecting'),
	);
	// narrow window: icon-only buttons so the title keeps its room
	let win_w = $state(800);
</script>

<div class="device" bind:clientWidth={win_w}>
	<Toolbar title={t('app.device')} {subtitle}>
		{#snippet trailing()}
			<ScreenTools bind:show_keys can_capture={status === 'live'} on_screenshot={screenshot} />
			<ToolButton label={t('device.home')} onclick={go_home} disabled={status !== 'live'} text={win_w >= 560}>
				<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true">
					<path d="M3.5 9.2L10 3.8l6.5 5.4M5.5 8v7.2h3.4v-4.2h2.2v4.2h3.4V8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
				</svg>
			</ToolButton>
		{/snippet}
	</Toolbar>
	<ScreenView
		bind:this={view}
		path="/ws/screen"
		{show_keys}
		bind:status
		layout={on_home ? 'launcher' : null}
		on_input={() => setTimeout(poll_foreground, 900)}
	/>
</div>

<style>
	.device {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}
</style>
