'use strict';

const t = zd.localize({
	en: {
		title: 'Camera', back: 'Back', info: 'Info', download: 'Download', delete: 'Delete', settings: 'Settings',
		lowLight: 'Low light', retry: 'Try Again', unreadable: "Can't Open", photo: 'Photo', video: 'Video',
		takePhoto: 'Take Photo', record: 'Record', stop: 'Stop', cameraInfo: 'Camera', photoSize: 'Photo Size',
		timer: 'Timer', off: 'Off', sound: 'Shutter Sound', source: 'Camera', builtIn: 'Built-in', cancel: 'Cancel',
		starting: 'Starting', saving: 'Saving', inUse: 'Camera in Use', noCamera: 'No Camera', error: 'Camera Error',
		deletePhoto: 'Delete Photo?', deleteVideo: 'Delete Video?', dims: 'Dimensions', size: 'Size', date: 'Date',
		duration: 'Duration', frames: 'Frames', exposure: 'Exposure', aperture: 'Aperture', focal: 'Focal Length',
		iso: 'ISO', make: 'Make', model: 'Model', play: 'Play', busy: 'Camera in use', recording: 'Recording', notLive: 'Camera not ready',
		failed: 'Could not save', deleted: 'Deleted', kindPhoto: 'Photo', kindVideo: 'Video', fps: 'fps',
	},
	zh: {
		title: '相机', back: '返回', info: '简介', download: '下载', delete: '删除', settings: '设置',
		lowLight: '光线不足', retry: '重试', unreadable: '无法打开', photo: '照片', video: '视频',
		takePhoto: '拍照', record: '录制', stop: '停止', cameraInfo: '相机', photoSize: '照片尺寸',
		timer: '定时', off: '关', sound: '快门声', source: '相机', builtIn: '内置', cancel: '取消',
		starting: '正在启动', saving: '正在存储', inUse: '相机正被占用', noCamera: '未检测到相机', error: '相机出错',
		deletePhoto: '删除照片？', deleteVideo: '删除视频？', dims: '尺寸', size: '大小', date: '日期',
		duration: '时长', frames: '帧数', exposure: '曝光', aperture: '光圈', focal: '焦距',
		iso: 'ISO', make: '制造商', model: '型号', play: '播放', busy: '相机正被占用', recording: '正在录制', notLive: '相机未就绪',
		failed: '无法存储', deleted: '已删除', kindPhoto: '照片', kindVideo: '视频', fps: 'fps',
	},
});

const $ = (id) => document.getElementById(id);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let st = null; // backend state
let items = [];
let view = 'camera';
let sel = -1; // index of the item in the viewer
let mode = 'photo';
let prefs = { timer: 0, sound: true };
let capturing = false;
let haveFrame = false;
let newName = '';
let countdownCancel = null;
let panOverride = null; // view position while dragging

// ---- multipart MJPEG over fetch (preview and video playback) ----

function concat(a, b) {
	const c = new Uint8Array(a.length + b.length);
	c.set(a);
	c.set(b, a.length);
	return c;
}

function headerEnd(buf) {
	for (let i = 0; i + 3 < buf.length; i++) {
		if (buf[i] === 13 && buf[i + 1] === 10 && buf[i + 2] === 13 && buf[i + 3] === 10) return i;
	}
	return -1;
}

async function readMultipart(url, signal, onFrame) {
	const r = await fetch(url, { signal, cache: 'no-store' });
	if (!r.ok || !r.body) throw new Error('HTTP ' + r.status);
	const reader = r.body.getReader();
	const dec = new TextDecoder();
	let buf = new Uint8Array(0);
	for (;;) {
		const { value, done } = await reader.read();
		if (done) return;
		buf = buf.length ? concat(buf, value) : value;
		for (;;) {
			const he = headerEnd(buf);
			if (he < 0) break;
			const m = /content-length:\s*(\d+)/i.exec(dec.decode(buf.subarray(0, he)));
			const start = he + 4;
			if (!m) {
				buf = buf.subarray(start);
				continue;
			}
			const n = +m[1];
			if (buf.length < start + n) break;
			onFrame(buf.slice(start, start + n));
			buf = buf.subarray(start + n);
		}
	}
}

function showJPEG(img, bytes) {
	const url = URL.createObjectURL(new Blob([bytes], { type: 'image/jpeg' }));
	const old = img.dataset.url;
	img.dataset.url = url;
	img.src = url;
	if (old) setTimeout(() => URL.revokeObjectURL(old), 1000);
}

