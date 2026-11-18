<script lang="ts">
	import { preferences } from '🍎/state/preferences.svelte.ts';
	import { create_interval } from '🍎/state/interval.svelte';

	const time = create_interval(1000);

	const label = $derived.by(() => {
		const d = new Date(time.value);
		const zh = preferences.lang === 'zh';
		const weekday = d.toLocaleDateString(zh ? 'zh-CN' : 'en-US', { weekday: 'short' });
		const date = zh
			? `${d.getMonth() + 1}月${d.getDate()}日 ${weekday}`
			: `${weekday} ${d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}`;
		const clock = d.toLocaleTimeString(zh ? 'zh-CN' : 'en-US', { hour: 'numeric', minute: '2-digit' });
		return { date, clock };
	});
</script>

<!-- a phone-width window shows the clock only -->
<span class="time"><span class="date">{label.date}</span>{label.clock}</span>

<style>
	.time {
		font-variant-numeric: tabular-nums;
		white-space: pre;
	}

	.date {
		margin-right: 0.55em;
	}

	@media (max-width: 520px) {
		.date {
			display: none;
		}
	}
</style>
