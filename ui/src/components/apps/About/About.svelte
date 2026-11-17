<script lang="ts">
	import { onMount } from 'svelte';
	import AppIcon from '🍎/components/SVG/AppIcon.svelte';
	import { format_bytes, format_uptime, t } from '🍎/state/i18n.svelte.ts';
	import { session, type SysInfo } from '🍎/state/session.svelte.ts';
	import Toolbar from '../common/Toolbar.svelte';

	let sys = $state<SysInfo | null>(session.sys);

	async function refresh() {
		try {
			const r = await fetch('/api/sys', { credentials: 'same-origin' });
			if (r.ok) sys = session.sys = await r.json();
		} catch {
			/* keep last */
		}
	}

	onMount(() => {
		refresh();
		const timer = setInterval(refresh, 3000);
		return () => clearInterval(timer);
	});

	const mem_used = $derived(sys ? 1 - sys.memAvailMB / sys.memTotalMB : 0);
	const disk_used = $derived(sys ? 1 - sys.diskFree / sys.diskTotal : 0);
	const batt = $derived(sys?.battery?.capacity ? +sys.battery.capacity : null);
	const batt_status = $derived(
		sys?.battery?.status === 'Charging' ? t('about.charging') : sys?.battery?.status === 'Full' ? t('about.full') : t('about.discharging'),
	);

	// green -> orange -> red as a resource fills up
	const level = (used: number) => (used < 0.7 ? 'var(--zd-green)' : used < 0.88 ? 'var(--zd-orange)' : 'var(--zd-red)');
</script>

<div class="about">
	<Toolbar bordered={false} />
	<div class="content">
		<div class="hero">
			<AppIcon id="device" size={92} />
			<h1>{sys?.hostname ?? 'CardputerZero'}</h1>
			<p>{t('about.model')}</p>
		</div>

		{#if sys}
			<dl class="specs">
				<dt>{t('about.chip')}</dt>
				<dd>{t('about.chip_value')}</dd>
				<dt>{t('about.system')}</dt>
				<dd>{sys.os}</dd>
				<dt>{t('about.network')}</dt>
				<dd>{sys.addrs.map((a) => `${a.iface} ${a.addr}`).join('  ') || '—'}</dd>
				<dt>{t('about.uptime')}</dt>
				<dd>{format_uptime(sys.uptimeSec)}</dd>
				<dt>{t('about.temp')}</dt>
				<dd>{sys.tempC.toFixed(1)} °C</dd>
			</dl>

			<div class="meters">
				<div class="meter">
					<div class="row"><span>{t('about.memory')}</span><span class="v">{t('about.available', { v: `${sys.memAvailMB} MB` })} / {sys.memTotalMB} MB</span></div>
					<span class="bar"><span style:width="{mem_used * 100}%" style:background={level(mem_used)}></span></span>
				</div>
				<div class="meter">
					<div class="row"><span>{t('about.storage')}</span><span class="v">{t('about.available', { v: format_bytes(sys.diskFree) })} / {format_bytes(sys.diskTotal)}</span></div>
					<span class="bar"><span style:width="{disk_used * 100}%" style:background={level(disk_used)}></span></span>
				</div>
				{#if batt !== null}
					<div class="meter">
						<div class="row"><span>{t('about.battery')}</span><span class="v">{batt}% · {batt_status}</span></div>
						<span class="bar"><span style:width="{batt}%" style:background={level(1 - batt / 100)}></span></span>
					</div>
				{/if}
			</div>
		{/if}
	</div>
</div>

<style>
	.about {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}

	.about :global(.toolbar) {
		background: transparent;
		backdrop-filter: none;
	}

	.content {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 0 36px 28px;
	}

	.hero {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;

		h1 {
			margin-top: 14px;
			font-size: 22px;
			font-weight: 700;
			letter-spacing: -0.01em;
		}

		p {
			margin-top: 2px;
			color: var(--zd-text-secondary);
		}
	}

	.specs {
		margin: 22px 0 0;
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 7px 16px;
		font-size: 12px;

		dt {
			text-align: right;
			color: var(--zd-text-secondary);
		}

		dd {
			margin: 0;
			user-select: text;
			font-variant-numeric: tabular-nums;
		}
	}

	.meters {
		margin-top: 22px;
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 14px 16px;
		border-radius: var(--zd-radius-panel);
		background: var(--zd-hover);
	}

	.meter .row {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		font-size: 12px;
		margin-bottom: 6px;

		.v {
			color: var(--zd-text-secondary);
			font-variant-numeric: tabular-nums;
		}
	}

	.bar {
		display: block;
		height: 6px;
		border-radius: 3px;
		background: var(--zd-hover);
		overflow: hidden;

		span {
			display: block;
			height: 100%;
			border-radius: 3px;
			transition:
				width 600ms var(--zd-ease-out),
				background-color 300ms ease;
		}
	}
</style>
