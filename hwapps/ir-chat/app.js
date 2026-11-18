'use strict';

const MAX = 7; // the original's single-frame limit (printable ASCII)

const t = zd.localize({
	en: {
		title: 'IR Chat', clear: 'Clear Messages', info: 'Info', send: 'Send', placeholder: 'IR Message',
		retry: 'Try Again', link: 'Infrared', traffic: 'Traffic', receiving: 'Listening', sending: 'Sending…',
		starting: 'Starting…', stopped: 'Stopped', error: 'Unavailable', noMessages: 'No Messages',
		noIr: 'IR Unavailable', receiver: 'Receiver', transmitter: 'Transmitter', carrier: 'Carrier',
		protocol: 'Protocol', received: 'Received', sent: 'Sent', crc: 'CRC Errors', malformed: 'Other Frames',
		state: 'Status', failed: 'Not Sent', resend: 'Resend', today: 'Today', none: '–',
		'err-no-receiver': 'No IR receiver', 'err-no-transmitter': 'No IR transmitter',
		'err-permission': 'No permission for IR', 'err-busy': 'IR device busy', 'err-io': 'IR read failed',
		'err-not-ready': 'IR is not ready', 'err-too-long': 'ASCII, 7 characters at most', 'err-empty': 'Empty message',
	},
	zh: {
		title: '红外聊天', clear: '清除消息', info: '信息', send: '发送', placeholder: '红外消息', retry: '重试',
		link: '红外', traffic: '收发', receiving: '正在接收', sending: '正在发送…', starting: '正在启动…',
		stopped: '已停止', error: '不可用', noMessages: '没有消息', noIr: '红外不可用', receiver: '接收器',
		transmitter: '发射器', carrier: '载波', protocol: '协议', received: '已接收', sent: '已发送',
		crc: 'CRC 错误', malformed: '其他帧', state: '状态', failed: '未发送', resend: '重新发送', today: '今天',
		none: '–', 'err-no-receiver': '没有红外接收器', 'err-no-transmitter': '没有红外发射器',
		'err-permission': '无权访问红外设备', 'err-busy': '红外设备忙', 'err-io': '红外读取失败',
		'err-not-ready': '红外未就绪', 'err-too-long': '仅限 ASCII，最多 7 个字符', 'err-empty': '消息为空',
	},
});

const $ = (id) => document.getElementById(id);
const errText = (e) => (t('err-' + e) === 'err-' + e ? t('err-io') : t('err-' + e));

let st = null;
let renderedIds = '';
const shown = new Set(); // message ids already animated in

function ascii(s) {
	return s.replace(/[^\x20-\x7e]/g, '').slice(0, MAX);
}

function timeText(ms) {
	const d = new Date(ms);
	const loc = zd.lang === 'zh' ? 'zh-CN' : 'en-US';
	const time = d.toLocaleTimeString(loc, { hour: '2-digit', minute: '2-digit' });
	const today = new Date().toDateString() === d.toDateString();
	return today ? `${t('today')} ${time}` : `${d.toLocaleDateString(loc, { month: 'short', day: 'numeric' })} ${time}`;
}

function renderStatus() {
	const s = st;
	const led = $('led');
	led.className = 'led';
	let text = t(s.state) || s.state;
	if (s.state === 'receiving') led.classList.add('on');
	else if (s.state === 'sending' || s.state === 'starting') led.classList.add('busy');
	else if (s.state === 'error') {
		led.classList.add('off');
		text = errText(s.error);
	}
	$('status').textContent = text;
	const ready = s.state === 'receiving' || s.state === 'sending';
	$('draft').disabled = !ready;
	updateSendButton();
	$('clear').disabled = !s.messages.length;
	$('retry').hidden = $('empty-retry').hidden = s.state !== 'error';
}

function updateSendButton() {
	const ready = st && (st.state === 'receiving' || st.state === 'sending');
	const v = $('draft').value;
	$('send').disabled = !ready || !v.length;
	$('counter').textContent = v.length ? `${v.length}/${MAX}` : '';
	$('counter').classList.toggle('full', v.length >= MAX);
}

