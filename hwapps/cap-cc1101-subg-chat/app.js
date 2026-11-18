'use strict';

const t = zd.localize({
	en: {
		title: 'SubG Chat', details: 'Details', placeholder: 'Message', send: 'Send', asciiOnly: 'ASCII letters, digits and symbols only',
		you: 'You', nickname: 'Nickname', edit: 'Edit Nickname', radio: 'Radio', clear: 'Clear Messages', cancel: 'Cancel', save: 'Save',
		listening: 'Listening', sending: 'Sending…', starting: 'Starting…', absent: 'Cap not found', busy: 'In use', error: 'Radio error', off: 'Off',
		noMessages: 'No Messages', startingTitle: 'Starting Radio', absentTitle: 'Cap CC1101 Not Found', busyTitle: 'Cap CC1101 in Use',
		busyText: 'Another app is using it', errorTitle: 'Radio Error', retry: 'Try Again',
		delivered: 'Delivered', sendingMsg: 'Sending…', notDelivered: 'Not Delivered', today: 'Today', yesterday: 'Yesterday',
		status: 'Status', chip: 'Chip', frequency: 'Frequency', modulation: 'Modulation', rate: 'Data Rate', deviation: 'Deviation',
		bandwidth: 'Bandwidth', power: 'TX Power', sync: 'Sync Word', bus: 'Bus', received: 'Received', crc: 'CRC Errors',
		raw: 'raw', badName: '1–10 ASCII characters', notReady: 'Radio not ready',
	},
	zh: {
		title: 'SubG Chat', details: '详细信息', placeholder: '消息', send: '发送', asciiOnly: '只能输入英文字母、数字和符号',
		you: '我', nickname: '昵称', edit: '编辑昵称', radio: '射频', clear: '清除消息', cancel: '取消', save: '存储',
		listening: '正在接收', sending: '正在发送…', starting: '正在启动…', absent: '未找到 Cap', busy: '使用中', error: '射频出错', off: '关闭',
		noMessages: '没有消息', startingTitle: '正在启动射频', absentTitle: '未找到 Cap CC1101', busyTitle: 'Cap CC1101 正在使用',
		busyText: '另一个应用正在使用', errorTitle: '射频出错', retry: '重试',
		delivered: '已送达', sendingMsg: '正在发送…', notDelivered: '未送达', today: '今天', yesterday: '昨天',
		status: '状态', chip: '芯片', frequency: '频率', modulation: '调制', rate: '速率', deviation: '频偏',
		bandwidth: '带宽', power: '发射功率', sync: '同步字', bus: '总线', received: '已接收', crc: 'CRC 错误',
		raw: '原始', badName: '1–10 个英文字符', notReady: '射频未就绪',
	},
});

const $ = (id) => document.getElementById(id);
const MAX = 56;
let radio = { state: 'off' };
let nickname = '';
let messages = [];
const shown = new Set();
let stick = true; // follow the bottom while the user has not scrolled up

