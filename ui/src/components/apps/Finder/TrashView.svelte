<script lang="ts">
	import { onMount } from 'svelte';
	import { api, post } from '🍎/lib/api.ts';
	import { show_context_menu } from '🍎/state/contextmenu.svelte.ts';
	import { format_bytes, format_date, t } from '🍎/state/i18n.svelte.ts';
	import ConfirmSheet from '../common/ConfirmSheet.svelte';
	import FileGlyph from './FileGlyph.svelte';

	// Finder ▸ Trash (shared with the Zero's own Linux desktop).
	type Item = { name: string; originalPath: string; deleted: string; dir: boolean; size: number };

	let { count = $bindable(0) }: { count?: number } = $props();

	let items = $state<Item[]>([]);
	let selected = $state<string | null>(null);
	let confirm = $state<'empty' | Item | null>(null);
	let error = $state('');

	export async function load() {
		try {
			items = await api<Item[]>('/api/trash');
			count = items.length;
		} catch (e) {
			error = (e as Error).message;
		}
	}

	onMount(load);

	export function ask_empty() {
		if (items.length) confirm = 'empty';
	}

	async function op(op: 'restore' | 'delete' | 'empty', name = '') {
		error = '';
		try {
			await post('/api/trash/op', { op, name });
		} catch (e) {
			error = (e as Error).message;
		}
		confirm = null;
		await load();
		window.dispatchEvent(new Event('zerodesk-trash-changed'));
	}

	function menu(e: MouseEvent, it: Item) {
		selected = it.name;
		show_context_menu(e, [
			{ label: t('trash.put_back'), action: () => op('restore', it.name), break_after: true },
			{ label: t('trash.delete_now'), action: () => (confirm = it) },
		]);
	}

	function where(p: string) {
		const dir = p.split('/').slice(0, -1).join('/') || '/';
		return dir.replace(/^\/home\/[^/]+/, '~');
	}
</script>

{#if items.length}
	<div class="header-row">
		<span>{t('finder.name')}</span>
		<span>{t('trash.original')}</span>
		<span>{t('trash.deleted')}</span>
		<span class="r">{t('finder.size')}</span>
	</div>
	<div class="list">
		{#each items as it (it.name)}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<div
				class="row"
				class:selected={selected === it.name}
				role="row"
				tabindex="-1"
				onclick={() => (selected = it.name)}
				oncontextmenu={(e) => menu(e, it)}
			>
				<span class="name"><FileGlyph name={it.name} dir={it.dir} /><span>{it.name}</span></span>
				<span class="dim">{where(it.originalPath)}</span>
				<span class="dim">{it.deleted ? format_date(it.deleted) : '—'}</span>
				<span class="dim r">{it.dir ? '—' : format_bytes(it.size)}</span>
			</div>
		{/each}
	</div>
{:else}
	<div class="empty">
		<svg viewBox="0 0 64 64" width="64" height="64" aria-hidden="true">
			<path d="M16 18h32l-3 36c-.2 2.3-2.1 4-4.4 4H23.4c-2.3 0-4.2-1.7-4.4-4z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" />
			<path d="M12 18h40M26 18v-4h12v4M27 26v24M37 26v24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
		</svg>
		<p>{t('trash.empty')}</p>
	</div>
{/if}

{#if error}
	<button class="toast" onclick={() => (error = '')}>{error}</button>
{/if}

{#if confirm}
	<ConfirmSheet
		title={confirm === 'empty' ? t('trash.confirm_empty', { n: items.length }) : t('finder.confirm_delete', { name: confirm.name })}
		message={t('finder.confirm_delete_hint')}
		confirm_label={confirm === 'empty' ? t('trash.empty_action') : t('finder.delete')}
		cancel_label={t('finder.cancel')}
		destructive
		on_confirm={() => (confirm === 'empty' ? op('empty') : op('delete', (confirm as Item).name))}
		on_cancel={() => (confirm = null)}
	/>
{/if}

<style>
	.header-row,
	.row {
		display: grid;
		grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr) 160px 76px;
		align-items: center;
		gap: 12px;
		padding: 0 16px;
	}

	.header-row {
		height: 28px;
		flex: none;
		font-size: 11px;
		font-weight: 500;
		color: var(--zd-text-secondary);
		box-shadow: 0 0.5px 0 var(--zd-hairline);
	}

	.list {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 4px 8px 8px;
	}

	.row {
		height: 30px;
		padding: 0 8px;
		border-radius: var(--zd-radius-control);
		font-size: 13px;

		&:nth-child(even) {
			background: hsla(0, 0%, 50%, 0.035);
		}

		&.selected {
			background: var(--system-color-primary);
			color: var(--system-color-primary-contrast);

			.dim {
				color: inherit;
				opacity: 0.75;
			}
		}
	}

	.name {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;

		span {
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}
	}

	.dim {
		color: var(--zd-text-secondary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.r {
		text-align: right;
		font-variant-numeric: tabular-nums;
	}

	.empty {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 12px;
		color: var(--zd-text-tertiary);
		font-size: 13px;
	}

	.toast {
		position: absolute;
		left: 50%;
		bottom: 44px;
		transform: translateX(-50%);
		padding: 8px 14px;
		border-radius: var(--zd-radius-panel);
		background: var(--zd-glass);
		backdrop-filter: blur(30px);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 6px 20px hsla(0, 0%, 0%, 0.18);
		font-size: 12px;
	}
</style>
