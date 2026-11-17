<script lang="ts">
	import { untrack } from 'svelte';
	import { apps, open_ids, type AppID } from '🍎/state/apps.svelte';
	import Window from './Window.svelte';
	import { launchpad } from '🍎/state/launchpad.svelte.ts';

	$effect(() => {
		apps.active;
		untrack(() => (apps.active_z_index += 2));
	});

	// Keep all window z-indices low so they never cover the menu bar or dock.
	$effect(() => {
		if (!Object.values(apps.z_indices).some((z) => z > 50)) return;
		const lowest = Math.min(...[...new Set(Object.values(apps.z_indices))].filter((v) => v !== 0));
		untrack(() => (apps.active_z_index -= lowest));
		for (const app of Object.keys(apps.z_indices) as AppID[]) {
			if (apps.z_indices[app] >= lowest) untrack(() => (apps.z_indices[app] -= lowest));
		}
	});
</script>

<!-- like Launchpad on macOS, windows step aside while the Apps layer is up -->
<section id="windows-area" class:hidden={launchpad.open && !launchpad.dragging}>
	{#each open_ids() as app_id (app_id)}
		{#if apps.open[app_id]}
			<Window {app_id} />
		{/if}
	{/each}
</section>

<style>
	section {
		position: fixed;
		inset: 0;
		pointer-events: none;
		transition:
			opacity 220ms ease,
			transform 260ms var(--zd-ease-out, ease-out);

		&.hidden {
			opacity: 0;
			transform: scale(1.04);
			visibility: hidden;
			transition:
				opacity 180ms ease,
				transform 220ms ease,
				visibility 0s 220ms;
		}

		& > :global(*) {
			pointer-events: auto;
		}
	}
</style>
