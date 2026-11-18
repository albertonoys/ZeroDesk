'use strict';

const t = zd.localize({
	en: {
		title: 'LoRa', details: 'Details', placeholder: 'LoRa Message', send: 'Send', cancel: 'Cancel',
		retry: 'Try Again', reply: 'Reply', copy: 'Copy', nickname: 'Nickname', link: 'Link', radio: 'Radio',
		hardware: 'Hardware', listening: 'Listening', sending: 'Sending', starting: 'Starting', off: 'Off',
		missing: 'Cap not found', unavailable: 'Unavailable', noMessages: 'No Messages',
		startingTitle: 'Starting Radio', missingTitle: 'LoRa Cap Not Found', errorTitle: 'Radio Unavailable',
		missingText: 'Attach the Cap LoRa-1262', notSent: 'Not Sent', sendingMeta: 'Sending…', sent: 'Sent',
		unknown: 'Unknown', origMissing: 'Original message unavailable', status: 'Status', rssi: 'RSSI', snr: 'SNR',
		received: 'Received', sentCount: 'Sent', crc: 'CRC errors', chip: 'Chip', frequency: 'Frequency',
		bandwidth: 'Bandwidth', sf: 'Spreading factor', cr: 'Coding rate', power: 'TX power', preamble: 'Preamble',
		sync: 'Sync word', tcxo: 'TCXO', current: 'Current limit', payload: 'Payload', spi: 'SPI', pi4io: 'PI4IO',
		symbols: 'symbols', today: 'Today', yesterday: 'Yesterday', tooLong: 'Too long', you: 'You',
	},
	zh: {
		title: 'LoRa', details: '详细信息', placeholder: 'LoRa 信息', send: '发送', cancel: '取消',
		retry: '重试', reply: '回复', copy: '拷贝', nickname: '昵称', link: '链路', radio: '射频',
		hardware: '硬件', listening: '接收中', sending: '发送中', starting: '正在启动', off: '已关闭',
		missing: '未找到扩展板', unavailable: '不可用', noMessages: '没有信息',
		startingTitle: '正在启动射频', missingTitle: '未找到 LoRa 扩展板', errorTitle: '射频不可用',
		missingText: '请装上 Cap LoRa-1262', notSent: '未发送', sendingMeta: '正在发送…', sent: '已发送',
		unknown: '未知', origMissing: '原信息不可用', status: '状态', rssi: 'RSSI', snr: 'SNR',
		received: '已接收', sentCount: '已发送', crc: 'CRC 错误', chip: '芯片', frequency: '频率',
		bandwidth: '带宽', sf: '扩频因子', cr: '编码率', power: '发射功率', preamble: '前导码',
		sync: '同步字', tcxo: 'TCXO', current: '电流限制', payload: '载荷', spi: 'SPI', pi4io: 'PI4IO',
		symbols: '符号', today: '今天', yesterday: '昨天', tooLong: '过长', you: '我',
	},
});

const $ = (id) => document.getElementById(id);
const enc = new TextEncoder();
let state = null;
let replyTo = null; // message id
let menuFor = null;
let shownKey = ''; // transcript render key
let atBottom = true;

const bytes = (s) => enc.encode(s).length;

// trim to a byte budget without splitting a character
function clip(s, max) {
	if (bytes(s) <= max) return s;
	let out = '';
	for (const ch of s) {
		if (bytes(out + ch) > max) break;
		out += ch;
	}
	return out;
}

function fmtTime(ms) {
	return new Intl.DateTimeFormat(zd.lang === 'zh' ? 'zh-CN' : 'en-US', { hour: '2-digit', minute: '2-digit' }).format(ms);
}

function fmtStamp(ms) {
	const d = new Date(ms);
	const today = new Date();
	const y = new Date();
	y.setDate(today.getDate() - 1);
	const same = (a, b) => a.toDateString() === b.toDateString();
	let day;
	if (same(d, today)) day = t('today');
	else if (same(d, y)) day = t('yesterday');
	else day = new Intl.DateTimeFormat(zd.lang === 'zh' ? 'zh-CN' : 'en-US', { month: 'short', day: 'numeric' }).format(d);
	return day + ' ' + fmtTime(ms);
}

