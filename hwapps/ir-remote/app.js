'use strict';

const t = zd.localize({
	en: {
		library: 'Library', all: 'All Buttons', unfiled: 'Unfiled', remotes: 'Remotes', newRemote: 'New Remote',
		learn: 'Learn Button', more: 'More', info: 'Info', send: 'Send', edit: 'Edit', delete: 'Delete', name: 'Name',
		remote: 'Remote', cancel: 'Cancel', save: 'Save', retry: 'Try Again', renameRemote: 'Rename Remote…',
		deleteRemote: 'Delete Remote…', renameTitle: 'Rename Remote', editTitle: 'Edit Button', create: 'Create',
		button1: '1 button', buttonN: '%n buttons', press: 'Press a Button', aim: 'Aim the remote at CardputerZero',
		unknownSignal: 'Unknown Signal', noButtons: 'No Buttons', noHardware: 'IR Unavailable',
		noTx: 'Transmitter unavailable', noRx: 'Receiver unavailable', protocol: 'Protocol', address: 'Address',
		command: 'Command', bits: 'Bits', carrier: 'Carrier', duty: 'Duty Cycle', duration: 'Duration', pulses: 'Pulses',
		source: 'Source', created: 'Created', unknown: 'Unknown', sent: 'Sent “%s”', deleteQ: 'Delete “%s”?',
		deleteRemoteQ: 'Delete “%s” and its buttons?', buttonName: 'Button %n', remoteName: 'Remote %n',
		'err-timeout': 'No Signal', 'err-overflow': 'Signal Too Long', 'err-io': 'Receive Failed',
		'err-no-receiver': 'Receiver Unavailable', 'err-no-transmitter': 'Transmitter Unavailable',
		'err-permission': 'No Permission for IR', 'err-busy': 'IR Busy', 'err-save': 'Could Not Save',
		'err-name': 'Name Required', 'err-not-found': 'Button Not Found',
	},
	zh: {
		library: '资料库', all: '全部按钮', unfiled: '未归类', remotes: '遥控器', newRemote: '新建遥控器',
		learn: '学习按钮', more: '更多', info: '信息', send: '发送', edit: '编辑', delete: '删除', name: '名称',
		remote: '遥控器', cancel: '取消', save: '存储', retry: '重试', renameRemote: '重命名遥控器…',
		deleteRemote: '删除遥控器…', renameTitle: '重命名遥控器', editTitle: '编辑按钮', create: '创建',
		button1: '1 个按钮', buttonN: '%n 个按钮', press: '按下遥控器按钮', aim: '将遥控器对准 CardputerZero',
		unknownSignal: '未知信号', noButtons: '没有按钮', noHardware: '红外不可用', noTx: '发射器不可用',
		noRx: '接收器不可用', protocol: '协议', address: '地址', command: '命令', bits: '位数', carrier: '载波',
		duty: '占空比', duration: '时长', pulses: '脉冲', source: '来源', created: '创建时间', unknown: '未知',
		sent: '已发送“%s”', deleteQ: '删除“%s”？', deleteRemoteQ: '删除“%s”及其所有按钮？', buttonName: '按钮 %n',
		remoteName: '遥控器 %n', 'err-timeout': '未收到信号', 'err-overflow': '信号过长', 'err-io': '接收失败',
		'err-no-receiver': '接收器不可用', 'err-no-transmitter': '发射器不可用', 'err-permission': '无权访问红外设备',
		'err-busy': '红外设备忙', 'err-save': '无法存储', 'err-name': '需要名称', 'err-not-found': '找不到按钮',
	},
});

const $ = (id) => document.getElementById(id);
const fmt = (key, s, n) => t(key).replace('%s', s ?? '').replace('%n', n ?? '');
const errText = (e) => t('err-' + e) === 'err-' + e ? t('err-io') : t('err-' + e);

let st = { signals: [], learn: { state: 'idle', timeout_ms: 15000 } };
let current = '*'; // '*' all, '' unfiled, else remote name
let selected = null; // signal id
const draftRemotes = new Set(); // created here, no buttons yet
const detailCache = new Map();

// ---- helpers ----

