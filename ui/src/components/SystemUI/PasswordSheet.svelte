<script lang="ts">
	import { tick } from 'svelte';
	import { admin, answer } from '🍎/state/admin.svelte.ts';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { session } from '🍎/state/session.svelte.ts';
	import Glyph from '../apps/common/Glyph.svelte';

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
		<form class="sheet" class:shake={admin.wrong} role="dialog" aria-label={t('admin.title')} onsubmit={(e) => (e.preventDefault(), ok())}>
			<div class="icon"><Glyph name="lock_fill" size={30} /></div>
			<h3>{t('admin.title')}</h3>
			<p>{admin.wrong ? t('admin.wrong') : t('admin.message', { user: session.user })}</p>
			<input
				bind:this={input}
				bind:value={password}
				type="password"
				autocomplete="current-password"
				placeholder={t('admin.password')}
				onkeydown={(e) => e.key === 'Escape' && answer(null)}
			/>
			<div class="buttons">
				<button type="button" class="zd-btn" onclick={() => answer(null)}>{t('admin.cancel')}</button>
				<button type="submit" class="zd-btn primary" disabled={!password}>{t('admin.ok')}</button>
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

	.sheet {
		width: min(300px, 100%);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		padding: 22px 20px 18px;
		border-radius: 24px;
		background: var(--zd-window-bg);
		box-shadow:
			0 0 0 0.5px var(--zd-hairline-strong),
			0 20px 60px hsla(0, 0%, 0%, 0.3);
		text-align: center;

		&.shake {
			animation: shake 360ms ease;
		}
	}

	.icon {
		display: grid;
		place-items: center;
		width: 56px;
		height: 56px;
		border-radius: 14px;
		color: var(--zd-text-secondary);
		background: var(--zd-hover);
	}

	h3 {
		margin: 2px 0 0;
		font-size: 13px;
		font-weight: 700;
	}

	p {
		margin: 0;
		font-size: 11px;
		color: var(--zd-text-secondary);
	}

	input {
		width: 100%;
		height: 28px;
		margin-top: 4px;
		padding: 0 10px;
		border: 0;
		border-radius: 7px;
		outline: 0;
		font: inherit;
		font-size: 13px;
		color: var(--zd-text);
		background: var(--zd-control-bg);
		box-shadow: 0 0 0 0.5px var(--zd-hairline-strong);
	}

	.buttons {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
		width: 100%;
		margin-top: 6px;
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
