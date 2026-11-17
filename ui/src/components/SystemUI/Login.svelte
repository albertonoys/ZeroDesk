<script lang="ts">
	import { onDestroy, onMount, tick } from 'svelte';
	import AppIcon from '🍎/components/SVG/AppIcon.svelte';
	import { t } from '🍎/state/i18n.svelte.ts';
	import { preferences, toggle_lang } from '🍎/state/preferences.svelte.ts';
	import { login, pair_cancel, pair_start, pair_status } from '🍎/state/session.svelte.ts';

	let mode = $state<'pair' | 'token'>('pair');
	let code = $state('');
	let pair = $state<'starting' | 'waiting' | 'denied' | 'offline'>('starting');
	let pair_id = '';
	let poll: ReturnType<typeof setTimeout> | undefined;
	let alive = true;

	let token = $state('');
	let busy = $state(false);
	let error = $state('');
	let shake = $state(false);
	let input_el = $state<HTMLInputElement>();

	async function start_pairing() {
		clearTimeout(poll);
		const res = await pair_start(preferences.lang);
		if (!alive || mode !== 'pair') return;
		if (!res) {
			pair = 'offline';
			poll = setTimeout(start_pairing, 3000);
			return;
		}
		pair_id = res.id;
		code = res.code;
		pair = 'waiting';
		const check = async () => {
			if (!alive || mode !== 'pair') return;
			const s = await pair_status(pair_id);
			if (s === 'approved') {
				pair_id = '';
				return; // session flips and this view unmounts
			}
			if (s === 'denied') return void (pair = 'denied');
			if (s === 'unavailable') return void to_token();
			if (s === 'expired') return void start_pairing(); // quietly issue a new code
			poll = setTimeout(check, s === 'offline' ? 2000 : 600);
		};
		check();
	}

	async function to_token() {
		mode = 'token';
		clearTimeout(poll);
		if (pair_id) pair_cancel(pair_id);
		pair_id = '';
		await tick();
		input_el?.focus();
	}

	function to_pair() {
		mode = 'pair';
		error = '';
		start_pairing();
	}

	onMount(() => {
		const m = location.hash.match(/token=([0-9a-f]+)/i);
		if (m) {
			token = m[1];
			history.replaceState(null, '', location.pathname);
			mode = 'token';
			submit();
			return;
		}
		start_pairing();
	});

	onDestroy(() => {
		alive = false;
		clearTimeout(poll);
		if (pair_id) pair_cancel(pair_id);
	});

	async function submit(e?: Event) {
		e?.preventDefault();
		if (!token.trim() || busy) return;
		busy = true;
		error = '';
		const res = await login(token.trim());
		busy = false;
		if (res === 'ok') return;
		error = res === 'wrong' ? t('login.wrong') : t('login.offline');
		shake = false;
		requestAnimationFrame(() => (shake = true));
		input_el?.select();
	}
</script>

