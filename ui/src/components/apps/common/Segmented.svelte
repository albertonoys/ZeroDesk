<script lang="ts" generics="T extends string | number">
	import { tick } from 'svelte';

	const {
		options,
		value,
		onchange,
		size = 'small',
	}: {
		options: { value: T; label: string }[];
		value: T;
		onchange: (v: T) => void;
		size?: 'small' | 'regular';
	} = $props();

	let el = $state<HTMLElement>();
	let ind = $state({ x: 0, w: 0 });
	let ready = $state(false);

	// The highlight moves *and* resizes to the selected option (options differ in width).
	$effect(() => {
		value;
		options;
		tick().then(() => {
			const btn = el?.querySelector<HTMLElement>(`[data-v="${CSS.escape(String(value))}"]`);
			if (btn) ind = { x: btn.offsetLeft, w: btn.offsetWidth };
			requestAnimationFrame(() => (ready = true));
		});
	});
</script>

<div class="seg {size}" bind:this={el} role="radiogroup">
	<span class="ind" class:ready style:transform="translateX({ind.x}px)" style:width="{ind.w}px"></span>
	{#each options as opt (opt.value)}
		<button
			role="radio"
			aria-checked={opt.value === value}
			data-v={opt.value}
			class:on={opt.value === value}
			onclick={() => onchange(opt.value)}>{opt.label}</button
		>
	{/each}
</div>

<style>
	.seg {
		position: relative;
		display: inline-flex;
		padding: 2px;
		border-radius: 8px;
		background: var(--zd-hover);
		box-shadow: inset 0 0 0 0.5px var(--zd-hairline);

		button {
			position: relative;
			z-index: 1;
			height: 22px;
			padding: 0 11px;
			border-radius: 6px;
			font-size: 12px;
			font-weight: 500;
			color: var(--zd-text-secondary);
			white-space: nowrap;
			transition: color 180ms ease;

			&.on {
				color: var(--zd-text);
			}
		}

		&.regular button {
			height: 26px;
			padding: 0 14px;
			font-size: 13px;
		}
	}

	.ind {
		position: absolute;
		top: 2px;
		left: 0;
		bottom: 2px;
		border-radius: 6px;
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);

		&.ready {
			transition:
				transform 420ms var(--zd-spring),
				width 420ms var(--zd-spring);
		}
	}
</style>
