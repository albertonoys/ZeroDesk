<script lang="ts">
	import { tick } from 'svelte';
	import { admin, answer } from '🍎/state/admin.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';
	import { phone } from '🍎/state/phone.svelte.ts';
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

{#if admin.open && phone.active}
	<!-- iPhone: the install confirmation slides up from the bottom -->
	<div class="sheet-layer" role="presentation" onclick={(e) => e.target === e.currentTarget && answer(null)}>
		<form
			class="sheet"
			class:shake={admin.wrong}
			role="dialog"
			aria-label={admin.prompt.title ?? t('admin.title')}
			onsubmit={(e) => (e.preventDefault(), ok())}
		>
			<div class="grabber"></div>
			<header>
				<b>{admin.prompt.source ?? 'ZeroDesk'}</b>
				<button type="button" class="x" aria-label={t('admin.cancel')} onclick={() => answer(null)}>
					<svg viewBox="0 0 20 20" width="14" height="14" aria-hidden="true"><path d="M5 5l10 10M15 5 5 15" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" /></svg>
				</button>
			</header>
			<div class="what">
				{#if admin.prompt.app?.icon}
					<img src={admin.prompt.app.icon} alt="" width="56" height="56" />
				{:else}
					<AppIcon id="device" size={56} />
				{/if}
				<span>
					<b>{admin.prompt.app?.name ?? (admin.prompt.title ?? t('admin.title'))}</b>
					<small>{admin.wrong ? t('admin.wrong') : t('admin.allow')}</small>
				</span>
			</div>
			<div class="account">
				<span>{t('admin.user')}</span><b>{session.user}</b>
			</div>
			<input
				bind:this={input}
				bind:value={password}
				type="password"
				autocomplete="current-password"
				placeholder={t('admin.password')}
			/>
			<button type="submit" class="go" disabled={!password}>{admin.prompt.short ?? admin.prompt.action ?? t('admin.ok')}</button>
		</form>
	</div>
{:else if admin.open}
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

	/* ---- iPhone sheet ---- */

	.sheet-layer {
		position: fixed;
		inset: 0;
		z-index: 9500;
		display: flex;
		align-items: flex-end;
		background: hsla(0, 0%, 0%, 0.28);
		animation: fade 200ms ease;
	}

	.sheet {
		width: 100%;
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 8px 20px calc(env(safe-area-inset-bottom) + 20px);
		border-radius: 28px 28px 0 0;
		background: var(--zd-window-bg);
		box-shadow: 0 -10px 40px hsla(0, 0%, 0%, 0.2);
		animation: slide-up 360ms var(--zd-spring);

		&.shake {
			animation: shake 360ms ease;
		}

		header {
			display: flex;
			align-items: center;
			justify-content: space-between;

			b {
				font-size: 17px;
				font-weight: 700;
			}
		}

		input {
			height: 44px;
			font-size: 16px; /* below 16px iOS zooms the page on focus */
			border-radius: 12px;
		}
	}

	.grabber {
		align-self: center;
		width: 36px;
		height: 5px;
		border-radius: 3px;
		background: var(--zd-hairline-strong);
	}

	.x {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border-radius: 50%;
		color: var(--zd-text-secondary);
		background: var(--zd-hover);
	}

	.what {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 12px 0;
		border-block: 0.5px solid var(--zd-hairline);

		img {
			border-radius: 13px;
		}

		span {
			display: flex;
			flex-direction: column;
			gap: 3px;
			min-width: 0;
		}

		b {
			font-size: 16px;
			font-weight: 600;
		}

		small {
			font-size: 13px;
			color: var(--zd-text-secondary);
		}
	}

	.account {
		display: flex;
		justify-content: space-between;
		font-size: 15px;

		span {
			color: var(--zd-text-secondary);
		}

		b {
			font-weight: 500;
		}
	}

	.go {
		height: 50px;
		border-radius: 25px;
		font-size: 17px;
		font-weight: 600;
		color: var(--system-color-primary-contrast);
		background: var(--system-color-primary);

		&:disabled {
			opacity: 0.45;
		}
	}

	@keyframes slide-up {
		from {
			transform: translateY(100%);
		}
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
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
