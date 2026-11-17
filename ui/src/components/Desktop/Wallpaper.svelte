<script lang="ts">
	import { elevation } from '🍎/actions';
	import { session } from '🍎/state/session.svelte.ts';

	// The desktop uses the device's own Linux desktop wallpaper, served by the
	// agent; until it has loaded (or if there is none) a plain warm tone shows.
	let loaded = $state(false);
	const src = '/api/wallpaper';
</script>

{#if session.authed}
	<img {src} alt="" aria-hidden="true" class="probe" onload={() => (loaded = true)} />
{/if}

<div
	class="wallpaper"
	class:loaded
	style:background-image={loaded ? `url(${src})` : 'none'}
	use:elevation={'wallpaper'}
></div>
<div class="dim" use:elevation={'wallpaper'}></div>

<style>
	.probe {
		display: none;
	}

	.wallpaper,
	.dim {
		position: fixed;
		inset: 0;
	}

	.wallpaper {
		background-color: var(--zd-wallpaper);
		background-size: cover;
		background-position: center;
		opacity: 1;
		transition: background-color 260ms ease;

		&.loaded {
			animation: wallpaper-in 600ms var(--zd-ease-out);
		}
	}

	.dim {
		pointer-events: none;
		background: hsla(0, 0%, 6%, 0);
		transition: background-color 260ms ease;
	}

	:global(body.dark) .dim {
		background: hsla(0, 0%, 6%, 0.38);
	}

	@keyframes wallpaper-in {
		from {
			opacity: 0;
			transform: scale(1.02);
		}
		to {
			opacity: 1;
			transform: scale(1);
		}
	}
</style>