function remoteNames() {
	const s = new Set(draftRemotes);
	for (const x of st.signals) if (x.remote) s.add(x.remote);
	return [...s].sort((a, b) => a.localeCompare(b));
}

function visible() {
	if (current === '*') return st.signals;
	return st.signals.filter((x) => x.remote === current);
}

function hex(v) {
	return '0x' + v.toString(16).toUpperCase().padStart(2, '0');
}

function codeText(s) {
	if (!s.decoded) return t('unknown');
	return `${s.protocol} ${hex(s.address)}·${hex(s.command)}`;
}

function durText(us) {
	return us >= 1e6 ? (us / 1e6).toFixed(2) + ' s' : Math.round(us / 1000) + ' ms';
}

function countText(n) {
	return n === 1 ? t('button1') : fmt('buttonN', '', n);
}

const GLYPHS = [
	[/power|电源|开关|on\/off/i, 'power'],
	[/mute|静音/i, 'speaker_slash'],
	[/vol.*(\+|up)|音量\s*\+|音量加|大声/i, 'speaker_3'],
	[/vol.*(-|down)|音量\s*-|音量减|小声/i, 'speaker_1'],
	[/^(ch|channel|频道).*(\+|up)|频道加/i, 'plus_circle'],
	[/^(ch|channel|频道).*(-|down)|频道减/i, 'minus_circle'],
	[/\bup\b|上$/i, 'chevron_up'],
	[/\bdown\b|下$/i, 'chevron_down'],
	[/\bleft\b|左/i, 'chevron_left'],
	[/\bright\b|右/i, 'chevron_right'],
	[/\bok\b|enter|select|确定|确认/i, 'checkmark_circle'],
	[/play|播放/i, 'play_fill'],
	[/pause|暂停/i, 'pause_fill'],
	[/stop|停止/i, 'stop_fill'],
	[/home|主页/i, 'house'],
	[/back|return|返回/i, 'arrow_uturn_left'],
	[/menu|菜单/i, 'list_bullet'],
	[/light|lamp|灯/i, 'lightbulb'],
	[/fan|wind|风/i, 'wind'],
	[/cool|制冷|冷/i, 'snow'],
	[/heat|制热|热/i, 'sun_max'],
	[/temp|温度/i, 'thermometer'],
	[/tv|电视/i, 'tv'],
	[/^\d+$/, 'number'],
];

function glyphFor(name) {
	for (const [re, g] of GLYPHS) if (re.test(name)) return g;
	return 'dot_radiowaves_right';
}

// waveform: long gaps compressed so short codes stay readable
function drawWave(svg, raw) {
	svg.textContent = '';
	if (!raw || !raw.length) return;
	const cap = 6000;
	let total = 0;
	for (const v of raw) total += Math.min(Math.abs(v), cap);
	const w = 300;
	const h = 40;
	svg.setAttribute('viewBox', `0 0 ${w} ${h}`);
	let x = 0;
	let d = `M0 ${h}`;
	for (const v of raw) {
		const y = v > 0 ? 2 : h;
		const nx = x + (Math.min(Math.abs(v), cap) / total) * w;
		d += ` L${x.toFixed(2)} ${y} L${nx.toFixed(2)} ${y}`;
		x = nx;
	}
	d += ` L${w} ${h}`;
	const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	p.setAttribute('d', d);
	svg.appendChild(p);
}

async function api(path, opts = {}) {
	const o = { method: opts.method || 'GET' };
	if (opts.body !== undefined) {
		o.body = JSON.stringify(opts.body);
		o.headers = { 'Content-Type': 'application/json' };
	}
	const r = await fetch('api/' + path, o);
	const ct = r.headers.get('Content-Type') || '';
	const data = ct.includes('json') ? await r.json() : null;
	if (!r.ok) throw new Error((data && data.error) || 'io');
	return data;
}

let toastTimer = 0;
function toast(text) {
	const el = $('toast');
	el.textContent = text;
	el.hidden = false;
	clearTimeout(toastTimer);
	toastTimer = setTimeout(() => (el.hidden = true), 2200);
}

// ---- rendering ----

