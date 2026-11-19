<script lang="ts">
	import { tick } from 'svelte';
	import { admin, answer } from '🍎/state/admin.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';
	import Glyph from '../apps/common/Glyph.svelte';
	import AppIcon from '../SVG/AppIcon.svelte';

	// The macOS authorization panel: the app's icon with a lock, what it wants
	// to do, the user name and password, Cancel and the action itself.

	let password = $state('');
	let input = $state<HTMLInputElement>();

	$effect(() => {
		if (admin.open) {
			password = '';
			tick().then(() => input?.focus());
		}
	});

	function ok() {
		if (password) answer(password);
	}
</script>

{#if admin.open}
	<div class="backdrop" role="presentation">
		<form
			class="panel"
			class:shake={admin.wrong}
			role="dialog"
			aria-label={admin.prompt.title ?? t('admin.title')}
			onsubmit={(e) => (e.preventDefault(), ok())}
		>
			<div class="icon">
				<AppIcon id="device" size={64} />
				<span class="lock"><Glyph name="lock_fill" size={13} /></span>
			</div>
			<h3>{admin.prompt.title ?? t('admin.title')}</h3>
			<p>{admin.wrong ? t('admin.wrong') : t('admin.allow')}</p>
			<div class="fields">
				<input value={session.user} readonly aria-label={t('admin.user')} tabindex="-1" />
				<input
					bind:this={input}
					bind:value={password}
					type="password"
					autocomplete="current-password"
					placeholder={t('admin.password')}
					onkeydown={(e) => e.key === 'Escape' && answer(null)}
				/>
			</div>
			<div class="buttons">
				<button type="button" class="zd-btn" onclick={() => answer(null)}>{t('admin.cancel')}</button>
				<button type="submit" class="zd-btn primary" disabled={!password}>{admin.prompt.action ?? t('admin.ok')}</button>
			</div>
		</form>
	</div>
{/if}

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: 9500;
		display: grid;
		place-items: center;
		padding: 16px;
		background: hsla(0, 0%, 0%, 0.18);
	}

	.panel {
		width: min(272px, 100%);
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 24px 20px 20px;
		border-radius: 24px;
		background: var(--zd-window-bg);
		box-shadow:
			0 0 0 0.5px var(--zd-hairline-strong),
			0 20px 60px hsla(0, 0%, 0%, 0.3);
		text-align: center;
		animation: pop 260ms var(--zd-spring);

		&.shake {
			animation: shake 360ms ease;
		}
	}

	.icon {
		position: relative;
		width: 64px;
		height: 64px;
		margin-bottom: 14px;
	}

	.lock {
		position: absolute;
		right: -4px;
		bottom: -2px;
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		color: var(--zd-text);
		background: var(--zd-window-bg);
		box-shadow:
			0 0 0 0.5px var(--zd-hairline-strong),
			0 2px 6px hsla(0, 0%, 0%, 0.18);
	}

	h3 {
		margin: 0;
		font-size: 13px;
		font-weight: 700;
		line-height: 1.35;
	}

	p {
		margin: 6px 0 0;
		font-size: 11px;
		line-height: 1.4;
		color: var(--zd-text-secondary);
	}

	.fields {
		display: flex;
		flex-direction: column;
		gap: 8px;
		width: 100%;
		margin-top: 16px;
	}

	input {
		width: 100%;
		height: 28px;
		padding: 0 12px;
		border: 0;
		border-radius: 8px;
		outline: 0;
		font: inherit;
		font-size: 13px;
		color: var(--zd-text);
		background: var(--zd-control-bg);
		box-shadow: 0 0 0 0.5px var(--zd-hairline-strong);

		&[readonly] {
			color: var(--zd-text-secondary);
		}

		&:focus {
			box-shadow:
				0 0 0 0.5px var(--zd-hairline-strong),
				0 0 0 3px color-mix(in srgb, var(--system-color-primary) 35%, transparent);
		}
	}

	.zd-btn {
		height: 28px;
		padding: 0 12px;
		border-radius: 14px;
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);
		font-size: 13px;
		font-weight: 500;
		color: var(--zd-text);
		white-space: nowrap;
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
	}

	.buttons {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
		width: 100%;
		margin-top: 16px;
	}

	@keyframes pop {
		from {
			opacity: 0;
			transform: scale(0.94);
		}
	}

	@keyframes shake {
		20%,
		60% {
			transform: translateX(-8px);
		}
		40%,
		80% {
			transform: translateX(8px);
		}
	}
</style>
