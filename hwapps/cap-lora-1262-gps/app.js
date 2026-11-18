'use strict';

const t = zd.localize({
	en: {
		title: 'GPS', copy: 'Copy Coordinates', details: 'Details', latitude: 'Latitude', longitude: 'Longitude',
		altitude: 'Altitude', speed: 'Speed', course: 'Course', sky: 'Sky', signal: 'Signal', inFix: 'In fix',
		tracked: 'Tracked', fix: 'Fix', receiver: 'Receiver', retry: 'Try Again', starting: 'Starting',
		nodata: 'No data', searching: 'Searching', stale: 'Signal lost', error: 'Unavailable', stopped: 'Stopped',
		fix3d: '3D Fix', fix2d: '2D Fix', satsIn: '{u} of {v} satellites', unavailableTitle: 'GPS Unavailable',
		noDataTitle: 'No GPS Data', noDataText: 'Attach the Cap LoRa-1262', fixType: 'Fix type', quality: 'Quality',
		hdop: 'HDOP', pdop: 'PDOP', vdop: 'VDOP', satellites: 'Satellites', module: 'Module', protocol: 'Protocol',
		port: 'Port', baud: 'Baud', sentences: 'Sentences', invalid: 'Invalid', bytes: 'Bytes', last: 'Last',
		dataAge: 'Data age', fixAge: 'Fix age', none: 'None', gps: 'GPS', dgps: 'DGPS', rtk: 'RTK', float: 'Float RTK',
		est: 'Estimated',
	},
	zh: {
		title: 'GPS', copy: '拷贝坐标', details: '详细信息', latitude: '纬度', longitude: '经度',
		altitude: '海拔', speed: '速度', course: '航向', sky: '天空', signal: '信号', inFix: '参与定位',
		tracked: '已跟踪', fix: '定位', receiver: '接收机', retry: '重试', starting: '正在启动',
		nodata: '无数据', searching: '正在搜索', stale: '信号丢失', error: '不可用', stopped: '已停止',
		fix3d: '3D 定位', fix2d: '2D 定位', satsIn: '{u}/{v} 颗卫星', unavailableTitle: 'GPS 不可用',
		noDataTitle: '没有 GPS 数据', noDataText: '请装上 Cap LoRa-1262', fixType: '定位类型', quality: '质量',
		hdop: 'HDOP', pdop: 'PDOP', vdop: 'VDOP', satellites: '卫星', module: '模块', protocol: '协议',
		port: '端口', baud: '波特率', sentences: '语句', invalid: '无效', bytes: '字节', last: '最近',
		dataAge: '数据时效', fixAge: '定位时效', none: '无', gps: 'GPS', dgps: 'DGPS', rtk: 'RTK', float: '浮点 RTK',
		est: '推算',
	},
});

const $ = (id) => document.getElementById(id);
const NS = 'http://www.w3.org/2000/svg';
const SYS = { 1: 'G', 2: 'R', 3: 'E', 4: 'C', 5: 'J', 6: 'I' };
let state = null;

function svg(tag, attrs, parent) {
	const e = document.createElementNS(NS, tag);
	for (const k in attrs) e.setAttribute(k, attrs[k]);
	parent.appendChild(e);
	return e;
}

function el(tag, cls, text) {
	const e = document.createElement(tag);
	if (cls) e.className = cls;
	if (text !== undefined) e.textContent = text;
	return e;
}

const num = (v, d) => (v == null ? '–' : v.toFixed(d));

function hemi(v, lat) {
	return lat ? (v >= 0 ? 'N' : 'S') : v >= 0 ? 'E' : 'W';
}

function dms(v, lat) {
	const a = Math.abs(v);
	let d = Math.floor(a);
	let m = Math.floor((a - d) * 60);
	let s = ((a - d) * 60 - m) * 60;
	if (s >= 59.95) {
		s = 0;
		m += 1;
	}
	if (m >= 60) {
		m = 0;
		d += 1;
	}
	return `${d}°${String(m).padStart(2, '0')}′${s.toFixed(1).padStart(4, '0')}″ ${hemi(v, lat)}`;
}

function age(ms) {
	if (ms == null) return '–';
	return ms < 1000 ? '<1 s' : Math.floor(ms / 1000) + ' s';
}

// ---- sky plot ----