function el(tag, cls, text) {
	const e = document.createElement(tag);
	if (cls) e.className = cls;
	if (text !== undefined) e.textContent = text;
	return e;
}

function senderName(m) {
	return m.sender || t('unknown');
}

// ---- transcript ----

function renderTranscript() {
	const msgs = state.messages;
	const key = JSON.stringify([msgs.map((m) => [m.id, m.delivery]), replyTo, menuFor, zd.lang]);
	if (key === shownKey) return;
	shownKey = key;
	const box = $('transcript');
	const stick = atBottom;
	box.textContent = '';
	let prev = null;
	const lastOut = [...msgs].reverse().find((m) => m.outgoing);
	for (const m of msgs) {
		if (!prev || m.time - prev.time > 15 * 60 * 1000) {
			box.appendChild(el('div', 'stamp', fmtStamp(m.time)));
			prev = null;
		}
		const sameRun = prev && prev.outgoing === m.outgoing && (m.outgoing || prev.sender === m.sender);
		const row = el('div', 'msg' + (m.outgoing ? ' out' : '') + (sameRun ? '' : ' first') + (m.delivery === 'failed' ? ' failed' : ''));
		if (m.id === menuFor) row.classList.add('selected');
		row.dataset.id = m.id;
		row.title = fmtTime(m.time);
		if (!m.outgoing && !sameRun) row.appendChild(el('div', 'sender', senderName(m)));
		const line = el('div', 'line');
		const bubble = el('div', 'bubble');
		if (m.reply_text || m.reply_missing) {
			const q = el('span', 'quote' + (m.reply_missing ? ' missing' : ''));
			if (m.reply_missing) q.textContent = t('origMissing');
			else {
				q.appendChild(el('b', '', m.reply_sender || t('unknown')));
				q.appendChild(document.createTextNode(m.reply_text));
			}
			bubble.appendChild(q);
		}
		bubble.appendChild(document.createTextNode(m.text));
		line.appendChild(bubble);
		if (canReply(m)) {
			const rb = el('button', 'reply-btn');
			rb.title = t('reply');
			rb.innerHTML = '<i class="zd-glyph">arrowshape_turn_up_left_fill</i>';
			rb.onclick = () => startReply(m.id);
			line.appendChild(rb);
		}
		row.appendChild(line);
		let meta = '';
		let cls = 'meta';
		if (!m.outgoing) meta = `${Math.round(m.rssi)} dBm · SNR ${m.snr.toFixed(1)} dB`;
		else if (m.delivery === 'pending') meta = t('sendingMeta');
		else if (m.delivery === 'failed') {
			meta = t('notSent');
			cls += ' failed';
		} else if (m === lastOut) meta = t('sent');
		if (meta) row.appendChild(el('div', cls, meta));
		box.appendChild(row);
		prev = m;
	}
	if (stick) box.scrollTop = box.scrollHeight;
}

$('transcript').addEventListener('scroll', () => {
	const b = $('transcript');
	atBottom = b.scrollHeight - b.scrollTop - b.clientHeight < 24;
});

function canReply(m) {
	return m.delivery === 'received' || m.delivery === 'sent';
}

function find(id) {
	return state && state.messages.find((m) => m.id === id);
}

// ---- status, empty state, composer ----