function setAspect(el, w, h) {
	if (!w || !h) return;
	el.style.setProperty('--arw', w);
	el.style.setProperty('--arh', h);
}

// ---- live preview: the stream connection is what keeps the camera on ----

let pv = null;
async function startPreview() {
	if (pv) return;
	const ctl = new AbortController();
	pv = ctl;
	while (pv === ctl) {
		try {
			await readMultipart('api/preview', ctl.signal, (f) => {
				if (!haveFrame) {
					haveFrame = true;
					renderState();
				}
				showJPEG($('feed'), f);
			});
		} catch (_) {
			/* reconnect below */
		}
		if (pv !== ctl) break;
		await sleep(1000);
	}
}

function stopPreview() {
	if (pv) pv.abort();
	pv = null;
}

$('feed').addEventListener('load', () => setAspect($('frame'), $('feed').naturalWidth, $('feed').naturalHeight));

// ---- state ----

function renderState() {
	const s = st;
	const frame = $('frame');
	const state = s ? s.state : 'idle';
	const live = state === 'live';
	if (state === 'busy' || state === 'nocamera' || state === 'error') haveFrame = false;
	const off = !haveFrame && state !== 'live';
	frame.classList.toggle('off', off);
	frame.classList.toggle('dim', haveFrame && !live && state !== 'capturing');

	const status = { busy: ['lock_fill', 'inUse'], nocamera: ['camera', 'noCamera'], error: ['exclamationmark_triangle', 'error'] }[state];
	$('status').hidden = !status;
	if (status) {
		$('status-glyph').textContent = status[0];
		$('status-title').textContent = t(status[1]);
		$('status-sub').textContent = state === 'busy' ? s.holder || '' : state === 'error' ? s.error || '' : '';
	}
	const spinning = !status && (state === 'starting' || state === 'idle' || state === 'capturing' || capturing);
	$('spinner').hidden = !spinning;
	$('spinner-text').textContent = state === 'capturing' || capturing ? t('saving') : t('starting');

	const recording = !!(s && s.recording);
	const sh = $('shutter');
	sh.classList.toggle('video', mode === 'video');
	sh.classList.toggle('recording', recording);
	sh.disabled = recording ? false : !live || capturing || !!countdownCancel;
	if (countdownCancel) sh.disabled = false;
	sh.title = mode === 'video' ? (recording ? t('stop') : t('record')) : t('takePhoto');
	for (const b of $('mode').children) {
		b.setAttribute('aria-pressed', String(b.dataset.mode === mode));
		b.disabled = recording;
	}

	const zoom = s ? s.zoom : 100;
	$('zoom').hidden = !(s && s.can_zoom);
	for (const b of $('zoom').children) b.setAttribute('aria-pressed', String(+b.dataset.zoom === zoom));
	$('zoom-chip').hidden = !(live && zoom > 100);
	$('zoom-chip').textContent = zoom / 100 + '×';
	$('low-chip').hidden = !(live && s.have_lux && s.lux < 3);
	$('rec-chip').hidden = !recording;
	frame.classList.toggle('zoomed', zoom > 100 && live);
	renderNavigator();
	renderSettings();
}

function renderNavigator() {
	const s = st;
	const zoom = s ? s.zoom : 100;
	$('nav').hidden = !(s && s.state === 'live' && zoom > 100);
	if (zoom <= 100) return;
	const v = panOverride || { x: s.view_x, y: s.view_y };
	const k = 100 / zoom;
	const r = $('nav-rect').style;
	r.width = k * 100 + '%';
	r.height = k * 100 + '%';
	r.left = (v.x / 100) * (1 - k) * 100 + '%';
	r.top = (v.y / 100) * (1 - k) * 100 + '%';
}

setInterval(() => {
	if (st && st.recording) {
		const sec = Math.max(0, Math.floor((Date.now() - st.record_since) / 1000));
		$('rec-time').textContent = Math.floor(sec / 60) + ':' + String(sec % 60).padStart(2, '0');
	}
}, 250);

// ---- settings popover ----