function buildSky() {
	const g = $('sky-grid');
	g.textContent = '';
	svg('circle', { r: 100, class: 'ring horizon' }, g);
	svg('circle', { r: 66.7, class: 'ring' }, g);
	svg('circle', { r: 33.3, class: 'ring' }, g);
	svg('line', { x1: -100, y1: 0, x2: 100, y2: 0, class: 'axis' }, g);
	svg('line', { x1: 0, y1: -100, x2: 0, y2: 100, class: 'axis' }, g);
	const dirs = zd.lang === 'zh' ? ['北', '东', '南', '西'] : ['N', 'E', 'S', 'W'];
	[
		[0, -105],
		[106, 0],
		[0, 106],
		[-106, 0],
	].forEach(([x, y], i) => {
		const txt = svg('text', { x, y, class: i === 0 ? 'dir north' : 'dir', 'text-anchor': 'middle', 'dominant-baseline': 'central' }, g);
		txt.textContent = dirs[i];
	});
	for (const [r, e] of [
		[66.7, 30],
		[33.3, 60],
	]) {
		const txt = svg('text', { x: 2, y: -r - 2, class: 'elev' }, g);
		txt.textContent = e + '°';
	}
}

function renderSky(sats) {
	const g = $('sky-sats');
	const seen = new Set();
	for (const s of sats) {
		if (s.elevation == null || s.azimuth == null || s.elevation < 0) continue;
		const id = `${s.system}-${s.prn}`;
		seen.add(id);
		let node = g.querySelector(`[data-id="${id}"]`);
		if (!node) {
			node = svg('g', { 'data-id': id }, g);
			svg('circle', { r: 6 }, node);
			svg('text', { 'text-anchor': 'middle', 'dominant-baseline': 'central' }, node);
		}
		const r = ((90 - s.elevation) / 90) * 100;
		const a = (s.azimuth * Math.PI) / 180;
		const x = (Math.sin(a) * r).toFixed(1);
		const y = (-Math.cos(a) * r).toFixed(1);
		node.setAttribute('class', 'sat' + (s.used ? ' used' : '') + (s.snr == null ? ' weak' : ''));
		const c = node.firstChild;
		c.setAttribute('cx', x);
		c.setAttribute('cy', y);
		const txt = node.lastChild;
		txt.setAttribute('x', x);
		txt.setAttribute('y', (+y + 11).toFixed(1));
		txt.textContent = (SYS[s.system] || '') + s.prn;
	}
	for (const node of [...g.children]) if (!seen.has(node.dataset.id)) node.remove();
}

function renderBars(sats) {
	const box = $('bars');
	const fit = Math.max(1, Math.floor((box.clientWidth + 4) / 16));
	const list = sats
		.filter((s) => s.snr != null)
		.sort((a, b) => b.used - a.used || b.snr - a.snr)
		.slice(0, fit)
		.sort((a, b) => a.system - b.system || a.prn - b.prn);
	while (box.childElementCount > list.length) box.lastChild.remove();
	while (box.childElementCount < list.length) {
		const b = el('div', 'bar');
		b.appendChild(el('div', 'fill'));
		b.appendChild(el('span', 'prn'));
		box.appendChild(b);
	}
	list.forEach((s, i) => {
		const b = box.children[i];
		b.className = 'bar' + (s.used ? ' used' : '');
		b.title = `${SYS[s.system] || ''}${s.prn} · ${s.snr} dB-Hz`;
		b.firstChild.style.height = Math.max(4, Math.min(100, (s.snr / 50) * 100)) + '%';
		b.lastChild.textContent = (SYS[s.system] || '') + s.prn;
	});
}

// ---- main ----

function statusLabel(s) {
	if (s.state === 'fixed') return s.fix_type >= 3 ? t('fix3d') : t('fix2d');
	return t(s.state);
}

