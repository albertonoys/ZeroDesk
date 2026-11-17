<script lang="ts">
	// SF Symbols-like glyph from framework7-icons (ligature font), sized in px.
	import 'framework7-icons/css/framework7-icons.css';

	let { name, size = 16 }: { name: string; size?: number } = $props();
</script>

<i class="f7-icons" style:font-size="{size}px" aria-hidden="true">{name}</i>

<style>
	i {
		flex: none;
		width: 1em;
		height: 1em;
		text-align: center;
	}
</style>