<div class="screen">
	<button class="lang" onclick={toggle_lang} aria-label="Language">
		<span class="indicator" class:right={preferences.lang === 'en'}></span>
		<span class="opt" class:on={preferences.lang === 'zh'}>中</span>
		<span class="opt" class:on={preferences.lang === 'en'}>EN</span>
	</button>

	<div class="card" class:shake onanimationend={() => (shake = false)}>
		<AppIcon id="device" size={84} />
		<h1>{t('login.title')}</h1>

		{#if mode === 'pair'}
			<div class="code" class:muted={pair !== 'waiting'} aria-live="polite">
				{#key code}
					<span class="digits">{code || '····'}</span>
				{/key}
			</div>
			<p class="instruction">
				{#if pair === 'denied'}
					{t('login.denied')}
				{:else if pair === 'offline'}
					{t('login.offline')}
				{:else}
					{t('login.press_ok')}
				{/if}
			</p>
			<div class="links">
				{#if pair === 'denied'}
					<button class="link strong" onclick={start_pairing}>{t('login.again')}</button>
				{/if}
				<button class="link" onclick={to_token}>{t('login.use_token')}</button>
			</div>
		{:else}
			<form class="field" onsubmit={submit}>
				<input
					bind:this={input_el}
					bind:value={token}
					type="password"
					autocomplete="off"
					data-1p-ignore
					data-lpignore="true"
					data-form-type="other"
					spellcheck="false"
					placeholder={t('login.placeholder')}
					aria-label={t('login.placeholder')}
				/>
				<button type="submit" class="go" disabled={!token.trim() || busy} aria-label={t('login.submit')}>
					<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
						<path d="M3 8h9M8.5 4.5L12 8l-3.5 3.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
					</svg>
				</button>
			</form>
			<p class="error" class:visible={!!error}>{error || ' '}</p>
			<div class="links">
				<button class="link" onclick={to_pair}>{t('login.use_pair')}</button>
			</div>
		{/if}
	</div>
</div>

<style>
	.screen {
		position: fixed;
		inset: 0;
		display: grid;
		place-items: center;
		background: var(--zd-window-bg);
		animation: fade 400ms var(--zd-ease-out);
	}

	.lang {
		position: absolute;
		top: 16px;
		right: 18px;
		display: grid;
		grid-template-columns: 1fr 1fr;
		height: 24px;
		padding: 2px;
		border-radius: 12px;
		background: var(--zd-hover);
		font-size: 11px;
		font-weight: 600;
		color: var(--zd-text);

		.opt {
			position: relative;
			z-index: 1;
			min-width: 32px;
			line-height: 20px;
			text-align: center;
			opacity: 0.5;
			transition: opacity 200ms ease;

			&.on {
				opacity: 1;
			}
		}

		.indicator {
			position: absolute;
			top: 2px;
			left: 2px;
			width: calc(50% - 2px);
			height: 20px;
			border-radius: 10px;
			background: var(--zd-control-bg);
			box-shadow: var(--zd-control-shadow);
			transition: transform 420ms var(--zd-spring);

			&.right {
				transform: translateX(100%);
			}
		}
	}

	.card {
		display: flex;
		flex-direction: column;
		align-items: center;
		width: 360px;
		margin-top: -40px;
		color: var(--zd-text);
		text-align: center;

		&.shake {
			animation: shake 420ms cubic-bezier(0.36, 0.07, 0.19, 0.97);
		}
	}

	h1 {
		margin-top: 22px;
		font-size: 22px;
		font-weight: 600;
		letter-spacing: -0.015em;
	}

	.code {
		margin-top: 30px;
		height: 56px;
		display: grid;
		place-items: center;
		transition: opacity 300ms ease;

		&.muted {
			opacity: 0.3;
		}
	}

	.digits {
		font-size: 46px;
		font-weight: 300;
		letter-spacing: 0.32em;
		margin-right: -0.32em; /* keep the spaced digits optically centred */
		font-variant-numeric: tabular-nums;
		line-height: 1;
		animation: digits-in 420ms var(--zd-ease-out);
	}

	.instruction {
		margin-top: 18px;
		font-size: 14px;
		color: var(--zd-text-secondary);
	}

	.links {
		margin-top: 36px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
	}

	.link {
		padding: 5px 10px;
		border-radius: 7px;
		font-size: 12px;
		color: var(--zd-text-tertiary);
		transition:
			color 120ms ease,
			background-color 120ms ease;

		&:hover {
			color: var(--zd-text);
			background: var(--zd-hover);
		}

		&.strong {
			font-size: 13px;
			font-weight: 500;
			color: var(--zd-text);
		}
	}

	.field {
		margin-top: 32px;
		width: 260px;
		height: 36px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 6px 0 16px;
		border-radius: 18px;
		background: var(--zd-control-bg);
		box-shadow: var(--zd-control-shadow);
		transition: box-shadow 160ms ease;

		&:focus-within {
			box-shadow:
				var(--zd-control-shadow),
				0 0 0 3.5px hsla(var(--system-color-primary-hsl), 0.12);
		}

		input {
			flex: 1;
			min-width: 0;
			height: 100%;
			border: 0;
			outline: 0;
			background: none;
			font-size: 13px;
			letter-spacing: 0.04em;

			&::placeholder {
				color: var(--zd-text-tertiary);
				letter-spacing: 0;
			}
		}
	}

	.go {
		flex: none;
		width: 26px;
		height: 26px;
		border-radius: 13px;
		background: var(--system-color-primary);
		color: var(--system-color-primary-contrast);
		transition:
			opacity 160ms ease,
			transform 220ms var(--zd-spring);

		&:disabled {
			opacity: 0.15;
			transform: scale(0.9);
		}
	}

	.error {
		margin-top: 10px;
		min-height: 16px;
		font-size: 12px;
		color: var(--zd-red);
		opacity: 0;
		transition: opacity 160ms ease;

		&.visible {
			opacity: 1;
		}
	}

	@keyframes digits-in {
		from {
			opacity: 0;
			filter: blur(6px);
			transform: translateY(4px);
		}
	}

	@keyframes fade {
		from {
			opacity: 0;
		}
	}

	@keyframes shake {
		10%,
		90% {
			transform: translateX(-1px);
		}
		20%,
		80% {
			transform: translateX(3px);
		}
		30%,
		50%,
		70% {
			transform: translateX(-6px);
		}
		40%,
		60% {
			transform: translateX(6px);
		}
	}
</style>