function renderSettings() {
	const s = st;
	if (!s) return;
	const res = $('res');
	if (res.childElementCount !== s.resolutions.length) {
		res.textContent = '';
		for (const r of s.resolutions) {
			const b = document.createElement('button');
			b.dataset.w = r[0];
			b.dataset.h = r[1];
			b.textContent = Math.round((r[0] * r[1]) / 1e6) + ' MP';
			b.title = r[0] + ' × ' + r[1];
			res.appendChild(b);
		}
	}
	for (const b of res.children) b.setAttribute('aria-pressed', String(+b.dataset.w === s.resolution[0]));
	res.closest('.zd-row').hidden = s.source !== 'csi';
	$('source-row').hidden = !s.usb && s.source !== 'usb';
	for (const b of $('source').children) b.setAttribute('aria-pressed', String(b.dataset.source === s.source));
	for (const b of $('timer').children) b.setAttribute('aria-pressed', String(+b.dataset.timer === prefs.timer));
	$('sound').setAttribute('aria-checked', String(prefs.sound));
	const parts = [];
	if (s.source === 'usb' && s.usb) parts.push(s.usb);
	else if (s.sensor) parts.push(s.sensor.toUpperCase());
	if (s.preview && s.preview[0]) parts.push(s.preview[0] + '×' + s.preview[1]);
	if (s.state === 'live' && s.fps > 0) parts.push(Math.round(s.fps) + ' ' + t('fps'));
	$('cam-desc').textContent = parts.join(' · ');
}

async function post(path, body) {
	const r = await fetch(path, {
		method: 'POST',
		headers: body ? { 'Content-Type': 'application/json' } : {},
		body: body ? JSON.stringify(body) : undefined,
	});
	let data = null;
	try {
		data = await r.json();
	} catch (_) {}
	if (!r.ok) throw new Error((data && data.error) || 'HTTP ' + r.status);
	return data;
}

async function setting(body) {
	try {
		const s = await post('api/settings', body);
		if (s) {
			st = s;
			renderState();
		}
	} catch (e) {
		toast(e.message);
	}
}

function savePrefs() {
	zd.storage.set('prefs', prefs).catch(() => {});
	renderSettings();
}

function togglePopover(open) {
	const p = $('settings');
	if (open === undefined) open = p.hidden;
	p.hidden = !open;
	$('settings-btn').setAttribute('aria-pressed', String(open));
}

$('settings-btn').onclick = (e) => {
	e.stopPropagation();
	togglePopover();
};
$('settings').addEventListener('pointerdown', (e) => e.stopPropagation());
document.addEventListener('pointerdown', (e) => {
	if (!e.target.closest || !e.target.closest('#settings-btn')) togglePopover(false);
});
$('res').onclick = (e) => {
	const b = e.target.closest('button');
	if (b) setting({ resolution: [+b.dataset.w, +b.dataset.h] });
};
$('source').onclick = (e) => {
	const b = e.target.closest('button');
	if (b && st && b.dataset.source !== st.source) {
		haveFrame = false;
		setting({ source: b.dataset.source });
	}
};
$('timer').onclick = (e) => {
	const b = e.target.closest('button');
	if (!b) return;
	prefs.timer = +b.dataset.timer;
	savePrefs();
};
$('sound').onclick = () => {
	prefs.sound = !prefs.sound;
	savePrefs();
};

// ---- zoom and pan ----

$('zoom').onclick = (e) => {
	const b = e.target.closest('button');
	if (b) setting({ zoom: +b.dataset.zoom });
};

let drag = null;
$('frame').addEventListener('pointerdown', (e) => {
	if (!st || st.zoom <= 100 || st.state !== 'live' || e.button !== 0) return;
	const r = $('frame').getBoundingClientRect();
	drag = { id: e.pointerId, x: e.clientX, y: e.clientY, w: r.width, h: r.height, vx: st.view_x, vy: st.view_y };
	$('frame').setPointerCapture(e.pointerId);
	$('frame').classList.add('dragging');
});
$('frame').addEventListener('pointermove', (e) => {
	if (!drag || e.pointerId !== drag.id) return;
	const k = 100 / st.zoom;
	const f = (k / (1 - k)) * 100; // view percent per frame width
	panOverride = {
		x: Math.max(0, Math.min(100, drag.vx - ((e.clientX - drag.x) / drag.w) * f)),
		y: Math.max(0, Math.min(100, drag.vy - ((e.clientY - drag.y) / drag.h) * f)),
	};
	renderNavigator();
});
function endDrag(e) {
	if (!drag || e.pointerId !== drag.id) return;
	drag = null;
	$('frame').classList.remove('dragging');
	if (panOverride) {
		const v = panOverride;
		panOverride = null;
		setting({ view_x: Math.round(v.x), view_y: Math.round(v.y) });
	}
}
$('frame').addEventListener('pointerup', endDrag);
$('frame').addEventListener('pointercancel', endDrag);

// ---- shutter ----

