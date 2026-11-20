<script lang="ts">
	import { untrack } from 'svelte';
	import { apps, close_app, open_ids, title_of } from '🍎/state/apps.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { go_home, phone } from '🍎/state/phone.svelte.ts';
	import AppNexus from '../apps/AppNexus.svelte';
	import PhoneHome from './PhoneHome.svelte';

	// iOS on a phone: the home screen, and the app in front full screen. Apps
	// stay open behind the home screen; the round glass button (where the Mac
	// window's traffic lights would be) and a swipe up on the home indicator
	// go back home. Apps lay themselves out for the width with @container rules.

	// an app opened from anywhere (Finder, App Store, a notification) comes to the front
	// (the phone starts on the home screen, even if a window was open before)
	let first = true;
	$effect(() => {
		const id = apps.active;
		if (first) {
			first = false;
			return;
		}
		if (id) untrack(() => (phone.home = false));
	});

	const shown = $derived(!phone.home && apps.active && apps.open[apps.active] ? apps.active : '');
	// keep every open app mounted (state survives going home), show only the front one
	const mounted = $derived(open_ids().filter((id) => apps.open[id] && id !== 'launchpad'));

	let swipe: { y: number } | null = null;
	function swipe_start(e: PointerEvent) {
		swipe = { y: e.clientY };
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function swipe_end(e: PointerEvent) {
		if (swipe && swipe.y - e.clientY > 24) go_home();
		swipe = null;
	}
</script>

<div class="phone">
	<PhoneHome />

	{#each mounted as id (id)}
		<!-- inert: an app behind the home screen must not take taps, even where one
		     of its children sets its own visibility (the Cardputer canvas does) -->
		<section class="window active app" class:front={shown === id} role="application" aria-hidden={shown !== id} inert={shown !== id}>
			<button class="home-btn" aria-label={t('phone.home')} onclick={go_home}>
				<svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true">
					<path d="M12.5 4 6.5 10l6 6" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
				</svg>
			</button>
			<div class="content">
				<AppNexus app_id={id} is_being_dragged={false} />
			</div>
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div class="indicator" onpointerdown={swipe_start} onpointerup={swipe_end} ondblclick={() => close_app(id)} title={title_of(id)}>
				<span></span>
			</div>
		</section>
	{/each}
</div>

<style>
	.phone {
		position: fixed;
		inset: 0;
		overflow: hidden;
	}

	/* iOS 26 home-screen apps size the viewport a top inset short of the screen and clip
	   position: fixed boxes there, leaving a bar along the bottom; 100lvh is the full screen */
	@media (display-mode: standalone) {
		.phone {
			position: absolute;
			bottom: auto;
			height: 100lvh;
		}
	}

	.app {
		position: absolute;
		inset: 0;
		z-index: 5;
		display: flex;
		flex-direction: column;
		padding-top: env(safe-area-inset-top);
		background: var(--zd-window-bg);
		color: var(--zd-text);
		opacity: 0;
		scale: 0.94;
		visibility: hidden;
		transition:
			opacity 200ms ease,
			scale 280ms var(--zd-spring),
			visibility 0s 280ms;

		&.front {
			opacity: 1;
			scale: 1;
			visibility: visible;
			transition:
				opacity 220ms ease,
				scale 340ms var(--zd-spring);
		}
	}

	/* where a Mac window's traffic lights sit: an iOS 26 glass back button */
	.home-btn {
		position: absolute;
		z-index: 20;
		top: calc(env(safe-area-inset-top) + (var(--zd-titlebar) - 36px) / 2);
		left: 14px;
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 50%;
		color: var(--zd-text);
		background: var(--zd-glass);
		backdrop-filter: blur(20px) saturate(1.6);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 2px 8px hsla(0, 0%, 0%, 0.12);
		transition: transform 160ms var(--zd-spring);

		&:active {
			transform: scale(0.9);
		}
	}

	.content {
		flex: 1;
		min-height: 0;
		overflow: hidden;
	}

	.indicator {
		flex: none;
		display: grid;
		place-items: center;
		height: calc(env(safe-area-inset-bottom) + 18px);
		touch-action: none;

		span {
			width: 134px;
			height: 5px;
			border-radius: 3px;
			background: var(--zd-text);
			opacity: 0.35;
		}
	}

	/* opened from the home screen the system draws its own home indicator right below ours */
	@media (display-mode: standalone) {
		.indicator {
			height: env(safe-area-inset-bottom);

			span {
				display: none;
			}
		}
	}
</style>
