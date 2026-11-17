<script lang="ts">
	const { name, dir, size = 18 }: { name: string; dir: boolean; size?: number } = $props();
	const ext = $derived(name.includes('.') ? name.split('.').pop()!.toLowerCase() : '');
	const kind = $derived(
		dir
			? 'dir'
			: /^(png|jpe?g|gif|webp|bmp|svg|heic)$/.test(ext)
				? 'image'
				: /^(mp3|wav|flac|ogg|m4a|aac)$/.test(ext)
					? 'audio'
					: /^(mp4|mov|mkv|webm|avi)$/.test(ext)
						? 'video'
						: ext === 'deb'
							? 'deb'
							: /^(zip|tar|gz|xz|bz2|7z|rar)$/.test(ext)
								? 'archive'
								: 'doc',
	);
</script>

<svg viewBox="0 0 20 20" width={size} height={size} aria-hidden="true" class="glyph">
	{#if kind === 'dir'}
		<path d="M2.5 5.4c0-.9.7-1.6 1.6-1.6h3.6l1.6 1.6h6.6c.9 0 1.6.7 1.6 1.6v7.6c0 .9-.7 1.6-1.6 1.6H4.1c-.9 0-1.6-.7-1.6-1.6z" fill="#8e8e93" />
		<path d="M2.5 7.6c0-.9.7-1.6 1.6-1.6h11.8c.9 0 1.6.7 1.6 1.6v7c0 .9-.7 1.6-1.6 1.6H4.1c-.9 0-1.6-.7-1.6-1.6z" fill="#aeaeb2" />
	{:else}
		<path d="M5 2.5h6.5l3.5 3.5v10c0 .8-.7 1.5-1.5 1.5h-8.5c-.8 0-1.5-.7-1.5-1.5V4c0-.8.7-1.5 1.5-1.5z" fill="var(--zd-window-bg)" stroke="var(--zd-text-tertiary)" stroke-width="0.9" />
		<path d="M11.5 2.5V6h3.5" fill="none" stroke="var(--zd-text-tertiary)" stroke-width="0.9" stroke-linejoin="round" />
		{#if kind === 'image'}
			<path d="M6 14.5l2.4-3 1.8 2 1.6-1.6 2.2 2.6z" fill="#4fae6e" />
			<circle cx="8" cy="9.4" r="1" fill="#f0a43a" />
		{:else if kind === 'audio'}
			<path d="M9 8.5v4.6a1.4 1.4 0 1 1-1-1.3V8l4.2-1v4.8a1.4 1.4 0 1 1-1-1.3V8.2z" fill="#d9534f" />
		{:else if kind === 'video'}
			<path d="M8 9l4 2.4L8 13.8z" fill="#8a7b6c" />
		{:else if kind === 'deb'}
			<rect x="6" y="9" width="8" height="5" rx="1" fill="#b45f3c" />
			<path d="M6 10.6h8" stroke="#fff" stroke-width="0.8" opacity="0.6" />
		{:else if kind === 'archive'}
			<path d="M9.4 7h1.2v1.2H9.4zm0 2h1.2v1.2H9.4zm0 2h1.2v1.2H9.4z" fill="var(--zd-text-secondary)" />
			<rect x="9" y="12.6" width="2" height="2" rx="0.5" fill="var(--zd-text-secondary)" />
		{:else}
			<path d="M6.5 9h7M6.5 11h7M6.5 13h4.5" stroke="var(--zd-text-tertiary)" stroke-width="0.9" stroke-linecap="round" />
		{/if}
	{/if}
</svg>

<style>
	.glyph {
		flex: none;
		display: block;
	}
</style>
