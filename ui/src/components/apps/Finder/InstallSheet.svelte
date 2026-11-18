<script lang="ts">
	import { api_error } from '🍎/lib/api.ts';
	import { as_admin } from '🍎/state/admin.svelte.ts';
	import { onMount } from 'svelte';
	import { t } from '🍎/state/i18n.svelte.ts';
	import FileGlyph from './FileGlyph.svelte';

	type Entry = { name: string; path: string };
	const { entry, on_close }: { entry: Entry; on_close: () => void } = $props();

	let log = $state('');
	let done = $state(false);
	let ok = $state(false);
	let log_el = $state<HTMLElement>();

	onMount(async () => {
		try {
			// the device asks for the user's password first (agent/privileged.go)
			const res = await as_admin(async (password) => {
				const r = await fetch('/api/deb/install', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ path: entry.path, password }),
				});
				if (!r.ok) throw await api_error(r);
				return r;
			});
			const reader = res.body!.getReader();
			const dec = new TextDecoder();
			for (;;) {
				const { value, done: end } = await reader.read();
				if (end) break;
				log += dec.decode(value, { stream: true });
				requestAnimationFrame(() => log_el?.scrollTo({ top: log_el.scrollHeight }));
			}
			ok = /\[exit\] 0\s*$/.test(log);
		} catch (e) {
			log += `\n${e}`;
		}
		done = true;
	});
</script>

<div class="backdrop">
	<div class="sheet">
		<div class="head">
			<FileGlyph name={entry.name} dir={false} size={36} />
			<div>
				<h3>{t('finder.installing', { name: entry.name })}</h3>
				<div class="bar" class:done class:ok><span></span></div>
			</div>
		</div>
		<pre bind:this={log_el}>{log}</pre>
		<button class="zd-btn" disabled={!done} onclick={on_close}>{t('finder.done')}</button>
	</div>
</div>

<style>
	.backdrop {
		position: absolute;
		inset: 0;
		z-index: 30;
		display: flex;
		justify-content: center;
		background: hsla(0, 0%, 0%, 0.08);
	}

	.sheet {
		margin-top: var(--zd-titlebar);
		width: min(520px, 90%);
		height: max-content;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
		border-radius: 0 0 var(--zd-radius-window) var(--zd-radius-window);
		background: var(--zd-window-bg);
		box-shadow: 0 12px 40px hsla(0, 0%, 0%, 0.25);
		animation: sheet-down 340ms var(--zd-spring);

		button {
			align-self: flex-end;
			padding: 0 18px;

			&:disabled {
				opacity: 0.4;
			}
		}
	}

	.head {
		display: flex;
		align-items: center;
		gap: 12px;

		div {
			flex: 1;
			min-width: 0;
		}

		h3 {
			font-size: 13px;
			font-weight: 600;
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}
	}

	.bar {
		margin-top: 8px;
		height: 4px;
		border-radius: 2px;
		background: var(--zd-hover);
		overflow: hidden;

		span {
			display: block;
			width: 35%;
			height: 100%;
			border-radius: 2px;
			background: var(--system-color-primary);
			animation: indeterminate 1.1s ease-in-out infinite;
		}

		&.done span {
			width: 100%;
			animation: none;
			background: var(--zd-red);
		}

		&.done.ok span {
			background: var(--zd-green);
		}
	}

	pre {
		height: 180px;
		margin: 0;
		padding: 10px 12px;
		border-radius: var(--zd-radius-inner);
		background: var(--zd-hover);
		font-family: var(--system-font-family);
		font-size: 11px;
		line-height: 1.5;
		white-space: pre-wrap;
		word-break: break-word;
		overflow-y: auto;
		user-select: text;
	}

	@keyframes indeterminate {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(290%);
		}
	}

	@keyframes sheet-down {
		from {
			transform: translateY(-100%);
		}
	}
</style>