function renderSidebar() {
	const names = remoteNames();
	const box = $('remotes');
	box.textContent = '';
	for (const n of names) {
		const b = document.createElement('button');
		b.className = 'place';
		b.dataset.remote = n;
		const c = st.signals.filter((x) => x.remote === n).length;
		b.innerHTML = '<i class="zd-glyph">tv</i><span></span><em class="count"></em>';
		b.querySelector('span').textContent = n;
		b.querySelector('em').textContent = c || '';
		box.appendChild(b);
	}
	const unfiled = st.signals.filter((x) => !x.remote).length;
	$('place-none').hidden = !unfiled || !names.length;
	$('count-none').textContent = unfiled || '';
	$('count-all').textContent = st.signals.length || '';
	if (current !== '*' && !(current === '' ? unfiled && names.length : names.includes(current))) current = '*';
	for (const el of document.querySelectorAll('.place[data-remote]')) {
		el.classList.toggle('active', el.dataset.remote === current);
	}
	// narrow-window picker
	const pk = $('picker');
	pk.textContent = '';
	const opt = (v, label) => {
		const o = document.createElement('option');
		o.value = v;
		o.textContent = label;
		pk.appendChild(o);
	};
	opt('*', t('all'));
	if (!$('place-none').hidden) opt('', t('unfiled'));
	for (const n of names) opt(n, n);
	pk.value = current;
	const dl = $('remote-list');
	dl.textContent = '';
	for (const n of names) {
		const o = document.createElement('option');
		o.value = n;
		dl.appendChild(o);
	}
}

function renderGrid() {
	const list = visible();
	const grid = $('grid');
	const title = current === '*' ? t('all') : current === '' ? t('unfiled') : current;
	$('title').textContent = title;
	let sub = countText(list.length);
	if (!st.tx && st.tx_error) sub = t('noTx');
	$('subtitle').textContent = sub;
	$('more').disabled = current === '*' || current === '';
	$('learn').disabled = !st.rx;

	const hw = st.rx || st.tx;
	const empty = !list.length;
	$('empty').hidden = !empty;
	grid.hidden = empty;
	if (empty) {
		$('empty-glyph').textContent = hw ? 'dot_radiowaves_left_right' : 'exclamationmark_triangle';
		$('empty-title').textContent = hw ? t('noButtons') : t('noHardware');
		$('empty-text').textContent = hw ? '' : errText(st.rx_error || 'no-receiver');
		$('empty-btn').hidden = !st.rx;
		$('empty-btn').textContent = t('learn');
	}
	const keep = new Map([...grid.children].map((el) => [el.dataset.id, el]));
	grid.textContent = '';
	for (const s of list) {
		let b = keep.get(s.id);
		if (!b) {
			b = document.createElement('button');
			b.className = 'key';
			b.dataset.id = s.id;
			b.innerHTML = '<span class="dot"><i class="zd-glyph"></i></span><span class="label"><b></b><small></small></span>';
		}
		b.querySelector('.zd-glyph').textContent = glyphFor(s.name);
		b.querySelector('b').textContent = s.name;
		b.querySelector('small').textContent = current === '*' && s.remote ? s.remote : codeText(s);
		b.title = `${s.name} — ${codeText(s)}`;
		b.classList.toggle('selected', s.id === selected);
		grid.appendChild(b);
	}
}

async function renderInspector() {
	const s = st.signals.find((x) => x.id === selected);
	const panelOpen = !$('panel').hidden;
	$('i-send').disabled = !s || !st.tx;
	$('i-edit').disabled = $('i-delete').disabled = !s;
	if (!s) {
		$('i-name').textContent = '–';
		$('i-rows').textContent = '';
		drawWave($('wave'), null);
		return;
	}
	$('i-name').textContent = s.name;
	const rows = [
		[t('remote'), s.remote || t('unfiled')],
		[t('protocol'), s.decoded ? s.protocol : t('unknown')],
	];
	if (s.decoded) rows.push([t('address'), hex(s.address)], [t('command'), hex(s.command)], [t('bits'), String(s.bits)]);
	rows.push(
		[t('carrier'), Math.round(s.carrier / 1000) + ' kHz'],
		[t('duty'), s.duty + '%'],
		[t('duration'), durText(s.duration_us)],
		[t('pulses'), String(s.pulses)],
	);
	if (s.driver) rows.push([t('source'), s.driver]);
	if (s.created) rows.push([t('created'), new Date(s.created * 1000).toLocaleString(zd.lang === 'zh' ? 'zh-CN' : 'en-US', { dateStyle: 'medium', timeStyle: 'short' })]);
	const box = $('i-rows');
	box.textContent = '';
	for (const [k, v] of rows) {
		const r = document.createElement('div');
		r.className = 'zd-row';
		r.innerHTML = '<span></span><span class="zd-value"></span>';
		r.firstChild.textContent = k;
		r.lastChild.textContent = v;
		r.lastChild.title = v;
		box.appendChild(r);
	}
	if (!panelOpen) return;
	let raw = detailCache.get(s.id);
	if (!raw) {
		try {
			raw = (await api('signals/' + encodeURIComponent(s.id))).raw;
			detailCache.set(s.id, raw);
		} catch (_) {
			raw = null;
		}
	}
	if (selected === s.id) drawWave($('wave'), raw);
}

