'use strict';

const t = zd.localize({
	en: {
		title: 'NFC', history: 'History', pause: 'Pause Scanning', resume: 'Resume Scanning', clear: 'Clear History',
		reader: 'Reader', copy: 'Copy UID', copied: 'Copied', tag: 'Tag', ndef: 'NDEF', memory: 'Memory',
		scanning: 'Scanning', paused: 'Paused', starting: 'Starting…', absent: 'Cap not found', busy: 'In use',
		error: 'Reader error', off: 'Off',
		holdCard: 'Hold a Card Near the Cap', pausedTitle: 'Scanning Paused', resumeBtn: 'Resume',
		absentTitle: 'Cap CC1101 Not Found', busyTitle: 'Cap CC1101 in Use', busyText: 'Another app is using it',
		errorTitle: 'Reader Error', startingTitle: 'Starting Reader', retry: 'Try Again',
		inField: 'In Field', technology: 'Technology', type: 'Type', uid: 'UID', atqa: 'ATQA', sak: 'SAK',
		idm: 'IDm', pmm: 'PMm', version: 'Version', seen: 'Seen', capacity: 'Capacity', message: 'Message',
		access: 'Access', locked: 'Locked', notFormatted: 'Not formatted', bytes: 'bytes', pages: 'pages',
		status: 'Status', chip: 'Chip', identity: 'Identity', revision: 'Revision', bus: 'Bus', cs: 'Chip Select',
		irq: 'IRQ', polls: 'Polls', recoveries: 'Recoveries', text: 'Text', uri: 'URI', record: 'Record', empty: 'Empty',
	},
	zh: {
		title: 'NFC', history: '记录', pause: '暂停扫描', resume: '继续扫描', clear: '清除记录',
		reader: '读卡器', copy: '拷贝 UID', copied: '已拷贝', tag: '标签', ndef: 'NDEF', memory: '存储',
		scanning: '正在扫描', paused: '已暂停', starting: '正在启动…', absent: '未找到 Cap', busy: '使用中',
		error: '读卡器出错', off: '关闭',
		holdCard: '将卡片靠近 Cap', pausedTitle: '扫描已暂停', resumeBtn: '继续',
		absentTitle: '未找到 Cap CC1101', busyTitle: 'Cap CC1101 正在使用', busyText: '另一个应用正在使用',
		errorTitle: '读卡器出错', startingTitle: '正在启动读卡器', retry: '重试',
		inField: '在场', technology: '技术', type: '类型', uid: 'UID', atqa: 'ATQA', sak: 'SAK',
		idm: 'IDm', pmm: 'PMm', version: '版本', seen: '读取时间', capacity: '容量', message: '消息',
		access: '访问', locked: '已锁定', notFormatted: '未格式化', bytes: '字节', pages: '页',
		status: '状态', chip: '芯片', identity: '标识', revision: '修订版', bus: '总线', cs: '片选',
		irq: '中断', polls: '轮询', recoveries: '恢复', text: '文本', uri: 'URI', record: '记录', empty: '空',
	},
});

const $ = (id) => document.getElementById(id);
let reader = { state: 'off', scanning: true };
let tags = [];
let selected = 0; // session id
let pinned = false; // the user picked an older entry

