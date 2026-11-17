<script lang="ts">
	import type { Snippet } from 'svelte';

	// macOS-style sheet that drops from the window's title bar.
	const {
		title,
		message,
		confirm_label,
		cancel_label,
		destructive = false,
		busy = false,
		icon,
		on_confirm,
		on_cancel,
	}: {
		title: string;
		message?: string;
		confirm_label: string;
		cancel_label: string;
		destructive?: boolean;
		busy?: boolean;
		icon?: Snippet;
		on_confirm: () => void;
		on_cancel: () => void;
	} = $props();
</script>

<svelte:window onkeydown={(e) => !busy && (e.key === 'Escape' ? on_cancel() : e.key === 'Enter' && on_confirm())} />

<div class="backdrop">
	<div class="sheet" role="alertdialog" aria-label={title}>
		{#if icon}<div class="icon">{@render icon()}</div>{/if}
		<h3>{title}</h3>
		{#if message}<p>{message}</p>{/if}
		<div class="buttons">
			<button class="zd-btn" disabled={busy} onclick={on_cancel}>{cancel_label}</button>
			<button class="zd-btn primary" class:destructive disabled={busy} onclick={on_confirm}>
				{#if busy}<span class="spinner"></span>{/if}{confirm_label}
			</button>
		</div>
	</div>
</div>

<style>
	.backdrop {
		position: absolute;
		inset: 0;
		z-index: 40;
		display: flex;
		justify-content: center;
		background: hsla(0, 0%, 0%, 0.08);
		animation: fade 160ms ease;
	}

	.sheet {
		margin-top: var(--zd-titlebar);
		width: 300px;
		height: max-content;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		padding: 20px 20px 16px;
		border-radius: 0 0 var(--zd-radius-window) var(--zd-radius-window);
		background: var(--zd-window-bg);
		box-shadow: 0 12px 40px hsla(0, 0%, 0%, 0.25);
		text-align: center;
		animation: sheet-down 340ms var(--zd-spring);
	}

	.icon {
		margin-bottom: 6px;
	}

	h3 {
		font-size: 13px;
		font-weight: 600;
		word-break: break-word;
	}

	p {
		font-size: 12px;
		color: var(--zd-text-secondary);
		line-height: 1.45;
	}

	.buttons {
		margin-top: 12px;
		width: 100%;
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
	}

	.zd-btn {
		height: 28px;
		gap: 6px;
		border-radius: var(--zd-radius-control);
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);
		font-size: 13px;
		font-weight: 500;
		transition: transform 200ms var(--zd-spring);

		&:active:not(:disabled) {
			transform: scale(0.97);
		}

		&:disabled {
			opacity: 0.6;
		}

		&.primary {
			background: var(--system-color-primary);
			color: var(--system-color-primary-contrast);
		}

		&.destructive {
			background: var(--zd-red);
			color: #fff;
		}
	}

	.spinner {
		width: 11px;
		height: 11px;
		border-radius: 50%;
		border: 1.5px solid hsla(0, 0%, 100%, 0.35);
		border-top-color: #fff;
		animation: spin 0.8s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
	}

	@keyframes sheet-down {
		from {
			transform: translateY(-100%);
		}
	}
</style>