let audioCtx = null;
const shutterSound = new Audio('api/sound/shutter');
shutterSound.preload = 'auto';
let soundFailed = false;
shutterSound.addEventListener('error', () => (soundFailed = true));

function playShutter() {
	if (!prefs.sound) return;
	if (!soundFailed) {
		shutterSound.currentTime = 0;
		shutterSound.play().catch(() => {});
		return;
	}
	try {
		audioCtx = audioCtx || new AudioContext();
		const n = audioCtx.sampleRate * 0.06;
		const buf = audioCtx.createBuffer(1, n, audioCtx.sampleRate);
		const d = buf.getChannelData(0);
		for (let i = 0; i < n; i++) d[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / n, 3);
		const src = audioCtx.createBufferSource();
		src.buffer = buf;
		src.connect(audioCtx.destination);
		src.start();
	} catch (_) {}
}

function flash() {
	const f = $('flash');
	f.classList.remove('on');
	void f.offsetWidth;
	f.classList.add('on');
}

async function countdown(sec) {
	const el = $('countdown');
	let cancelled = false;
	countdownCancel = () => (cancelled = true);
	renderState();
	el.hidden = false;
	for (let i = sec; i > 0 && !cancelled; i--) {
		el.innerHTML = '<span>' + i + '</span>';
		for (let k = 0; k < 10 && !cancelled; k++) await sleep(100);
	}
	el.hidden = true;
	countdownCancel = null;
	renderState();
	return !cancelled;
}

const errText = (m) => ({ busy: t('busy'), recording: t('recording'), 'not live': t('notLive') })[m] || m;

async function takePhoto() {
	if (countdownCancel) return countdownCancel();
	if (capturing || !st || st.state !== 'live') return;
	if (prefs.timer && !(await countdown(prefs.timer))) return;
	if (!st || st.state !== 'live') return;
	capturing = true;
	playShutter();
	flash();
	renderState();
	try {
		const r = await post('api/capture');
		newName = r.name;
		await loadLibrary();
	} catch (e) {
		toast(t('failed') + ': ' + errText(e.message));
	} finally {
		capturing = false;
		renderState();
	}
}

async function toggleRecording() {
	try {
		if (st && st.recording) {
			const r = await post('api/record/stop');
			if (r && r.name) newName = r.name;
		} else {
			if (prefs.timer && !(await countdown(prefs.timer))) return;
			await post('api/record/start');
			playShutter();
		}
	} catch (e) {
		toast(errText(e.message));
	}
}

$('shutter').onclick = () => (mode === 'video' || (st && st.recording) ? toggleRecording() : takePhoto());
$('mode').onclick = (e) => {
	const b = e.target.closest('button');
	if (!b || (st && st.recording)) return;
	mode = b.dataset.mode;
	renderState();
};

// ---- library ----

function itemDate(name) {
	const m = /^(?:CAM|VID)_(\d{4})(\d\d)(\d\d)_(\d\d)(\d\d)(\d\d)/.exec(name);
	return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) : null;
}

function fmtDate(d) {
	return d.toLocaleString(zd.lang === 'zh' ? 'zh-CN' : 'en-US', {
		year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
	});
}

const enc = encodeURIComponent;
const thumbURL = (it) => 'api/media/' + enc(it.name) + '/thumb?v=' + it.time;

async function loadLibrary() {
	try {
		const r = await fetch('api/media', { cache: 'no-store' });
		const list = await r.json();
		const cur = sel >= 0 && items[sel] ? items[sel].name : '';
		items = list;
		if (view === 'viewer') {
			const i = items.findIndex((x) => x.name === cur);
			if (i < 0) {
				if (!items.length) closeViewer();
				else openItem(Math.min(sel, items.length - 1));
			} else sel = i;
		}
		renderStrip();
	} catch (_) {}
}

function renderStrip() {
	const strip = $('strip');
	const keep = strip.scrollLeft;
	strip.textContent = '';
	items.slice(0, 300).forEach((it, i) => {
		const b = document.createElement('button');
		b.className = 'thumb';
		b.title = fmtDate(itemDate(it.name) || new Date(it.time));
		if (view === 'viewer' && i === sel) b.setAttribute('aria-current', 'true');
		if (it.name === newName) b.classList.add('new');
		const img = document.createElement('img');
		img.loading = 'lazy';
		img.alt = '';
		img.draggable = false;
		img.src = thumbURL(it);
		b.appendChild(img);
		if (it.kind === 'video') {
			const g = document.createElement('i');
			g.className = 'zd-glyph';
			g.textContent = 'videocam_fill';
			b.appendChild(g);
		}
		b.onclick = () => openItem(i);
		strip.appendChild(b);
	});
	newName = '';
	strip.scrollLeft = keep;
	const cur = strip.querySelector('[aria-current="true"]');
	if (cur) cur.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}

