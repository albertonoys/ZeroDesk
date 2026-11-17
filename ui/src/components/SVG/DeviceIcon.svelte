<script lang="ts" module>
	/** Official CardputerZero apps that have a macOS redesign of their own icon (design/icon-gen/off-*). */
	export const OFFICIAL_ART = new Set([
		'camera_app',
		'compass',
		'factory_test',
		'cap-lora-1262-gps',
		'ir-chat',
		'ir-remote',
		'keyboard-guide',
		'cap-lora-1262',
		'cap-cc1101-nfc',
		'zclaw',
		'cap-cc1101-subg-chat',
		'music',
		'piano',
		'recorder',
	]);

	// whether a device icon PNG is a full square (opaque corners) or a shape on transparency
	const opaque_cache = new Map<string, Promise<boolean>>();
	function is_opaque(src: string): Promise<boolean> {
		let p = opaque_cache.get(src);
		if (!p) {
			p = new Promise((resolve) => {
				const img = new Image();
				img.onload = () => {
					try {
						const c = document.createElement('canvas');
						c.width = img.naturalWidth;
						c.height = img.naturalHeight;
						const ctx = c.getContext('2d')!;
						ctx.drawImage(img, 0, 0);
						const w = c.width - 1;
						const h = c.height - 1;
						const a = [ctx.getImageData(1, 1, 1, 1), ctx.getImageData(w - 1, 1, 1, 1), ctx.getImageData(1, h - 1, 1, 1)];
						resolve(a.every((d) => d.data[3] > 200));
					} catch {
						resolve(false);
					}
				};
				img.onerror = () => resolve(false);
				img.src = src;
			});
			opaque_cache.set(src, p);
		}
		return p;
	}
</script>

<script lang="ts">
	import { ICON_TILE } from '🍎/lib/icon-shape.ts';
	import { icon_look } from '🍎/state/preferences.svelte.ts';

	// A device app's icon in the Dock, Apps and Finder:
	// - official CardputerZero apps: our macOS redesign of their own icon (light / dark)
	// - other apps: their own PNG as is — a full square is clipped to the macOS
	//   tile shape, a shape on transparency is shown without any plate.

	const { id, size = 56 }: { id: string; size?: number } = $props();

	const dark = $derived(icon_look() === 'dark');
	const official = $derived(OFFICIAL_ART.has(id));
	const src = $derived(official ? `/icons/off-${id}-${dark ? 'dark' : 'light'}.webp` : `/api/apps/icon?id=${encodeURIComponent(id)}`);
	const uid = $derived(`dv-${id.replace(/[^a-z0-9]/gi, '')}-${Math.round(size * 10)}`);

	let opaque = $state(true);
	$effect(() => {
		if (official) {
			opaque = true;
			return;
		}
		let alive = true;
		is_opaque(src).then((o) => alive && (opaque = o));
		return () => (alive = false);
	});

	const T0 = 9.88;
	const TS = 80.24;
</script>

<svg viewBox="0 0 100 100" width={size} height={size} role="img" aria-hidden="true" class="device-icon">
	<defs>
		<clipPath id="{uid}-clip"><path d={ICON_TILE} /></clipPath>
		<filter id="{uid}-shadow" x="-15%" y="-15%" width="130%" height="135%">
			<feDropShadow dx="0" dy="1" stdDeviation="1.3" flood-color="#000" flood-opacity="0.3" />
		</filter>
		<linearGradient id="{uid}-rim" x1="0" y1="0" x2="1" y2="1">
			<stop offset="0" stop-color="#fff" stop-opacity={dark ? 0.45 : 0.85} />
			<stop offset="0.28" stop-color="#fff" stop-opacity="0" />
			<stop offset="0.72" stop-color="#fff" stop-opacity="0" />
			<stop offset="1" stop-color="#fff" stop-opacity={dark ? 0.22 : 0.4} />
		</linearGradient>
	</defs>
	{#if opaque}
		<g filter="url(#{uid}-shadow)">
			<image href={src} x={T0} y={T0} width={TS} height={TS} preserveAspectRatio="xMidYMid slice" clip-path="url(#{uid}-clip)" />
		</g>
		<path d={ICON_TILE} fill="none" stroke="url(#{uid}-rim)" stroke-width="1.1" />
	{:else}
		<image href={src} x={T0} y={T0} width={TS} height={TS} preserveAspectRatio="xMidYMid meet" filter="url(#{uid}-shadow)" />
	{/if}
</svg>

<style>
	.device-icon {
		display: block;
		overflow: visible;
		user-select: none;
		-webkit-user-drag: none;
	}
</style>