function render() {
	renderSidebar();
	renderGrid();
	renderInspector();
}

function setSignals(list) {
	st.signals = list || [];
	const ids = new Set(st.signals.map((s) => s.id));
	for (const k of [...detailCache.keys()]) if (!ids.has(k)) detailCache.delete(k);
	for (const s of st.signals) if (s.remote) draftRemotes.delete(s.remote);
	if (selected && !ids.has(selected)) selected = null;
	render();
}

// ---- actions ----

function select(id) {
	selected = id;
	for (const el of $('grid').children) el.classList.toggle('selected', el.dataset.id === id);
	renderInspector();
}

async function sendSignal(id) {
	const s = st.signals.find((x) => x.id === id);
	if (!s) return;
	const el = [...$('grid').children].find((e) => e.dataset.id === id);
	el && el.classList.add('sending');
	try {
		await api('signals/' + encodeURIComponent(id) + '/send', { method: 'POST' });
		toast(fmt('sent', s.name));
	} catch (e) {
		toast(errText(e.message));
		if (el) {
			el.classList.add('failed');
			setTimeout(() => el.classList.remove('failed'), 1200);
		}
	} finally {
		setTimeout(() => el && el.classList.remove('sending'), 180);
	}
}

$('grid').addEventListener('click', (e) => {
	const b = e.target.closest('.key');
	if (!b) return;
	select(b.dataset.id);
	sendSignal(b.dataset.id);
});
$('grid').addEventListener('contextmenu', (e) => {
	const b = e.target.closest('.key');
	if (!b) return;
	e.preventDefault();
	select(b.dataset.id);
	openInspector(true);
});
$('grid').addEventListener('keydown', (e) => {
	const keys = [...$('grid').children];
	const i = keys.indexOf(document.activeElement);
	if (i < 0) return;
	const cols = Math.max(1, Math.round($('grid').clientWidth / (keys[0].offsetWidth + 12)));
	const step = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: cols, ArrowUp: -cols }[e.key];
	if (step) {
		e.preventDefault();
		const n = keys[Math.max(0, Math.min(keys.length - 1, i + step))];
		n.focus();
		select(n.dataset.id);
	} else if ((e.key === 'Backspace' || e.key === 'Delete') && selected) {
		e.preventDefault();
		confirmDeleteSignal();
	}
});

for (const el of document.querySelectorAll('.place[data-remote]')) el.onclick = () => go(el.dataset.remote);
$('remotes').addEventListener('click', (e) => {
	const b = e.target.closest('.place');
	if (b) go(b.dataset.remote);
});
$('picker').onchange = (e) => go(e.target.value);

function go(remote) {
	current = remote;
	closeMenu();
	render();
}

function openInspector(open) {
	$('panel').hidden = !open;
	$('inspector').setAttribute('aria-pressed', String(open));
	if (open) renderInspector();
}
$('inspector').onclick = () => openInspector($('panel').hidden);
$('i-send').onclick = () => selected && sendSignal(selected);
$('i-edit').onclick = () => selected && editSignal();
$('i-delete').onclick = () => selected && confirmDeleteSignal();

