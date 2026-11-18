import { svelte } from '@sveltejs/vite-plugin-svelte';
import UnpluginIcons from 'unplugin-icons/vite';
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { defineConfig } from 'vite';

// Hash of the built-in icons: their URLs carry it (?v=) so browsers cache them for good.
const iconsDir = decodeURIComponent(new URL('./public/icons/', import.meta.url).pathname);
const iconHash = createHash('sha1');
for (const f of readdirSync(iconsDir).sort()) iconHash.update(f).update(readFileSync(iconsDir + f));
const ICON_V = iconHash.digest('hex').slice(0, 10);

// During `pnpm dev`, API and WebSocket calls go to the real device.
const DEVICE = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.ZERODESK_DEVICE ?? 'http://cardputerzero.local:8686';

export default defineConfig({
	plugins: [svelte(), UnpluginIcons({ compiler: 'svelte' })],
	define: { __ICON_V__: JSON.stringify(ICON_V) },
	resolve: {
		alias: {
			'🍎': decodeURIComponent(new URL('./src/', import.meta.url).pathname),
		},
	},
	build: {
		outDir: '../agent/web',
		emptyOutDir: true,
		assetsDir: '_app/immutable',
		target: 'es2022',
		reportCompressedSize: true,
	},
	server: {
		proxy: {
			'/api': { target: DEVICE, changeOrigin: true },
			'/ws': { target: DEVICE, ws: true, changeOrigin: true },
		},
	},
});
