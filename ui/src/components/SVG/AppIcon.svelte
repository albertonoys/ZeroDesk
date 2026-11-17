<script lang="ts">
	import type { AppID } from '🍎/state/apps.svelte.ts';
	import { ICON_TILE } from '🍎/lib/icon-shape.ts';
	import { icon_look } from '🍎/state/preferences.svelte.ts';

	const { id, size = 56 }: { id: AppID; size?: number } = $props();

	// Artwork: generated in macOS 26/27 Liquid Glass style (design/icon-gen,
	// built by ui/scripts/build-icons.py), full-bleed squares with a light and a
	// dark version. The rounded tile is the shape fitted from a real macOS icon
	// (lib/icon-shape.ts). The Cardputer icon is the hand-drawn hardware.
	// set to true once design/icon-gen/cardputer-*.png is approved and built
	const HAS_CARDPUTER_ART = true;
	const ART: Record<string, string> = {
		device: 'cardputer',
		finder: 'files',
		terminal: 'terminal',
		settings: 'settings',
		launchpad: 'apps',
		about: 'about',
		desktop: 'raspberry',
		// native rewrites of official apps use the redesign of their own Cardputer icon
		music: 'off-music',
		piano: 'off-piano',
		recorder: 'off-recorder',
	};
	const mode = $derived(icon_look());
	const src = $derived(`/icons/${ART[id] ?? 'settings'}-${mode === 'dark' ? 'dark' : 'light'}.webp`);
	const uid = $derived(`ic-${id}-${Math.round(size * 10)}`);
	// the tile's bounding box on the 100-unit canvas
	const T0 = 9.88;
	const TS = 80.24;

	const DEV_BODY = 'M27 9h46c10 0 18 8 18 18v46c0 10-8 18-18 18H27C17 91 9 83 9 73V27C9 17 17 9 27 9z';
</script>

{#if id === 'device' && !HAS_CARDPUTER_ART}
	<!-- the CardputerZero itself, front view, in its own colours (the user's chosen design) -->
	<svg viewBox="0 0 100 100" width={size} height={size} role="img" aria-hidden="true" class="app-icon">
		<defs>
			<filter id="{uid}-dev" x="-20%" y="-20%" width="140%" height="150%">
				<feDropShadow dx="0" dy="1.4" stdDeviation="1.6" flood-color="#000" flood-opacity="0.28" />
			</filter>
		</defs>
		<g filter="url(#{uid}-dev)">
			<path d={DEV_BODY} fill="#c9cacc" />
			<path d={DEV_BODY} fill="none" stroke="#a7a8ab" stroke-width="1" />
			<rect x="15" y="17" width="14" height="27" rx="3" fill="#e8e8ea" />
			<rect x="71" y="17" width="14" height="27" rx="3" fill="#e8e8ea" />
			<rect x="74" y="20" width="5" height="7" rx="1.2" fill="#d33a32" />
			<rect x="18" y="21" width="8" height="2" rx="1" fill="#1c1c1e" opacity="0.55" />
			<rect x="18" y="25" width="8" height="3.2" rx="1" fill="#1c1c1e" opacity="0.85" />
			<rect x="31" y="15" width="38" height="30" rx="2.6" fill="#0e0e10" />
			<rect x="44" y="21" width="12" height="12" rx="3" fill="#ef6a3a" />
			<rect x="35" y="23" width="6" height="8" rx="2" fill="#30c060" />
			<rect x="59" y="23" width="6" height="8" rx="2" fill="#f2a93b" />
			<rect x="45" y="37" width="10" height="2" rx="1" fill="#e8e8ea" opacity="0.6" />
			{#each [0, 1, 2] as row}
				<rect x="15" y={49 + row * 12} width="70" height="4" rx="2" fill="#e8e8ea" />
				{#each [0, 1, 2, 3, 4, 5, 6] as col}
					<rect x={15.5 + col * 10} y={53.5 + row * 12} width="8" height="5.2" rx="2.6" fill="#25262a" />
				{/each}
			{/each}
		</g>
	</svg>
{:else}
	<svg viewBox="0 0 100 100" width={size} height={size} role="img" aria-hidden="true" class="app-icon">
		<defs>
			<clipPath id="{uid}-clip"><path d={ICON_TILE} /></clipPath>
			<filter id="{uid}-tile" x="-15%" y="-15%" width="130%" height="135%">
				<feDropShadow dx="0" dy="1" stdDeviation="1.3" flood-color="#000" flood-opacity="0.3" />
			</filter>
			<!-- specular rim only on the diagonal corners, like macOS 26/27 icons:
			     brightest top-left, nothing along the middle, a faint glint bottom-right -->
			<linearGradient id="{uid}-rim" x1="0" y1="0" x2="1" y2="1">
				<stop offset="0" stop-color="#fff" stop-opacity={mode === 'dark' ? 0.45 : 0.85} />
				<stop offset="0.28" stop-color="#fff" stop-opacity="0" />
				<stop offset="0.72" stop-color="#fff" stop-opacity="0" />
				<stop offset="1" stop-color="#fff" stop-opacity={mode === 'dark' ? 0.22 : 0.4} />
			</linearGradient>
		</defs>
		<g filter="url(#{uid}-tile)">
			<image href={src} x={T0} y={T0} width={TS} height={TS} preserveAspectRatio="xMidYMid slice" clip-path="url(#{uid}-clip)" />
		</g>
		<path d={ICON_TILE} fill="none" stroke="url(#{uid}-rim)" stroke-width="1.1" />
	</svg>
{/if}

<style>
	.app-icon {
		display: block;
		overflow: visible;
		user-select: none;
		-webkit-user-drag: none;
	}
</style>
