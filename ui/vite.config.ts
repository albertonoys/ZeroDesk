import { svelte } from '@sveltejs/vite-plugin-svelte';
import UnpluginIcons from 'unplugin-icons/vite';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';

// During `pnpm dev`, API and WebSocket calls go to the real device.
const DEVICE = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.ZERODESK_DEVICE ?? 'http://cardputerzero.local:8686';

export default defineConfig({
	plugins: [svelte(), UnpluginIcons({ compiler: 'svelte' })],
	resolve: {
		alias: {
			'🍎': fileURLToPath(new URL('./src/', import.meta.url)),
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