$('strip').addEventListener(
	'wheel',
	(e) => {
		if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
			$('strip').scrollLeft += e.deltaY;
			e.preventDefault();
		}
	},
	{ passive: false },
);

// ---- viewer ----

let play = null;
function stopPlay() {
	if (play) play.abort();
	play = null;
}

function openItem(i) {
	if (!items[i]) return;
	togglePopover(false);
	stopPlay();
	sel = i;
	const it = items[i];
	if (view !== 'viewer') {
		view = 'viewer';
		$('win').dataset.view = 'viewer';
		stopPreview();
	}
	const ph = $('photo');
	const low = $('photo-low');
	$('viewer-empty').hidden = true;
	ph.hidden = low.hidden = false;
	if (ph.dataset.url) {
		URL.revokeObjectURL(ph.dataset.url);
		delete ph.dataset.url;
	}
	// the thumbnail shows at once; the full photo fades in once decoded
	low.onload = () => setAspect($('viewer'), low.naturalWidth, low.naturalHeight);
	low.src = thumbURL(it);
	ph.classList.remove('ready');
	ph.onerror = () => {
		if (ph.dataset.url) return;
		ph.hidden = low.hidden = true;
		$('viewer-empty').hidden = false;
	};
	ph.onload = () => {
		setAspect($('viewer'), ph.naturalWidth, ph.naturalHeight);
		ph.classList.add('ready');
	};
	if (it.kind === 'video') ph.removeAttribute('src');
	else ph.src = 'api/media/' + enc(it.name) + '?v=' + it.time;
	$('play').hidden = it.kind !== 'video';
	const d = itemDate(it.name);
	$('v-title').textContent = d ? fmtDate(d) : it.name;
	$('v-sub').textContent = it.name;
	renderStrip();
	if (!$('inspector').hidden) loadInfo();
}

function closeViewer() {
	stopPlay();
	view = 'camera';
	sel = -1;
	$('win').dataset.view = 'camera';
	$('inspector').hidden = true;
	$('v-info').setAttribute('aria-pressed', 'false');
	renderStrip();
	startPreview();
}

$('back').onclick = closeViewer;

$('play').onclick = async () => {
	const it = items[sel];
	if (!it) return;
	stopPlay();
	const ctl = new AbortController();
	play = ctl;
	$('play').hidden = true;
	try {
		await readMultipart('api/media/' + enc(it.name) + '/play', ctl.signal, (f) => {
			showJPEG($('photo'), f);
			$('photo').classList.add('ready');
		});
	} catch (_) {}
	if (play === ctl) {
		play = null;
		$('play').hidden = false;
	}
};

function fmtBytes(n) {
	if (n < 1024) return n + ' B';
	if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
	return (n / 1048576).toFixed(1) + ' MB';
}

function fmtExposure(us) {
	if (us >= 1e6) return (us / 1e6).toFixed(1) + ' s';
	return '1/' + Math.round(1e6 / us) + ' s';
}

function rows(card, list) {
	card.textContent = '';
	for (const [k, v] of list) {
		if (v === undefined || v === null || v === '') continue;
		const r = document.createElement('div');
		r.className = 'zd-row';
		const a = document.createElement('span');
		a.textContent = t(k);
		const b = document.createElement('span');
		b.className = 'zd-value';
		b.textContent = v;
		b.title = v;
		r.append(a, b);
		card.appendChild(r);
	}
	return card.childElementCount;
}

async function loadInfo() {
	const it = items[sel];
	if (!it) return;
	let info = null;
	try {
		const r = await fetch('api/media/' + enc(it.name) + '/info', { cache: 'no-store' });
		if (r.ok) info = await r.json();
	} catch (_) {}
	if (items[sel] !== it) return;
	$('i-name').textContent = it.kind === 'video' ? t('kindVideo') : t('kindPhoto');
	const i = info || {};
	const dur = i.frames && i.fps ? i.frames / i.fps : 0;
	rows($('i-file'), [
		['dims', i.width ? i.width + ' × ' + i.height : ''],
		['size', fmtBytes(it.size)],
		['date', fmtDate(itemDate(it.name) || new Date(it.time))],
		['duration', dur ? Math.floor(dur / 60) + ':' + String(Math.round(dur % 60)).padStart(2, '0') : ''],
		['frames', i.frames ? i.frames + ' · ' + i.fps + ' ' + t('fps') : ''],
	]);
	const n = rows($('i-cam'), [
		['make', i.make],
		['model', i.model],
		['exposure', i.exposure_us ? fmtExposure(i.exposure_us) : ''],
		['aperture', i.f_number ? 'ƒ/' + +i.f_number.toFixed(1) : ''],
		['focal', i.focal_length ? +i.focal_length.toFixed(2) + ' mm' : ''],
		['iso', i.iso || ''],
	]);
	$('i-cam-sec').hidden = n === 0;
}