// menu
function closeMenu() {
	$('menu').hidden = true;
}
$('more').onclick = (e) => {
	e.stopPropagation();
	$('menu').hidden = !$('menu').hidden;
};
document.addEventListener('click', (e) => {
	if (!e.target.closest('#menu')) closeMenu();
});
$('m-rename').onclick = () => {
	closeMenu();
	const old = current;
	openEdit(t('renameTitle'), { name: old }, false, async (v) => {
		if (!v.name || v.name === old) return;
		if (st.signals.some((s) => s.remote === old)) {
			setSignals(await api('remotes', { method: 'POST', body: { from: old, to: v.name } }));
		} else {
			draftRemotes.delete(old);
			draftRemotes.add(v.name);
		}
		current = v.name;
		render();
	});
};
$('m-delete').onclick = () => {
	closeMenu();
	const old = current;
	confirmSheet(fmt('deleteRemoteQ', old), async () => {
		draftRemotes.delete(old);
		current = '*';
		if (st.signals.some((s) => s.remote === old)) {
			setSignals(await api('remotes', { method: 'POST', body: { from: old, delete: true } }));
		} else render();
	});
};
$('new-remote').onclick = () => {
	const names = remoteNames();
	let n = names.length + 1;
	while (names.includes(fmt('remoteName', '', n))) n++;
	openEdit(t('newRemote'), { name: fmt('remoteName', '', n) }, false, async (v) => {
		if (!v.name) return;
		draftRemotes.add(v.name);
		current = v.name;
		render();
	}, t('create'));
};

// edit sheet
let editDone = null;
function openEdit(title, v, withRemote, done, okLabel) {
	$('e-title').textContent = title;
	$('e-name').value = v.name || '';
	$('e-remote').value = v.remote || '';
	$('e-remote-row').hidden = !withRemote;
	$('e-ok').textContent = okLabel || t('save');
	editDone = done;
	$('edit-sheet').hidden = false;
	setTimeout(() => $('e-name').select(), 50);
}
function closeEdit() {
	$('edit-sheet').hidden = true;
	editDone = null;
}
$('e-cancel').onclick = closeEdit;
$('e-form').onsubmit = async (e) => {
	e.preventDefault();
	const fn = editDone;
	const v = { name: $('e-name').value.trim(), remote: $('e-remote').value.trim() };
	if (!v.name) return $('e-name').focus();
	closeEdit();
	try {
		await fn(v);
	} catch (err) {
		toast(errText(err.message));
	}
};

function editSignal() {
	const s = st.signals.find((x) => x.id === selected);
	if (!s) return;
	openEdit(t('editTitle'), s, true, async (v) => {
		const list = await api('signals/' + encodeURIComponent(s.id), { method: 'PATCH', body: v });
		if (current !== '*' && v.remote !== current) current = v.remote || '*';
		setSignals(list);
	});
}

// confirm sheet
let confirmDone = null;
function confirmSheet(title, done) {
	$('c-title').textContent = title;
	confirmDone = done;
	$('confirm-sheet').hidden = false;
}
$('c-cancel').onclick = () => ($('confirm-sheet').hidden = true);
$('c-ok').onclick = async () => {
	$('confirm-sheet').hidden = true;
	try {
		await confirmDone();
	} catch (err) {
		toast(errText(err.message));
	}
};

function confirmDeleteSignal() {
	const s = st.signals.find((x) => x.id === selected);
	if (!s) return;
	confirmSheet(fmt('deleteQ', s.name), async () => {
		setSignals(await api('signals/' + encodeURIComponent(s.id), { method: 'DELETE' }));
	});
}

// ---- learn ----

let learnOpen = false;
let ringTimer = 0;

async function startLearn() {
	closeMenu();
	learnOpen = true;
	$('learn-sheet').hidden = false;
	st.learn = { state: 'waiting', started: Date.now(), timeout_ms: st.learn.timeout_ms || 15000 };
	renderLearn();
	try {
		const s = await api('learn', { method: 'POST' });
		if (learnOpen) {
			st.learn = s.learn;
			renderLearn();
		}
	} catch (e) {
		st.learn = { state: 'error', error: e.message };
		renderLearn();
	}
}

function closeLearn() {
	learnOpen = false;
	$('learn-sheet').hidden = true;
	clearInterval(ringTimer);
	api('learn/cancel', { method: 'POST' }).catch(() => {});
}