function render() {
	if (!state) return;
	const r = state.radio;
	const st = $('status');
	let label;
	let cls;
	switch (r.state) {
		case 'receiving':
			label = `${t('listening')} · ${state.config.frequency_mhz} MHz`;
			cls = 'ok';
			break;
		case 'sending':
			label = t('sending');
			cls = 'busy';
			break;
		case 'starting':
			label = t('starting');
			cls = 'busy';
			break;
		case 'error':
			label = r.missing ? t('missing') : t('unavailable');
			cls = 'bad';
			break;
		default:
			label = t('off');
			cls = '';
	}
	st.className = 'zd-subtitle status ' + cls;
	$('status-text').textContent = label;

	const empty = $('empty');
	const none = state.messages.length === 0;
	let glyph = 'bubble_left_bubble_right';
	let title = '';
	let text = '';
	let retry = false;
	if (r.state === 'error' && none) {
		glyph = 'exclamationmark_triangle';
		title = r.missing ? t('missingTitle') : t('errorTitle');
		text = r.missing ? t('missingText') : r.error || '';
		retry = true;
	} else if ((r.state === 'starting' || r.state === 'off') && none) {
		glyph = 'antenna_radiowaves_left_right';
		title = t('startingTitle');
	} else if (none) {
		title = t('noMessages');
		const c = state.config;
		text = `${c.frequency_mhz} MHz · SF${c.sf} · ${c.bandwidth_khz} kHz`;
	}
	empty.hidden = !title;
	$('empty-glyph').textContent = glyph;
	$('empty-title').textContent = title;
	$('empty-text').textContent = text;
	$('retry').hidden = !retry;

	if (replyTo !== null && !find(replyTo)) replyTo = null;
	renderReplyBar();
	renderTranscript();
	renderComposer();
	renderInspector();
}

function limit() {
	return replyTo !== null ? state.config.reply_limit : state.config.payload_limit;
}

function renderComposer() {
	const input = $('input');
	const ready = state.radio.state === 'receiving';
	const v = input.value;
	const left = limit() - bytes(v);
	const c = $('count');
	c.textContent = left <= 20 ? String(left) : '';
	c.classList.toggle('full', left <= 0);
	$('send').disabled = !ready || v.trim() === '' || left < 0;
}

function renderReplyBar() {
	const m = replyTo !== null ? find(replyTo) : null;
	$('reply-bar').hidden = !m;
	if (!m) return;
	$('reply-sender').textContent = m.outgoing ? state.nickname : senderName(m);
	$('reply-text').textContent = m.text;
}

function startReply(id) {
	const m = find(id);
	if (!m || !canReply(m)) return;
	replyTo = id;
	const input = $('input');
	input.value = clip(input.value, limit());
	render();
	input.focus();
}

$('reply-cancel').onclick = () => {
	replyTo = null;
	render();
	$('input').focus();
};

$('input').addEventListener('input', () => {
	const input = $('input');
	const clipped = clip(input.value, limit());
	if (clipped !== input.value) input.value = clipped;
	renderComposer();
});

$('input').addEventListener('keydown', (e) => {
	if (e.key === 'Escape' && replyTo !== null) {
		replyTo = null;
		render();
	}
});

$('form').addEventListener('submit', async (e) => {
	e.preventDefault();
	const input = $('input');
	const text = input.value;
	if ($('send').disabled || !text.trim()) return;
	const body = { text };
	if (replyTo !== null) body.reply_to = replyTo;
	const r = await fetch('api/send', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
	if (r.ok) {
		input.value = '';
		replyTo = null;
		atBottom = true;
		render();
	}
});

$('retry').onclick = () => fetch('api/retry', { method: 'POST' });

// ---- inspector ----

function rows(card, list) {
	if (card.childElementCount !== list.length) {
		card.textContent = '';
		for (const [k] of list) {
			const row = el('div', 'zd-row');
			row.appendChild(el('span', '', ''));
			row.appendChild(el('span', 'zd-value', ''));
			row.dataset.k = k;
			card.appendChild(row);
		}
	}
	list.forEach(([k, v], i) => {
		const row = card.children[i];
		row.firstChild.textContent = t(k);
		row.lastChild.textContent = v;
		row.lastChild.title = v;
	});
}

function renderInspector() {
	if ($('panel').hidden) return;
	const r = state.radio;
	const c = state.config;
	const haveRx = r.rx > 0;
	rows($('link'), [
		['status', $('status-text').textContent],
		['rssi', haveRx ? `${Math.round(r.rssi)} dBm` : '–'],
		['snr', haveRx ? `${r.snr.toFixed(1)} dB` : '–'],
		['received', String(r.rx)],
		['sentCount', String(r.tx)],
		['crc', String(r.crc_errors)],
	]);
	rows($('radio'), [
		['chip', r.chip || 'SX1262'],
		['frequency', `${c.frequency_mhz.toFixed(3)} MHz`],
		['bandwidth', `${c.bandwidth_khz} kHz`],
		['sf', `SF${c.sf}`],
		['cr', `4/${c.cr}`],
		['power', `${c.power_dbm} dBm`],
		['preamble', `${c.preamble} ${t('symbols')}`],
		['sync', '0x' + c.sync_word.toString(16).toUpperCase().padStart(2, '0')],
		['tcxo', `${c.tcxo_v.toFixed(1)} V`],
		['current', `${c.current_limit_ma} mA`],
		['payload', `${c.payload_limit} B`],
	]);
	rows($('hardware'), [
		['spi', `${r.spi} · ${c.spi_speed_hz / 1e6} MHz`],
		['pi4io', r.pi4io],
	]);
	const nick = $('nick');
	if (document.activeElement !== nick) nick.value = state.nickname;
}

$('inspector').onclick = () => {
	const open = $('panel').hidden;
	$('panel').hidden = !open;
	$('inspector').setAttribute('aria-pressed', String(open));
	if (open) renderInspector();
};

function validNick(s) {
	return s.length > 0 && s.length <= 10 && s.trim() !== '' && /^[\x20-\x7e]+$/.test(s);
}

$('nick').addEventListener('input', () => {
	$('nick').classList.toggle('invalid', !validNick($('nick').value));
});

async function saveNick() {
	const nick = $('nick');
	const v = nick.value;
	if (!state || v === state.nickname) return;
	if (!validNick(v)) {
		nick.value = state.nickname;
		nick.classList.remove('invalid');
		return;
	}
	const r = await fetch('api/nickname', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ nickname: v }) });
	if (!r.ok) nick.value = state.nickname;
}