function renderMessages() {
	const box = $('transcript');
	const list = st.messages;
	const key = list.map((m) => m.id + m.status).join(',');
	const err = st.state === 'error';
	$('empty').hidden = list.length > 0 && !err;
	box.hidden = err;
	if (err) {
		$('empty-glyph').textContent = 'exclamationmark_triangle';
		$('empty-title').textContent = t('noIr');
		$('empty-text').textContent = errText(st.error);
	} else {
		$('empty-glyph').textContent = 'bubble_left_bubble_right';
		$('empty-title').textContent = t('noMessages');
		$('empty-text').textContent = '';
	}
	if (key === renderedIds) return;
	const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
	renderedIds = key;
	box.textContent = '';
	let prev = null;
	list.forEach((m, i) => {
		if (!prev || m.time - prev.time > 5 * 60 * 1000) {
			const s = document.createElement('div');
			s.className = 'stamp';
			s.textContent = timeText(m.time);
			box.appendChild(s);
		}
		const el = document.createElement('div');
		el.className = 'msg' + (m.outgoing ? ' out' : '') + (m.status === 'sending' ? ' sending' : '');
		if (prev && prev.outgoing !== m.outgoing) el.classList.add('gap');
		if (shown.has(m.id)) el.style.animation = 'none';
		shown.add(m.id);
		const b = document.createElement('div');
		b.className = 'bubble';
		b.textContent = m.text;
		b.title = `#${m.seq}`;
		el.appendChild(b);
		const lastOut = m.outgoing && !list.slice(i + 1).some((x) => x.outgoing);
		if (m.outgoing && (m.status === 'failed' || lastOut)) {
			const meta = document.createElement('div');
			meta.className = 'meta';
			if (m.status === 'failed') {
				meta.classList.add('failed');
				meta.innerHTML = '<i class="zd-glyph">exclamationmark_circle</i><span></span> · <button type="button"></button>';
				meta.querySelector('span').textContent = t('failed');
				const r = meta.querySelector('button');
				r.textContent = t('resend');
				r.onclick = () => send(m.text);
			} else {
				meta.textContent = m.status === 'sending' ? t('sending') : t('sent');
			}
			el.appendChild(meta);
		}
		box.appendChild(el);
		prev = m;
	});
	if (atBottom || list.length && list[list.length - 1].outgoing) box.scrollTop = box.scrollHeight;
}

function row(box, k, v, bad, tip) {
	const r = document.createElement('div');
	r.className = 'zd-row';
	r.innerHTML = '<span></span><span class="zd-value"></span>';
	r.firstChild.textContent = k;
	r.lastChild.textContent = v;
	r.lastChild.title = tip ? `${v} · ${tip}` : v;
	if (bad) r.lastChild.classList.add('bad');
	box.appendChild(r);
}

function renderInspector() {
	if ($('panel').hidden) return;
	const s = st;
	const dev = (d) => (d ? d.path : t('none'));
	const link = $('rows-link');
	link.textContent = '';
	row(link, t('state'), s.state === 'error' ? errText(s.error) : t(s.state), s.state === 'error');
	row(link, t('receiver'), dev(s.rx), false, s.rx && s.rx.driver);
	row(link, t('transmitter'), dev(s.tx), false, s.tx && s.tx.driver);
	row(link, t('carrier'), Math.round(s.carrier / 1000) + ' kHz');
	row(link, t('protocol'), s.protocol);
	const tr = $('rows-traffic');
	tr.textContent = '';
	row(tr, t('received'), String(s.rx_count));
	row(tr, t('sent'), String(s.tx_count));
	row(tr, t('crc'), String(s.stats.crc_errors));
	row(tr, t('malformed'), String(s.stats.malformed));
}

function render() {
	if (!st) return;
	renderStatus();
	renderMessages();
	renderInspector();
}

async function send(text) {
	text = ascii(text);
	if (!text) return;
	try {
		const r = await fetch('api/send', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ text }),
		});
		if (!r.ok) {
			const e = await r.json().catch(() => ({}));
			$('status').textContent = errText(e.error);
		}
	} catch (_) {
		$('status').textContent = errText('io');
	}
}

$('draft').addEventListener('input', () => {
	const el = $('draft');
	const clean = ascii(el.value);
	if (clean !== el.value) el.value = clean;
	updateSendButton();
});
$('composer').onsubmit = (e) => {
	e.preventDefault();
	const v = $('draft').value;
	if (!v || $('send').disabled) return;
	$('draft').value = '';
	updateSendButton();
	send(v);
};
$('clear').onclick = () => fetch('api/clear', { method: 'POST' });
const retry = () => fetch('api/retry', { method: 'POST' });
$('retry').onclick = retry;
$('empty-retry').onclick = retry;
$('inspector').onclick = () => {
	const open = $('panel').hidden;
	$('panel').hidden = !open;
	$('inspector').setAttribute('aria-pressed', String(open));
	renderInspector();
};
// typing anywhere goes to the message field, like the original
document.addEventListener('keydown', (e) => {
	if (e.metaKey || e.ctrlKey || e.altKey || e.key.length !== 1) return;
	if (document.activeElement && /INPUT|TEXTAREA/.test(document.activeElement.tagName)) return;
	if (!$('draft').disabled) $('draft').focus();
});

zd.on('lang', () => {
	renderedIds = '';
	render();
});
updateSendButton();

function apply(s) {
	st = s;
	render();
}
// the stream's first event carries the state after the receiver opened
apply({ state: 'starting', messages: [], carrier: 38000, protocol: 'IR Chat v1', rx_count: 0, tx_count: 0, stats: { crc_errors: 0, malformed: 0 } });
const es = new EventSource('api/stream');
es.addEventListener('state', (e) => apply(JSON.parse(e.data)));
