<script lang="ts">
	import { onMount } from 'svelte';
	import { format_bytes, t } from '🍎/state/i18n.svelte.ts';
	import FileGlyph from './FileGlyph.svelte';

	type Entry = { name: string; path: string; size: number };
	const { entry, on_close, on_download }: { entry: Entry; on_close: () => void; on_download: () => void } = $props();

	const ext = $derived(entry.name.split('.').pop()!.toLowerCase());
	const url = $derived(`/api/fs/get?path=${encodeURIComponent(entry.path)}`);
	const kind = $derived(
		/^(png|jpe?g|gif|webp|bmp|svg)$/.test(ext)
			? 'image'
			: /^(mp3|wav|flac|ogg|m4a|aac)$/.test(ext)
				? 'audio'
				: /^(mp4|webm|mov)$/.test(ext)
					? 'video'
					: /^(txt|md|log|json|ya?ml|toml|ini|conf|cfg|sh|py|js|ts|c|cpp|h|go|rs|csv|desktop|service)$/.test(ext) ||
						  !entry.name.includes('.')
						? 'text'
						: 'other',
	);
	let text = $state('');

	onMount(async () => {
		if (kind === 'text' && entry.size < 512 * 1024) {
			try {
				text = await (await fetch(url, { headers: { Range: 'bytes=0-65535' } })).text();
			} catch {
				text = '';
			}
		}
	});
</script>

<svelte:window onkeydown={(e) => (e.key === 'Escape' || e.key === ' ') && (e.preventDefault(), on_close())} />

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="backdrop" onclick={on_close}>
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="panel" onclick={(e) => e.stopPropagation()}>
		<header>
			<span class="title">{entry.name}</span>
			<button class="zd-btn small" onclick={on_download}>{t('finder.download')}</button>
		</header>
		<div class="body">
			{#if kind === 'image'}
				<img src={url} alt={entry.name} />
			{:else if kind === 'video'}
				<!-- svelte-ignore a11y_media_has_caption -->
				<video src={url} controls autoplay></video>
			{:else if kind === 'audio'}
				<div class="center"><FileGlyph name={entry.name} dir={false} size={72} /><audio src={url} controls autoplay></audio></div>
			{:else if kind === 'text' && text}
				<pre>{text}</pre>
			{:else}
				<div class="center">
					<FileGlyph name={entry.name} dir={false} size={88} />
					<p>{format_bytes(entry.size)}</p>
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.backdrop {
		position: absolute;
		inset: 0;
		z-index: 40;
		display: grid;
		place-items: center;
		background: hsla(0, 0%, 0%, 0.12);
		animation: fade 160ms ease;
	}

	.panel {
		width: min(86%, 760px);
		height: min(86%, 560px);
		display: flex;
		flex-direction: column;
		border-radius: var(--zd-radius-window);
		background: var(--zd-glass);
		backdrop-filter: blur(40px) saturate(1.6);
		box-shadow:
			inset 0 0 0 0.5px var(--zd-glass-border),
			0 0 0 0.5px hsla(0, 0%, 0%, 0.2),
			0 20px 60px hsla(0, 0%, 0%, 0.3);
		overflow: hidden;
		animation: pop 360ms var(--zd-spring);
	}

	header {
		display: flex;
		align-items: center;
		gap: 12px;
		height: 42px;
		padding: 0 10px 0 16px;
		box-shadow: 0 0.5px 0 var(--zd-hairline);

		.title {
			flex: 1;
			font-weight: 600;
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}

		.small {
			padding: 0 12px;
			height: 24px;
			font-size: 12px;
		}
	}

	.body {
		flex: 1;
		min-height: 0;
		display: grid;
		place-items: center;
		overflow: auto;

		img,
		video {
			max-width: 100%;
			max-height: 100%;
			object-fit: contain;
		}

		pre {
			align-self: stretch;
			justify-self: stretch;
			margin: 0;
			padding: 16px 20px;
			white-space: pre-wrap;
			word-break: break-word;
			font-family: var(--system-font-family);
			font-size: 12px;
			line-height: 1.55;
			user-select: text;
			overflow: auto;
		}
	}

	.center {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 14px;
		color: var(--zd-text-secondary);
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
	}

	@keyframes pop {
		from {
			opacity: 0;
			transform: scale(0.92);
		}
	}
</style>