function esc(s) {
	return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

const locale = () => (zd.lang === 'zh' ? 'zh-CN' : 'en-GB');
const ready = () => radio.state === 'listening' || radio.state === 'sending';

function stamp(ms) {
	const d = new Date(ms);
	const now = new Date();
	const time = d.toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' });
	const y = new Date(now);
	y.setDate(now.getDate() - 1);
	if (d.toDateString() === now.toDateString()) return `${t('today')} ${time}`;
	if (d.toDateString() === y.toDateString()) return `${t('yesterday')} ${time}`;
	const day = zd.lang === 'zh' ? `${d.getMonth() + 1}月${d.getDate()}日` : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	return `${day} ${time}`;
}

function dbm(v) {
	return `${v < 0 ? '−' : ''}${Math.abs(Math.round(v))} dBm`;
}

// ---- rendering ----

function renderStatus() {
	const s = radio.state;
	const label = t(s === 'off' ? 'starting' : s);
	$('status').textContent = ready() ? `868 MHz · ${label}` : label;
	$('input').disabled = !ready();
	updateSend();
}

function renderEmpty() {
	const s = radio.state;
	const [glyph, title, text, btn] = {
		listening: ['bubble_left_bubble_right', 'noMessages'],
		sending: ['bubble_left_bubble_right', 'noMessages'],
		absent: ['exclamationmark_triangle', 'absentTitle', '', 'retry'],
		busy: ['hourglass', 'busyTitle', 'busyText', 'retry'],
		error: ['exclamationmark_triangle', 'errorTitle', '', 'retry'],
	}[s] || ['', 'startingTitle'];
	const icon = $('empty-icon');
	if (glyph) icon.innerHTML = `<i class="zd-glyph">${glyph}</i>`;
	else if (!icon.querySelector('.spinner')) icon.innerHTML = '<div class="spinner"></div>';
	$('empty-title').textContent = t(title);
	$('empty-text').textContent = s === 'error' ? radio.detail || '' : text ? t(text) : '';
	$('empty-text').hidden = !$('empty-text').textContent;
	$('empty-btn').hidden = !btn;
	if (btn) $('empty-btn').textContent = t(btn);
}

function author(m) {
	return m.outgoing ? '\u0000me' : m.sender || '';
}

function renderThread() {
	$('empty').hidden = messages.length > 0;
	$('thread').hidden = messages.length === 0;
	if (!messages.length) {
		renderEmpty();
		return;
	}
	const lastOut = [...messages].reverse().find((m) => m.outgoing);
	const html = [];
	let prev = null;
	for (const m of messages) {
		const gap = !prev || m.time - prev.time > 10 * 60 * 1000;
		if (gap) html.push(`<div class="stamp">${esc(stamp(m.time))}</div>`);
		const first = gap || !prev || author(prev) !== author(m) || m.time - prev.time > 2 * 60 * 1000;
		const cls = ['msg', m.outgoing ? 'out' : 'in', first ? 'first' : '', m.status, m.legacy ? 'legacy' : ''].join(' ');
		const style = shown.has(m.id) ? ' style="animation:none"' : '';
		let meta = '';
		if (!m.outgoing) {
			meta = dbm(m.rssi) + (m.legacy ? ` · ${t('raw')}` : '');
		} else if (m.status === 'failed') {
			meta = `<i class="zd-glyph">exclamationmark_circle</i>${esc(t('notDelivered'))}`;
		} else if (m.status === 'sending') {
			meta = esc(t('sendingMsg'));
		} else if (m === lastOut && m.status === 'delivered') {
			meta = esc(t('delivered'));
		}
		html.push(`<div class="${cls}"${style} data-id="${m.id}" title="${esc(new Date(m.time).toLocaleTimeString(locale()))}">` +
			(!m.outgoing && first && m.sender ? `<span class="sender">${esc(m.sender)}</span>` : '') +
			`<div class="bubble">${esc(m.text)}</div>` + (meta ? `<span class="meta">${meta}</span>` : '') + '</div>');
		prev = m;
	}
	$('thread').innerHTML = html.join('');
	for (const m of messages) shown.add(m.id);
	if (stick) scrollToEnd();
}

function scrollToEnd() {
	const tr = $('transcript');
	requestAnimationFrame(() => tr.scrollTo({ top: tr.scrollHeight, behavior: 'smooth' }));
}

function row(label, value) {
	return `<div class="zd-row"><span class="label">${esc(label)}</span><span class="zd-value">${esc(value)}</span></div>`;
}

function renderInspector() {
	$('nick').textContent = nickname;
	const r = radio;
	const rows = [row(t('status'), t(r.state === 'off' ? 'starting' : r.state))];
	if (r.version !== undefined && r.version !== null) rows.push(row(t('chip'), `CC1101 · 0x${r.version.toString(16).toUpperCase().padStart(2, '0')}`));
	rows.push(row(t('frequency'), `${(r.frequency || 868).toFixed(3)} MHz`));
	rows.push(row(t('modulation'), '2-FSK'));
	rows.push(row(t('rate'), `${r.bitrate || 2.4} kbit/s`));
	rows.push(row(t('deviation'), `${r.deviation || 25.4} kHz`));
	rows.push(row(t('bandwidth'), `${r.bandwidth || 58} kHz`));
	rows.push(row(t('power'), `${r.power || 10} dBm`));
	rows.push(row(t('sync'), r.sync || '0x12AD'));
	if (r.bus) rows.push(row(t('bus'), r.bus.replace('/dev/', '')));
	if (r.received) rows.push(row(t('received'), String(r.received)));
	if (r.crc_errors) rows.push(row(t('crc'), String(r.crc_errors)));
	$('radio-card').innerHTML = rows.join('');
	$('clear').disabled = !messages.some((m) => m.status !== 'sending');
}

function render() {
	renderStatus();
	renderThread();
	renderInspector();
}

// ---- composer ----

let hintTimer = 0;
function hint(text) {
	const h = $('hint');
	h.textContent = text;
	h.hidden = false;
	clearTimeout(hintTimer);
	hintTimer = setTimeout(() => (h.hidden = true), 2400);
}

function ascii(s) {
	return s.replace(/[^\x20-\x7e]/g, '');
}

function updateSend() {
	const v = $('input').value;
	$('send').disabled = !ready() || !v.trim();
	const c = $('counter');
	c.hidden = v.length < MAX - 16;
	c.textContent = `${v.length}/${MAX}`;
	c.classList.toggle('full', v.length >= MAX);
}

$('input').addEventListener('input', () => {
	const el = $('input');
	if (el.value !== ascii(el.value)) {
		const pos = el.selectionStart;
		el.value = ascii(el.value).slice(0, MAX);
		el.setSelectionRange(Math.min(pos, el.value.length), Math.min(pos, el.value.length));
		hint(t('asciiOnly'));
	}
	updateSend();
});

async function send() {
	const el = $('input');
	const text = el.value;
	if (!text.trim() || !ready()) return;
	el.value = '';
	updateSend();
	stick = true;
	const r = await fetch('api/send', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text }) });
	if (!r.ok) {
		el.value = text;
		updateSend();
		const e = await r.json().catch(() => ({}));
		hint(r.status === 409 ? t('notReady') : e.error || String(r.status));
	}
}