function esc(s) {
	return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

function spaced(hex) {
	return (hex || '').match(/../g)?.join(' ') || '';
}

function when(ms) {
	const d = new Date(ms);
	const now = new Date();
	const time = d.toLocaleTimeString(zd.lang === 'zh' ? 'zh-CN' : 'en-GB', { hour: '2-digit', minute: '2-digit' });
	if (d.toDateString() === now.toDateString()) return time;
	const day = zd.lang === 'zh' ? `${d.getMonth() + 1}月${d.getDate()}日` : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	return `${day} ${time}`;
}

function listWhen(ms) {
	const d = new Date(ms);
	if (d.toDateString() === new Date().toDateString()) return when(ms);
	return zd.lang === 'zh' ? `${d.getMonth() + 1}月${d.getDate()}日` : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

function clock(ms) {
	return new Date(ms).toLocaleTimeString(zd.lang === 'zh' ? 'zh-CN' : 'en-GB');
}

function glyphFor(tag) {
	return tag.tech === 'NFC-F' ? 'radiowaves_right' : 'creditcard';
}

function row(label, value, cls = '') {
	return `<div class="zd-row ${cls}"><span class="label">${esc(label)}</span><span class="zd-value">${esc(value)}</span></div>`;
}

// ---- rendering ----

function renderStatus() {
	const s = reader.state;
	$('status').textContent = t(s === 'off' ? 'starting' : s);
	const live = s === 'scanning' || s === 'paused';
	$('scan').disabled = !live;
	$('scan-glyph').textContent = reader.scanning ? 'pause_fill' : 'play_fill';
	$('scan').title = t(reader.scanning ? 'pause' : 'resume');
	$('scan').setAttribute('aria-label', $('scan').title);
	$('clear').disabled = !tags.some((x) => !x.present);
}

function renderEmpty() {
	const s = reader.state;
	const cfg = {
		off: ['', 'startingTitle'],
		starting: ['', 'startingTitle'],
		scanning: ['dot_radiowaves_left_right', 'holdCard'],
		paused: ['pause_circle', 'pausedTitle', '', 'resumeBtn'],
		absent: ['exclamationmark_triangle', 'absentTitle', '', 'retry'],
		busy: ['hourglass', 'busyTitle', 'busyText', 'retry'],
		error: ['exclamationmark_triangle', 'errorTitle', '', 'retry'],
	}[s] || ['', 'startingTitle'];
	const [glyph, title, text, btn] = cfg;
	const pulse = $('pulse');
	pulse.classList.toggle('active', s === 'scanning');
	if (glyph) {
		pulse.innerHTML = `<i class="zd-glyph">${glyph}</i>`;
	} else if (!pulse.querySelector('.spinner')) {
		pulse.innerHTML = '<div class="spinner"></div>';
	}
	$('empty-title').textContent = t(title);
	$('empty-text').textContent = s === 'error' ? reader.detail || '' : text ? t(text) : '';
	$('empty-text').hidden = !$('empty-text').textContent;
	$('empty-btn').hidden = !btn;
	if (btn) $('empty-btn').textContent = t(btn);
}

function renderList() {
	const list = $('list');
	const have = new Map([...list.children].map((li) => [li.dataset.session, li]));
	const keep = new Set();
	let prev = null;
	for (const tag of tags) {
		const key = String(tag.session);
		keep.add(key);
		let li = have.get(key);
		if (!li) {
			li = document.createElement('li');
			li.dataset.session = key;
			li.setAttribute('role', 'option');
			li.onclick = () => select(tag.session, true);
		}
		li.innerHTML = `<i class="zd-glyph">${glyphFor(tag)}</i><span class="name">${esc(tag.type || tag.tech)}</span>` +
			(tag.present ? '<span class="dot"></span>' : `<span class="when">${esc(listWhen(tag.first))}</span>`);
		li.title = spaced(tag.uid);
		li.setAttribute('aria-selected', String(tag.session === selected));
		if (prev ? prev.nextSibling !== li : list.firstChild !== li) list.insertBefore(li, prev ? prev.nextSibling : list.firstChild);
		prev = li;
	}
	for (const [key, li] of have) if (!keep.has(key)) li.remove();
	$('win').classList.toggle('has-history', tags.length > 0);
}

function renderDetail() {
	const tag = tags.find((x) => x.session === selected);
	$('empty').hidden = !!tag;
	$('detail').hidden = !tag;
	if (!tag) {
		renderEmpty();
		return;
	}
	$('hero-glyph').textContent = glyphFor(tag);
	$('hero-type').textContent = tag.type || tag.tech;
	$('hero-uid').textContent = spaced(tag.uid);
	const badge = $('hero-badge');
	badge.classList.toggle('live', tag.present);
	badge.textContent = tag.present ? t('inField') : when(tag.last || tag.first);

	const rows = [row(t('technology'), tag.tech === 'NFC-F' ? 'NFC-F · FeliCa' : 'NFC-A · ISO 14443A'), row(t('type'), tag.type)];
	if (tag.atqa) rows.push(row(t('atqa'), spaced(tag.atqa)));
	if (tag.sak !== undefined && tag.sak !== null) rows.push(row(t('sak'), '0x' + tag.sak.toString(16).toUpperCase().padStart(2, '0')));
	if (tag.pmm) rows.push(row(t('pmm'), spaced(tag.pmm)));
	if (tag.version) rows.push(row(t('version'), spaced(tag.version)));
	const seen = tag.last && tag.last !== tag.first ? `${clock(tag.first)} – ${clock(tag.last)}` : clock(tag.first);
	rows.push(row(t('seen'), seen));
	$('tag-card').innerHTML = rows.join('');

	// NDEF: Type 2 tags only (the reader does not read NDEF from other types)
	const n = tag.ndef;
	const type2 = tag.tech === 'NFC-A' && tag.sak === 0;
	$('ndef-sec').hidden = !n && !type2;
	if (n || type2) {
		const r = [];
		if (!n) {
			r.push(row(t('message'), t('notFormatted')));
		} else {
			r.push(row(t('capacity'), `${n.capacity} ${t('bytes')}`));
			if (!n.readable) r.push(row(t('access'), t('locked')));
			else r.push(row(t('message'), n.size ? `${n.size} ${t('bytes')}` : t('empty')));
			if (n.error) r.push(row(t('message'), n.error));
			for (const rec of n.records || []) {
				let kind = rec.kind === 'text' ? t('text') + (rec.lang ? ' · ' + rec.lang : '') : rec.kind === 'uri' ? t('uri') : rec.type || t('record');
				const val = rec.value || spaced(rec.payload);
				r.push(`<div class="zd-row record"><span class="kind">${esc(kind)}</span><span class="val">${esc(val)}</span></div>`);
			}
		}
		$('ndef-card').innerHTML = r.join('');
	}

	const mem = tag.memory || '';
	$('mem-sec').hidden = !mem;
	if (mem) {
		const pages = mem.length / 8;
		$('mem-count').textContent = `${pages} ${t('pages')}`;
		const out = [];
		for (let p = 0; p < pages; p++) {
			const bytes = mem.slice(p * 8, p * 8 + 8).match(/../g);
			out.push(`<span class="pg">${p.toString(16).toUpperCase().padStart(2, '0')}</span>`);
			for (const b of bytes) out.push(`<span class="b${b === '00' ? ' zero' : ''}">${b}</span>`);
			const asc = bytes.map((b) => {
				const c = parseInt(b, 16);
				return c >= 0x20 && c < 0x7f ? String.fromCharCode(c) : '·';
			});
			out.push(`<span class="asc">${esc(asc.join(''))}</span>`);
		}
		$('memory').innerHTML = out.join('');
	}
}

function renderReader() {
	const c = reader.chip;
	const rows = [row(t('status'), $('status').textContent)];
	if (c) {
		rows.push(row(t('chip'), 'ST25R3916'));
		rows.push(row(t('identity'), '0x' + c.identity.toString(16).toUpperCase().padStart(2, '0')));
		rows.push(row(t('revision'), String(c.revision)));
	}
	if (reader.bus) rows.push(row(t('bus'), reader.bus.replace('/dev/', '')));
	if (reader.cs) rows.push(row(t('cs'), reader.cs));
	if (reader.irq) rows.push(row(t('irq'), reader.irq));
	rows.push(row(t('polls'), String(reader.polls || 0)));
	if (reader.recoveries) rows.push(row(t('recoveries'), String(reader.recoveries)));
	$('reader-card').innerHTML = rows.join('');
}

function render() {
	if (!tags.some((x) => x.session === selected)) {
		selected = tags.length ? tags[0].session : 0;
		pinned = false;
	}
	renderStatus();
	renderList();
	renderDetail();
	renderReader();
}

function select(session, byUser) {
	if (session !== selected) {
		selected = session;
		const d = $('detail');
		d.style.animation = 'none';
		void d.offsetWidth;
		d.style.animation = '';
	}
	if (byUser) pinned = session !== (tags[0] && tags[0].session);
	render();
}

// ---- actions ----

async function post(path, body) {
	await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
}

$('scan').onclick = () => {
	reader.scanning = !reader.scanning;
	renderStatus();
	post('api/scan', { on: reader.scanning });
};
$('clear').onclick = () => post('api/clear');
$('empty-btn').onclick = () => {
	if (reader.state === 'paused') {
		reader.scanning = true;
		post('api/scan', { on: true });
	} else {
		post('api/retry');
	}
};
$('inspector-btn').onclick = () => {
	const open = $('panel').hidden;
	$('panel').hidden = !open;
	$('inspector-btn').setAttribute('aria-pressed', String(open));
};
$('mem-toggle').onclick = () => {
	const open = $('memory').hidden;
	$('memory').hidden = !open;
	$('mem-toggle').setAttribute('aria-expanded', String(open));
};

function toast(text) {
	const el = document.createElement('div');
	el.className = 'toast';
	el.textContent = text;
	document.body.appendChild(el);
	setTimeout(() => {
		el.style.transition = 'opacity 200ms ease';
		el.style.opacity = '0';
		setTimeout(() => el.remove(), 220);
	}, 1100);
}

$('copy').onclick = async () => {
	const tag = tags.find((x) => x.session === selected);
	if (!tag) return;
	let ok = false;
	try {
		await navigator.clipboard.writeText(tag.uid);
		ok = true;
	} catch {
		const ta = document.createElement('textarea');
		ta.value = tag.uid;
		document.body.appendChild(ta);
		ta.select();
		try {
			ok = document.execCommand('copy');
		} catch {}
		ta.remove();
	}
	if (ok) toast(t('copied'));
};

document.addEventListener('keydown', (e) => {
	if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
	const i = tags.findIndex((x) => x.session === selected);
	const j = Math.max(0, Math.min(tags.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)));
	if (tags[j]) {
		e.preventDefault();
		select(tags[j].session, true);
	}
});

zd.on('lang', render);

// ---- data ----

function upsert(tag) {
	const i = tags.findIndex((x) => x.session === tag.session);
	if (i >= 0) tags[i] = tag;
	else {
		tags.unshift(tag);
		if (!pinned) selected = tag.session; // a new card takes the focus
	}
}

function apply(snapshot) {
	reader = snapshot.reader || reader;
	tags = snapshot.tags || [];
	render();
}

fetch('api/state')
	.then((r) => r.json())
	.then(apply)
	.catch(() => {});
const es = new EventSource('api/stream');
es.addEventListener('snapshot', (e) => apply(JSON.parse(e.data)));
es.addEventListener('reader', (e) => {
	reader = JSON.parse(e.data);
	render();
});
es.addEventListener('tag', (e) => {
	upsert(JSON.parse(e.data));
	render();
});
es.addEventListener('tags', (e) => {
	tags = JSON.parse(e.data) || [];
	render();
});
render();