function render() {
	const s = state;
	if (!s) return;
	const st = $('status');
	const cls = { fixed: 'ok', searching: 'busy', starting: 'busy', stale: 'busy', nodata: 'bad', error: 'bad' }[s.state] || '';
	st.className = 'zd-subtitle status ' + cls;
	let label = statusLabel(s);
	if (s.state === 'fixed' || s.state === 'searching') label += ' · ' + t('satsIn').replace('{u}', s.sats_used).replace('{v}', s.sats_visible);
	$('status-text').textContent = label;

	// full-window states: receiver error, or no data at all
	const noData = s.state === 'nodata' && (s.data_age_ms || 0) > 3000;
	const empty = s.state === 'error' || noData;
	$('empty').hidden = !empty;
	if (empty) {
		$('empty-title').textContent = s.state === 'error' ? t('unavailableTitle') : t('noDataTitle');
		$('empty-text').textContent = s.state === 'error' ? s.error || '' : t('noDataText');
		$('retry').hidden = s.state !== 'error';
	}

	const pos = s.lat != null && s.lon != null;
	$('main').classList.toggle('stale', s.state !== 'fixed');
	$('lat').textContent = pos ? `${Math.abs(s.lat).toFixed(6)}° ${hemi(s.lat, true)}` : '–';
	$('lon').textContent = pos ? `${Math.abs(s.lon).toFixed(6)}° ${hemi(s.lon, false)}` : '–';
	$('lat-dms').textContent = pos ? dms(s.lat, true) : '';
	$('lon-dms').textContent = pos ? dms(s.lon, false) : '';
	$('copy').disabled = !pos;
	$('alt').textContent = num(s.altitude, 1);
	$('speed').textContent = num(s.speed_kph, 1);
	$('course').textContent = s.course == null ? '–' : Math.round(s.course) + '°';
	$('course-arrow').style.transform = `rotate(${s.course || 0}deg)`;
	$('course-arrow').hidden = s.course == null;
	$('utc').textContent = s.time || '–';
	$('date').textContent = s.date || '';
	$('sats').textContent = `${s.sats_used} / ${s.sats_visible}`;
	renderSky(s.satellites);
	renderBars(s.satellites);
	renderInspector();
}

function rows(card, list) {
	if (card.childElementCount !== list.length) {
		card.textContent = '';
		for (let i = 0; i < list.length; i++) {
			const row = el('div', 'zd-row');
			row.appendChild(el('span', ''));
			row.appendChild(el('span', 'zd-value'));
			card.appendChild(row);
		}
	}
	list.forEach(([k, v], i) => {
		const row = card.children[i];
		row.firstChild.textContent = k.startsWith('=') ? k.slice(1) : t(k);
		row.lastChild.textContent = v;
		row.lastChild.title = v;
	});
}

function renderInspector() {
	if ($('panel').hidden) return;
	const s = state;
	const fixType = s.state === 'fixed' ? (s.fix_type >= 3 ? '3D' : '2D') : t('none');
	const q = ['none', 'gps', 'dgps', 'none', 'rtk', 'float', 'est'][s.fix_quality] || String(s.fix_quality);
	rows($('fix'), [
		['fixType', fixType],
		['quality', t(q)],
		['satellites', `${s.sats_used} / ${s.sats_visible}`],
		['hdop', num(s.hdop, 2)],
		['pdop', num(s.pdop, 2)],
		['vdop', num(s.vdop, 2)],
	]);
	rows($('receiver'), [
		['module', s.module],
		['protocol', s.protocol],
		['port', s.device],
		['baud', String(s.baud)],
	]);
	rows($('nmea'), [
		['sentences', String(s.sentences)],
		['invalid', String(s.invalid)],
		['bytes', String(s.bytes)],
		['last', s.last_sentence ? s.talker + s.last_sentence : '–'],
		['dataAge', age(s.data_age_ms)],
		['fixAge', age(s.fix_age_ms)],
	]);
}

$('inspector').onclick = () => {
	const open = $('panel').hidden;
	$('panel').hidden = !open;
	$('inspector').setAttribute('aria-pressed', String(open));
	render();
};

$('copy').onclick = () => {
	if (!state || state.lat == null) return;
	const text = `${state.lat.toFixed(6)}, ${state.lon.toFixed(6)}`;
	const ta = document.createElement('textarea');
	ta.value = text;
	ta.style.position = 'fixed';
	ta.style.opacity = '0';
	document.body.appendChild(ta);
	ta.select();
	try {
		document.execCommand('copy');
	} catch (_) {}
	ta.remove();
	if (navigator.clipboard) navigator.clipboard.writeText(text).catch(() => {});
};

$('retry').onclick = () => fetch('api/retry', { method: 'POST' });

new ResizeObserver(() => state && renderBars(state.satellites)).observe($('bars'));

// hairline under the toolbar once the content scrolls
$('main').addEventListener('scroll', () => {
	document.querySelector('.zd-toolbar').classList.toggle('bordered', $('main').scrollTop > 0);
});

zd.on('lang', () => {
	buildSky();
	render();
});
buildSky();

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