$('input').addEventListener('keydown', (e) => {
	if (e.key === 'Enter' && !e.isComposing) {
		e.preventDefault();
		send();
	}
});
$('send').onclick = send;

$('transcript').addEventListener('scroll', () => {
	const tr = $('transcript');
	stick = tr.scrollHeight - tr.scrollTop - tr.clientHeight < 40;
});

// ---- inspector and nickname ----

$('inspector-btn').onclick = () => {
	const open = $('panel').hidden;
	$('panel').hidden = !open;
	$('inspector-btn').setAttribute('aria-pressed', String(open));
};
$('clear').onclick = () => fetch('api/clear', { method: 'POST' });
$('empty-btn').onclick = () => fetch('api/retry', { method: 'POST' });

function openSheet() {
	$('nick-input').value = nickname;
	$('nick-error').hidden = true;
	$('sheet').hidden = false;
	setTimeout(() => $('nick-input').select(), 50);
}

function closeSheet() {
	$('sheet').hidden = true;
}

$('nick-edit').onclick = openSheet;
$('nick-cancel').onclick = closeSheet;
$('nick-input').addEventListener('input', () => {
	const el = $('nick-input');
	if (el.value !== ascii(el.value)) el.value = ascii(el.value).slice(0, 10);
	$('nick-error').hidden = true;
});
$('nick-form').onsubmit = async (e) => {
	e.preventDefault();
	const name = $('nick-input').value;
	const r = name.trim()
		? await fetch('api/nickname', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) })
		: null;
	if (r && r.ok) {
		nickname = name;
		renderInspector();
		closeSheet();
	} else {
		$('nick-error').textContent = t('badName');
		$('nick-error').hidden = false;
	}
};
document.addEventListener('keydown', (e) => {
	if (e.key === 'Escape' && !$('sheet').hidden) closeSheet();
});

zd.on('lang', render);

// ---- data ----

function apply(s) {
	radio = s.radio || radio;
	nickname = s.nickname || nickname;
	messages = s.messages || [];
	render();
}

function upsert(m) {
	const i = messages.findIndex((x) => x.id === m.id);
	if (i >= 0) messages[i] = m;
	else {
		messages.push(m);
		if (messages.length > 64) messages = messages.slice(-64);
	}
}

fetch('api/state')
	.then((r) => r.json())
	.then(apply)
	.catch(() => {});
const es = new EventSource('api/stream');
es.addEventListener('snapshot', (e) => apply(JSON.parse(e.data)));
es.addEventListener('radio', (e) => {
	radio = JSON.parse(e.data);
	renderStatus();
	renderInspector();
	if (!messages.length) renderEmpty();
});
es.addEventListener('message', (e) => {
	upsert(JSON.parse(e.data));
	renderThread();
	renderInspector();
});
es.addEventListener('messages', (e) => {
	messages = JSON.parse(e.data) || [];
	render();
});
es.addEventListener('nickname', (e) => {
	nickname = JSON.parse(e.data);
	renderInspector();
});
render();
