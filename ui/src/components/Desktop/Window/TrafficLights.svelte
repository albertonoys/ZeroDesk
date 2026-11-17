<script lang="ts">
	import CloseIcon from '🍎/components/SVG/traffic-lights/CloseSVG.svelte';
	import GreenLight from '🍎/components/SVG/traffic-lights/GreenLight.svelte';
	import MinimizeSvg from '🍎/components/SVG/traffic-lights/MinimizeSVG.svelte';
	import { apps, config_of, type AppID } from '🍎/state/apps.svelte.ts';

	const {
		app_id,
		on_close_app,
		on_maximize_click,
		on_minimize_app,
	}: {
		app_id: AppID;
		on_close_app: () => void;
		on_maximize_click: () => void;
		on_minimize_app: () => void;
	} = $props();
</script>

<div class="container" class:unfocused={apps.active !== app_id}>
	<button class="close-light" aria-label="close" onclick={(e) => (e.stopPropagation(), on_close_app())}>
		<CloseIcon />
	</button>
	<button class="minimize-light" aria-label="minimize" onclick={(e) => (e.stopPropagation(), on_minimize_app())}>
		<MinimizeSvg />
	</button>
	<button
		class="stretch-light"
		aria-label="zoom"
		disabled={!config_of(app_id).expandable}
		onclick={(e) => (e.stopPropagation(), on_maximize_click())}
	>
		<GreenLight expandable={config_of(app_id).expandable} />
	</button>
</div>

<style>
	.container {
		/* macOS 26: 14pt lights, centres 22.5pt apart */
		--button-size: 14px;

		display: grid;
		grid-template-columns: repeat(3, var(--button-size));
		align-items: center;
		gap: 8.5px;

		&.unfocused button {
			--bgcolor: hsla(0, 0%, 50%, 0.28);
			--border-color: hsla(0, 0%, 30%, 0.12);
		}

		:global(svg) {
			opacity: 0;
			transition: opacity 100ms ease;
		}

		&:hover {
			button:not(:disabled) {
				--bgcolor: var(--on-color);
				--border-color: var(--on-border);
			}

			:global(svg) {
				opacity: 1;
			}
		}
	}

	button {
		height: var(--button-size);
		width: var(--button-size);
		border-radius: 50%;
		background-color: var(--bgcolor);
		box-shadow: inset 0 0 0 0.5px var(--border-color);
		transition: background-color 120ms ease;

		&:active:not(:disabled) {
			filter: brightness(0.85);
		}

		&:disabled {
			--bgcolor: hsla(0, 0%, 50%, 0.28) !important;
			--border-color: hsla(0, 0%, 30%, 0.12) !important;
		}
	}

	.close-light {
		--on-color: #ff5f57;
		--on-border: #e2463f;
		--bgcolor: var(--on-color);
		--border-color: var(--on-border);
	}

	.minimize-light {
		--on-color: #febc2e;
		--on-border: #e1a116;
		--bgcolor: var(--on-color);
		--border-color: var(--on-border);
	}

	.stretch-light {
		--on-color: #28c840;
		--on-border: #14ae2c;
		--bgcolor: var(--on-color);
		--border-color: var(--on-border);

		:global(svg) {
			transform: rotate(90deg);
		}
	}
</style>
