<script lang="ts">
	import { as_admin, Cancelled } from '🍎/state/admin.svelte.ts';
	import { onMount } from 'svelte';
	import { api, post } from '🍎/lib/api.ts';
	import AppIcon from '🍎/components/SVG/AppIcon.svelte';
	import DeviceIcon from '🍎/components/SVG/DeviceIcon.svelte';
	import { native_of, open_app, open_device_app } from '🍎/state/apps.svelte.ts';
	import { show_context_menu } from '🍎/state/contextmenu.svelte.ts';
	import { format_bytes, t } from '🍎/state/i18n.svelte.ts';
	import { DRAG_MIME, pin, unpin } from '🍎/state/launchpad.svelte.ts';
	import ConfirmSheet from '../common/ConfirmSheet.svelte';

	// Finder ▸ Applications: every app installed on the Zero, as icons.
	// Double-click opens it on this computer; "Move to Trash" uninstalls it.
	type DeviceApp = { id: string; name: string; terminal: boolean; hasIcon: boolean };
	type AppInfo = { package: string; removable: boolean; version?: string; sizeKB?: number };

	let { query = '' }: { query?: string } = $props();

	let list = $state<DeviceApp[]>([]);
	let selected = $state<string | null>(null);
	let removing = $state<DeviceApp | null>(null);
	let remove_info = $state<AppInfo | null>(null);
	let busy = $state(false);
	let error = $state('');

	const shown = $derived(
		query.trim() ? list.filter((a) => a.name.toLowerCase().includes(query.trim().toLowerCase())) : list,
	);

	async function load() {
		try {
			list = (await api<DeviceApp[]>('/api/apps')).filter((a) => !a.terminal);
		} catch (e) {
			error = (e as Error).message;
		}
	}

	onMount(() => {
		load();
		// the Dock's Trash forwards apps dropped on it
		const on_uninstall = (e: Event) => request_remove((e as CustomEvent<string>).detail);
		window.addEventListener('zerodesk-uninstall', on_uninstall);
		return () => window.removeEventListener('zerodesk-uninstall', on_uninstall);
	});

	function open(a: DeviceApp) {
		open_device_app({ id: a.id, name: a.name });
	}

	async function open_on_zero(a: DeviceApp) {
		try {
			await post('/api/apps/open', { id: a.id });
			open_app('device');
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function ask_remove(a: DeviceApp) {
		error = '';
		try {
			remove_info = await api<AppInfo>(`/api/apps/info?id=${encodeURIComponent(a.id)}`);
		} catch (e) {
			error = (e as Error).message;
			return;
		}
		if (!remove_info.removable) {
			error = t('apps.system_app', { name: a.name });
			return;
		}
		removing = a;
	}

	async function remove() {
		if (!removing) return;
		busy = true;
		try {
			const id = removing.id;
			await as_admin((password) => post('/api/apps/uninstall', { id, password }));
			unpin(removing.id);
			removing = null;
			await load();
		} catch (e) {
			if (!(e instanceof Cancelled)) error = (e as Error).message;
			removing = null;
		} finally {
			busy = false;
		}
	}

	function menu(e: MouseEvent, a: DeviceApp) {
		selected = a.id;
		show_context_menu(e, [
			{ label: t('apps.open'), action: () => open(a) },
			{ label: t('vapp.open_on_zero'), action: () => open_on_zero(a), break_after: true },
			{ label: t('apps.keep_in_dock'), action: () => pin({ id: a.id, name: a.name }), break_after: true },
			{ label: t('apps.move_to_trash'), action: () => ask_remove(a) },
		]);
	}

	function drag_start(e: DragEvent, a: DeviceApp) {
		e.dataTransfer?.setData(DRAG_MIME, JSON.stringify({ id: a.id, name: a.name }));
		e.dataTransfer!.effectAllowed = 'copy';
	}

	// The Dock's Trash asks us to uninstall an app dropped on it.
	export function request_remove(id: string) {
		const a = list.find((x) => x.id === id);
		if (a) ask_remove(a);
	}
</script>

<div class="apps" role="grid">
	{#each shown as a, i (a.id)}
		<button
			class="tile"
			class:selected={selected === a.id}
			style:--i={i}
			draggable="true"
			onclick={() => (selected = a.id)}
			ondblclick={() => open(a)}
			oncontextmenu={(e) => menu(e, a)}
			ondragstart={(e) => drag_start(e, a)}
		>
			<span class="icon">
				{#if native_of[a.id]}
					<AppIcon id={native_of[a.id]} size={56} />
				{:else if a.hasIcon}
					<DeviceIcon id={a.id} size={56} />
				{:else}
					<span class="fallback">{a.name.slice(0, 1)}</span>
				{/if}
			</span>
			<span class="label">{a.name}</span>
		</button>
	{/each}
</div>

{#if error}
	<button class="toast" onclick={() => (error = '')}>{error}</button>
{/if}

{#if removing}
	<ConfirmSheet
		title={t('apps.confirm_remove', { name: removing.name })}
		message={t('apps.confirm_remove_hint', {
			pkg: remove_info?.package ?? '',
			size: remove_info?.sizeKB ? format_bytes(remove_info.sizeKB * 1024) : '—',
		})}
		confirm_label={t('apps.remove')}
		cancel_label={t('finder.cancel')}
		destructive
		{busy}
		on_confirm={remove}
		on_cancel={() => (removing = null)}
	>
		{#snippet icon()}
			<img class="sheet-icon" src="/api/apps/icon?id={encodeURIComponent(removing!.id)}" alt="" />
		{/snippet}
	</ConfirmSheet>
{/if}

<style>
	.apps {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
		grid-auto-rows: 112px;
		gap: 6px;
		padding: 18px 18px 28px;
		align-content: start;
	}

	.tile {
		flex-direction: column;
		justify-content: flex-start;
		gap: 7px;
		padding-top: 8px;
		border-radius: var(--zd-radius-panel);
		animation: tile-in 360ms var(--zd-spring) backwards;
		animation-delay: calc(var(--i) * 10ms);

		&.selected {
			.icon {
				background: var(--zd-selected);
			}

			.label {
				background: var(--system-color-primary);
				color: var(--system-color-primary-contrast);
			}
		}
	}

	.icon {
		width: 66px;
		height: 66px;
		display: grid;
		place-items: center;
		border-radius: 10px;

		img {
			width: 100%;
			height: 100%;
			object-fit: cover;
		}

		.fallback {
			color: #fff;
			font-size: 24px;
			font-weight: 600;
		}
	}

	.label {
		max-width: 96px;
		padding: 1px 6px;
		border-radius: 4px;
		font-size: 12px;
		line-height: 1.3;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.toast {
		position: absolute;
		left: 50%;
		bottom: 44px;
		transform: translateX(-50%);
		max-width: 80%;
		padding: 8px 14px;
		border-radius: var(--zd-radius-panel);
		background: var(--zd-glass);
		backdrop-filter: blur(30px);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 6px 20px hsla(0, 0%, 0%, 0.18);
		font-size: 12px;
	}

	.sheet-icon {
		width: 48px;
		height: 48px;
		border-radius: 22.5%;
	}

	@keyframes tile-in {
		from {
			opacity: 0;
			transform: scale(0.9);
		}
	}
</style>
