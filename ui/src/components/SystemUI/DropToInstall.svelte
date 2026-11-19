<script lang="ts">
	import { t } from '🍎/state/i18n.svelte.ts';
	import { install_file, is_package } from '🍎/state/installer.svelte.ts';

	// Drop a .deb from the computer anywhere on ZeroDesk to install it, like
	// dragging an app out of a disk image. Windows that take drops themselves
	// (Finder uploads into the folder it shows) handle them first.

	let over = $state(false);
	let depth = 0;

	const has_files = (e: DragEvent) => !!e.dataTransfer && [...e.dataTransfer.types].includes('Files');
	const on_desktop = (e: Event) => !(e.target as Element | null)?.closest?.('section.window');

	function dragenter(e: DragEvent) {
		if (!has_files(e)) return;
		depth++;
		over = on_desktop(e);
	}

	function dragover(e: DragEvent) {
		if (!has_files(e) || e.defaultPrevented) return;
		e.preventDefault(); // otherwise the browser opens the file itself
		over = on_desktop(e);
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
	}

	function dragleave(e: DragEvent) {
		if (!has_files(e)) return;
		depth = Math.max(0, depth - 1);
		if (depth === 0) over = false;
	}

	function drop(e: DragEvent) {
		depth = 0;
		over = false;
		if (!has_files(e) || e.defaultPrevented) return;
		e.preventDefault();
		const pkg = [...(e.dataTransfer?.files ?? [])].find((f) => is_package(f.name));
		if (pkg) install_file(pkg);
	}
</script>

<svelte:window ondragenter={dragenter} ondragover={dragover} ondragleave={dragleave} ondrop={drop} />

{#if over}
	<div class="drop">
		<span>{t('desktop.drop_install')}</span>
	</div>
{/if}

<style>
	.drop {
		position: fixed;
		inset: 10px;
		z-index: 9300;
		display: grid;
		place-items: center;
		border-radius: 24px;
		border: 2px dashed color-mix(in srgb, var(--system-color-primary) 70%, transparent);
		background: color-mix(in srgb, var(--system-color-primary) 8%, transparent);
		pointer-events: none;
		animation: fade 160ms ease;

		span {
			padding: 10px 18px;
			border-radius: 999px;
			font-size: 13px;
			font-weight: 600;
			color: var(--zd-text);
			background: var(--zd-window-bg);
			box-shadow: 0 8px 30px hsla(0, 0%, 0%, 0.2);
		}
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
	}
</style>