function learnButton(label, cls, fn, type) {
	const b = document.createElement('button');
	b.className = 'zd-button' + (cls ? ' ' + cls : '');
	b.textContent = label;
	b.type = type || 'button';
	b.onclick = fn;
	return b;
}

function renderLearn() {
	if (!learnOpen) return;
	const l = st.learn;
	const waiting = l.state === 'waiting';
	const captured = l.state === 'captured';
	$('l-wait').hidden = captured;
	$('l-wait').classList.toggle('error', l.state === 'error');
	$('l-wavebox').hidden = !captured;
	$('l-form').hidden = !captured;
	clearInterval(ringTimer);
	const ring = () => {
		const left = waiting ? Math.max(0, 1 - (Date.now() - l.started) / l.timeout_ms) : 0;
		$('l-ring').style.strokeDasharray = `${(left * 100).toFixed(1)} 100`;
	};
	ring();
	if (waiting) ringTimer = setInterval(ring, 250);
	const btns = $('l-buttons');
	btns.textContent = '';
	if (waiting) {
		$('l-title').textContent = t('press');
		$('l-text').textContent = t('aim');
		btns.append(learnButton(t('cancel'), '', closeLearn));
	} else if (l.state === 'error') {
		$('l-title').textContent = errText(l.error);
		$('l-text').textContent = '';
		btns.append(learnButton(t('cancel'), '', closeLearn), learnButton(t('retry'), 'primary', startLearn));
	} else if (captured) {
		const d = l.decode || {};
		$('l-title').textContent = d.decoded ? `${d.protocol} · ${hex(d.address)} · ${hex(d.command)}` : t('unknownSignal');
		$('l-text').textContent = `${l.pulses} ${t('pulses').toLowerCase()} · ${durText(l.duration_us)}`;
		drawWave($('l-wave'), l.raw);
		const remote = current === '*' || current === '' ? '' : current;
		if (!$('l-form').dataset.filled) {
			const n = st.signals.filter((s) => s.remote === remote).length + 1;
			$('l-name').value = fmt('buttonName', '', n);
			$('l-remote').value = remote;
			$('l-form').dataset.filled = '1';
			setTimeout(() => $('l-name').select(), 50);
		}
		btns.append(
			learnButton(t('cancel'), '', closeLearn),
			learnButton(t('retry'), '', () => {
				delete $('l-form').dataset.filled;
				startLearn();
			}),
			learnButton(t('save'), 'primary', saveLearned),
		);
	}
	if (!captured) delete $('l-form').dataset.filled;
}

async function saveLearned() {
	const name = $('l-name').value.trim();
	if (!name) return $('l-name').focus();
	const remote = $('l-remote').value.trim();
	try {
		const r = await api('learn/save', { method: 'POST', body: { name, remote } });
		learnOpen = false;
		$('learn-sheet').hidden = true;
		delete $('l-form').dataset.filled;
		if (current !== '*' && remote !== current) current = remote || '*';
		selected = r.id;
		setSignals(r.signals);
	} catch (e) {
		toast(errText(e.message));
	}
}
$('l-form').onsubmit = (e) => {
	e.preventDefault();
	saveLearned();
};

$('learn').onclick = startLearn;
$('empty-btn').onclick = startLearn;

document.addEventListener('keydown', (e) => {
	if (e.key !== 'Escape') return;
	if (learnOpen) closeLearn();
	else if (!$('edit-sheet').hidden) closeEdit();
	else if (!$('confirm-sheet').hidden) $('confirm-sheet').hidden = true;
	else closeMenu();
});

zd.on('lang', render);

// ---- data ----

function applyState(s) {
	st = Object.assign(st, s);
	setSignals(s.signals);
	if (learnOpen && s.learn && s.learn.state !== 'idle') renderLearn();
}

fetch('api/state')
	.then((r) => r.json())
	.then(applyState)
	.catch(() => {
		st.rx_error = 'io';
		render();
	});
const es = new EventSource('api/stream');
es.addEventListener('state', (e) => applyState(JSON.parse(e.data)));
es.addEventListener('signals', (e) => setSignals(JSON.parse(e.data)));
es.addEventListener('learn', (e) => {
	st.learn = JSON.parse(e.data);
	renderLearn();
});
render();