$('nick').addEventListener('keydown', (e) => {
	if (e.key === 'Enter') $('nick').blur();
	if (e.key === 'Escape') {
		$('nick').value = state.nickname;
		$('nick').blur();
	}
});
$('nick').addEventListener('blur', saveNick);

// ---- context menu ----

function closeMenu() {
	if (menuFor === null) return;
	menuFor = null;
	$('menu').hidden = true;
	render();
}

$('transcript').addEventListener('contextmenu', (e) => {
	const row = e.target.closest('.msg');
	if (!row) return;
	e.preventDefault();
	const m = find(Number(row.dataset.id));
	if (!m) return;
	menuFor = m.id;
	const menu = $('menu');
	menu.querySelector('[data-act="reply"]').disabled = !canReply(m);
	menu.hidden = false;
	const w = menu.offsetWidth;
	const h = menu.offsetHeight;
	menu.style.left = Math.min(e.clientX, innerWidth - w - 6) + 'px';
	menu.style.top = Math.min(e.clientY, innerHeight - h - 6) + 'px';
	render();
});

$('transcript').addEventListener('dblclick', (e) => {
	const row = e.target.closest('.msg');
	if (row) startReply(Number(row.dataset.id));
});

$('menu').addEventListener('click', (e) => {
	const b = e.target.closest('button');
	if (!b || menuFor === null) return;
	const m = find(menuFor);
	closeMenu();
	if (!m) return;
	if (b.dataset.act === 'reply') startReply(m.id);
	else copyText(m.text);
});

document.addEventListener('pointerdown', (e) => {
	if (!e.target.closest('#menu')) closeMenu();
});
document.addEventListener('keydown', (e) => {
	if (e.key === 'Escape') closeMenu();
});

function copyText(s) {
	const ta = document.createElement('textarea');
	ta.value = s;
	ta.style.position = 'fixed';
	ta.style.opacity = '0';
	document.body.appendChild(ta);
	ta.select();
	try {
		document.execCommand('copy');
	} catch (_) {}
	ta.remove();
	if (navigator.clipboard) navigator.clipboard.writeText(s).catch(() => {});
}

// ---- data ----

zd.on('lang', () => {
	shownKey = '';
	render();
});

fetch('api/state')
	.then((r) => r.json())
	.then((s) => {
		if (!state) {
			state = s;
			render();
		}
	});
const es = new EventSource('api/stream');
es.addEventListener('state', (e) => {
	state = JSON.parse(e.data);
	render();
});