$('v-info').onclick = () => {
	const open = $('inspector').hidden;
	$('inspector').hidden = !open;
	$('v-info').setAttribute('aria-pressed', String(open));
	if (open) loadInfo();
};

$('v-save').onclick = () => {
	const it = items[sel];
	if (!it) return;
	const a = document.createElement('a');
	a.href = 'api/media/' + enc(it.name) + '?download=1';
	a.download = it.name;
	document.body.appendChild(a);
	a.click();
	a.remove();
};

function askDelete() {
	const it = items[sel];
	if (!it) return;
	$('sheet-title').textContent = it.kind === 'video' ? t('deleteVideo') : t('deletePhoto');
	$('sheet-text').textContent = it.name;
	$('sheet').hidden = false;
	$('sheet-ok').focus();
}

$('v-delete').onclick = askDelete;
$('sheet-cancel').onclick = () => ($('sheet').hidden = true);
$('sheet-ok').onclick = async () => {
	$('sheet').hidden = true;
	const it = items[sel];
	if (!it) return;
	stopPlay();
	const r = await fetch('api/media/' + enc(it.name), { method: 'DELETE' });
	if (!r.ok) toast(t('failed'));
	await loadLibrary();
};

// ---- toast ----

let toastTimer = 0;
function toast(text) {
	const el = $('toast');
	el.textContent = text;
	el.hidden = false;
	clearTimeout(toastTimer);
	toastTimer = setTimeout(() => (el.hidden = true), 2600);
}

// ---- keyboard ----

document.addEventListener('keydown', (e) => {
	if (e.metaKey || e.ctrlKey || e.altKey) return;
	if (!$('sheet').hidden) {
		if (e.key === 'Escape') $('sheet').hidden = true;
		return;
	}
	if (e.key === 'Escape') {
		if (!$('settings').hidden) togglePopover(false);
		else if (countdownCancel) countdownCancel();
		else if (view === 'viewer') closeViewer();
		return;
	}
	if (view === 'viewer') {
		if (e.key === 'ArrowLeft' && sel > 0) openItem(sel - 1);
		else if (e.key === 'ArrowRight' && sel < items.length - 1) openItem(sel + 1);
		else if (e.key === 'Delete' || e.key === 'Backspace') askDelete();
		else if (e.key === 'i') $('v-info').click();
		else if (e.key === ' ' && items[sel] && items[sel].kind === 'video') $('play').click();
		else return;
		e.preventDefault();
		return;
	}
	if (e.key === ' ' || e.key === 'Enter') {
		if (e.target.closest && e.target.closest('button') && e.key === 'Enter') return;
		$('shutter').click();
	} else if ((e.key === '+' || e.key === '=') && st && st.can_zoom) {
		setting({ zoom: st.zoom >= 250 ? 500 : 250 });
	} else if (e.key === '-' && st && st.can_zoom) {
		setting({ zoom: st.zoom > 250 ? 250 : 100 });
	} else if (e.key.startsWith('Arrow') && st && st.zoom > 100) {
		const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[e.key];
		setting({ pan: d });
	} else return;
	e.preventDefault();
});

// ---- start ----

zd.on('lang', () => {
	renderState();
	renderStrip();
	if (view === 'viewer') openItem(sel);
});

zd.ready.then(() =>
	zd.storage
		.get('prefs')
		.then((p) => {
			if (p && typeof p === 'object') prefs = { ...prefs, ...p };
			renderSettings();
		})
		.catch(() => {}),
);

fetch('api/state')
	.then((r) => r.json())
	.then((s) => {
		st = s;
		renderState();
	})
	.catch(() => {});
const es = new EventSource('api/events');
es.addEventListener('state', (e) => {
	st = JSON.parse(e.data);
	renderState();
});
es.addEventListener('library', () => loadLibrary());
loadLibrary();
renderState();
startPreview();
