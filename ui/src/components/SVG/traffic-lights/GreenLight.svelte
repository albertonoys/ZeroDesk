<script lang="ts">
	import ExpandSvg from './ExpandSVG.svelte';
	import StretchSvg from './StretchSVG.svelte';

	let { expandable }: { expandable: boolean } = $props();
</script>

{#if expandable}
	<ExpandSvg />
{:else}
	<StretchSvg />
{/if}
