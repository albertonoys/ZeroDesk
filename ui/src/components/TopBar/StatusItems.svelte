<script lang="ts">
	import { t } from '🍎/state/i18n.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';

	const battery = $derived.by(() => {
		const b = session.sys?.battery;
		if (!b?.capacity) return null;
		const pct = Math.max(0, Math.min(100, +b.capacity));
		return { pct, charging: b.status === 'Charging' || b.status === 'Full' };
	});

	const ip = $derived(session.sys?.addrs?.[0]?.addr ?? '');
</script>

<div class="status">
	{#if ip}
		<span class="item" title={ip}>
			<svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true">
				<path d="M2.5 8.2a10.6 10.6 0 0 1 15 0" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
				<path d="M5.2 11a6.8 6.8 0 0 1 9.6 0" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
				<circle cx="10" cy="14.3" r="1.7" fill="currentColor" />
			</svg>
		</span>
	{/if}

	{#if battery}
		<span class="item battery" title="{battery.pct}%">
			<span class="pct">{battery.pct}%</span>
			<svg viewBox="0 0 28 14" width="25" height="12" aria-hidden="true">
				<rect x="0.75" y="0.75" width="23" height="12.5" rx="3.6" fill="none" stroke="currentColor" stroke-opacity="0.45" stroke-width="1.2" />
				<rect x="25.4" y="4.6" width="1.8" height="4.8" rx="0.9" fill="currentColor" opacity="0.45" />
				<rect
					x="2.5"
					y="2.5"
					width={Math.max(1.5, 19.5 * (battery.pct / 100))}
					height="9"
					rx="2"
					fill={battery.pct <= 15 && !battery.charging ? 'var(--zd-red)' : battery.charging ? 'var(--zd-green)' : 'currentColor'}
				/>
				{#if battery.charging}
					<path d="M13.6 2.2L9.6 7.6h3l-1.2 4.2 4-5.4h-3z" fill="var(--zd-text)" stroke="var(--zd-window-bg)" stroke-width="0.6" />
				{/if}
			</svg>
		</span>
	{/if}
</div>

<style>
	.status {
		display: flex;
		align-items: center;
		gap: 4px;
		height: 100%;
	}

	.item,
	.icon-btn {
		height: 100%;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 0 7px;
		border-radius: 5px;
	}

	.icon-btn {
		transition: background-color 120ms ease;

		&:hover {
			background: hsla(var(--system-color-light-contrast-hsl), 0.1);
		}

		svg {
			transition: transform 380ms var(--zd-spring);
		}

		&:active svg {
			transform: rotate(-20deg) scale(0.9);
		}
	}

	.pct {
		font-size: 12px;
		font-variant-numeric: tabular-nums;
	}

	.lang {
		position: relative;
		display: grid;
		grid-template-columns: 1fr 1fr;
		height: 20px;
		margin-right: 4px;
		padding: 2px;
		border-radius: 10px;
		background: hsla(var(--system-color-light-contrast-hsl), 0.1);
		font-size: 11px;
		font-weight: 600;

		.opt {
			position: relative;
			z-index: 1;
			min-width: 26px;
			padding: 0 6px;
			line-height: 16px;
			text-align: center;
			opacity: 0.55;
			transition: opacity 200ms ease;

			&.on {
				opacity: 1;
			}
		}

		.indicator {
			position: absolute;
			top: 2px;
			left: 2px;
			width: calc(50% - 2px);
			height: 16px;
			border-radius: 8px;
			background: var(--zd-control-bg);
			box-shadow: var(--zd-control-shadow);
			transition: transform 420ms var(--zd-spring);

			&.right {
				transform: translateX(100%);
			}
		}
	}
</style>
