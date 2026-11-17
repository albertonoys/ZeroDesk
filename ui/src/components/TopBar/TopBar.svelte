<script lang="ts">
	import MenuBar from './MenuBar.svelte';
	import StatusItems from './StatusItems.svelte';
	import TopBarTime from './TopBarTime.svelte';
</script>

<header>
	<MenuBar />
	<span class="spacer"></span>
	<StatusItems />
	<span class="clock"><TopBarTime /></span>
</header>

<style>
	header {
		position: relative;
		z-index: 90;
		display: flex;
		align-items: center;
		width: 100%;
		height: 28px;
		padding: 0 4px 0 0;
		color: var(--system-color-light-contrast);
		font-size: 13px;
		font-weight: 500;

		&::before {
			content: '';
			position: absolute;
			inset: 0;
			z-index: -1;
			background: hsla(var(--system-color-light-hsl), 0.42);
			backdrop-filter: blur(30px) saturate(1.8);
		}
	}

	.spacer {
		flex: 1 1 auto;
	}

	.clock {
		padding: 0 10px 0 6px;
	}
</style>
