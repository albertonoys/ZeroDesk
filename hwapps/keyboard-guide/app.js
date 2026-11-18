'use strict';

// Keyboard Guide: Keyboard Viewer for the built-in keyboard plus the
// original app's lessons (CardputerZero/Keyboard-Guide, MIT).

const t = zd.localize({
	en: {
		title: 'Keyboard Guide', lessons: 'Lessons', restart: 'Restart', previous: 'Previous', next: 'Next',
		typeHere: 'Type on CardputerZero', l1: 'Lowercase', l2: 'Hold Shift', l3: 'One-Shot Shift', l4: 'Shift Lock',
		l5: 'Modifier Keys', l6: 'Symbols', l7: 'Fn Keys', l8: 'Type a Phrase', p1: 'Press A',
		p2: 'Hold shift, then press A', p3: 'Tap shift, then press A', p4: 'Double-tap shift, type A B C',
		p5: 'Sym, shift and fn work the same way', p6: 'Use sym to type ! @ #', p7: 'Use fn for ▲ ▼ and volume',
		p8: 'Type Hi,M5Stack!', ushift: 'Tap shift to unlock', usym: 'Tap sym to unlock', ufn: 'Tap fn to unlock',
		pok: 'Press OK', done: 'Done', allDone: 'All lessons done', mTap: 'Tap', mTapV: 'one key', mDouble: 'Double-tap',
		mDoubleV: 'lock', mHold: 'Hold', mHoldV: 'while held', noDevice: 'Keyboard Not Found',
		noDeviceText: 'No built-in keyboard', denied: 'No Access to the Keyboard', deniedText: 'Needs the input group',
		read: 'Keyboard Not Responding', readText: 'Reconnecting…', offline: 'Not Connected', offlineText: 'Reconnecting…',
	},
	zh: {
		title: '键盘指南', lessons: '课程', restart: '重新开始', previous: '上一课', next: '下一课',
		typeHere: '在 CardputerZero 上打字', l1: '小写字母', l2: '按住 Shift', l3: '单次 Shift', l4: '锁定 Shift',
		l5: '修饰键', l6: '符号', l7: 'Fn 功能键', l8: '输入短语', p1: '按 A', p2: '按住 shift，再按 A',
		p3: '点按 shift，再按 A', p4: '双击 shift，输入 A B C', p5: 'sym、shift、fn 用法相同', p6: '用 sym 输入 ! @ #',
		p7: '用 fn 输入 ▲ ▼ 和音量', p8: '输入 Hi,M5Stack!', ushift: '点按 shift 解锁', usym: '点按 sym 解锁',
		ufn: '点按 fn 解锁', pok: '按 OK 继续', done: '完成', allDone: '全部完成', mTap: '点按', mTapV: '单次',
		mDouble: '双击', mDoubleV: '锁定', mHold: '按住', mHoldV: '按住时', noDevice: '找不到键盘',
		noDeviceText: '没有内置键盘', denied: '无法访问键盘', deniedText: '需要 input 用户组', read: '键盘无响应',
		readText: '正在重新连接…', offline: '未连接', offlineText: '正在重新连接…',
	},
});

const $ = (id) => document.getElementById(id);

// ---- physical layout (CardputerZero): visual rows of matrix positions ----

const ROWS = [
	['r4c0', null, null, null, null, null, null, null, null, null, 'r4c9'],
	['r4c4', 'r0c0', 'r0c1', 'r0c2', 'r0c3', 'r0c4', 'r0c5', 'r0c6', 'r0c7', 'r0c8', 'r0c9'],
	['r4c3', 'r1c0', 'r1c1', 'r1c2', 'r1c3', 'r1c4', 'r1c5', 'r1c6', 'r1c7', 'r1c8', 'r1c9'],
	['r4c1', 'r2c0', 'r2c1', 'r2c2', 'r2c3', 'r2c4', 'r2c5', 'r2c6', 'r2c7', 'r2c8', 'r2c9'],
	['r4c2', 'r3c0', 'r3c1', 'r3c2', 'r3c3', 'r3c4', 'r3c5', 'r3c6', 'r3c7', 'r3c8', 'r3c9'],
];
const KEY_W = 46, KEY_H = 38, GAP = 6, PAD = 14, TOP_EXTRA = 4;
const BOARD_W = PAD * 2 + 11 * KEY_W + 10 * GAP;
const BOARD_H = PAD * 2 + 5 * KEY_H + 4 * GAP + TOP_EXTRA;

const KEY_SHIFT = 42, KEY_SYM = 383, KEY_FN = 389, KEY_CTRL = 29, KEY_ALT = 56;
const KEY_ENTER = 28, KEY_BACKSPACE = 14, KEY_ESC = 1, KEY_TAB = 15;
const CHIPS = { [KEY_SYM]: ['sym', 'SYM'], [KEY_SHIFT]: ['shift', 'Aa'], [KEY_FN]: ['fn', 'fn'], [KEY_CTRL]: ['ctrl', 'ctrl'], [KEY_ALT]: ['alt', 'alt'] };
const MOD_CODE = { shift: KEY_SHIFT, sym: KEY_SYM, fn: KEY_FN };
const HOLD = { [KEY_ESC]: 'home', [KEY_TAB]: 'talk' }; // printed long-press functions

let state = null; // backend state
let mods = { shift: 'off', sym: 'off', fn: 'off', source: '' };
let manualLayer = null;
const pressed = new Set();
const keyEls = {};
let typed = '';
let connected = false;

function el(tag, cls, text) {
	const e = document.createElement(tag);
	if (cls) e.className = cls;
	if (text !== undefined) e.textContent = text;
	return e;
}

function glyph(name, cls) {
	return el('i', 'zd-glyph' + (cls ? ' ' + cls : ''), name);
}

// content of a cap: icon, label or both
function capNodes(c) {
	if (!c || !c.code) return [];
	if (c.code === KEY_ENTER) return [glyph('return', 'ok'), el('small', 'ok', 'OK')];
	if (c.code === KEY_BACKSPACE || c.code === 111) return [glyph(c.icon), el('small', '', 'del')];
	if (c.icon) return [glyph(c.icon)];
	return [document.createTextNode(c.label)];
}

function isWord(c) {
	return c && c.code && !c.icon && [...c.label].length > 2;
}

function modKeyId(name) {
	if (!state) return null;
	for (const id in state.layout.keys) if (state.layout.keys[id].base.code === MOD_CODE[name]) return id;
	return null;
}

function autoLayer() {
	if (mods.fn !== 'off') return 'fn';
	if (mods.sym !== 'off') return 'sym';
	if (mods.shift !== 'off') return 'shift';
	return 'base';
}

function layer() {
	return manualLayer || autoLayer();
}

// ---- keyboard drawing ----

function buildBoard() {
	const board = $('board');
	board.textContent = '';
	board.style.width = BOARD_W + 'px';
	board.style.height = BOARD_H + 'px';
	for (const k in keyEls) delete keyEls[k];
	ROWS.forEach((row, r) => {
		row.forEach((id, c) => {
			if (!id || !state || !state.layout.keys[id]) return;
			const k = el('div', 'key');
			k.style.left = PAD + c * (KEY_W + GAP) + 'px';
			k.style.top = PAD + r * (KEY_H + GAP) + (r > 0 ? TOP_EXTRA : 0) + 'px';
			k.dataset.id = id;
			board.appendChild(k);
			keyEls[id] = k;
		});
	});
	renderFaces();
	renderKeyState();
	fit();
}

function renderFaces() {
	if (!state) return;
	const L = layer();
	for (const id in keyEls) {
		const k = keyEls[id];
		const caps = state.layout.keys[id];
		const base = caps.base;
		const cap = caps[L];
		k.textContent = '';
		k.classList.remove('empty', 'changed', 'word', 'solo');
		const main = el('span', 'main');
		const chip = CHIPS[base.code];
		if (chip) {
			main.appendChild(el('span', 'chip ' + chip[0], chip[1]));
			k.classList.add('solo');
		} else if (!cap.code) {
			k.classList.add('empty');
		} else {
			main.append(...capNodes(cap));
			if (isWord(cap)) k.classList.add('word');
			if (L !== 'base' && (cap.code !== base.code || cap.label !== base.label)) k.classList.add('changed');
			if (L === 'base') {
				if (HOLD[base.code]) k.appendChild(el('span', 'leg hold', HOLD[base.code]));
				const fn = caps.fn, sym = caps.sym;
				if (fn.code && fn.code !== base.code && fn.label !== base.label) {
					const leg = el('span', 'leg fn');
					leg.append(...(fn.icon ? [glyph(fn.icon)] : [document.createTextNode(fn.label)]));
					k.appendChild(leg);
				}
				if (sym.code && sym.code !== base.code) k.appendChild(el('span', 'leg sym', sym.label));
				if (!k.querySelector('.leg')) k.classList.add('solo');
			} else {
				k.classList.add('solo');
			}
		}
		k.appendChild(main);
		const ind = el('span', 'ind');
		if (chip) k.appendChild(ind);
	}
	for (const b of $('layers').children) b.setAttribute('aria-pressed', String(b.dataset.layer === L));
}

function renderKeyState() {
	const target = currentTarget();
	for (const id in keyEls) {
		const k = keyEls[id];
		const code = state.layout.keys[id].base.code;
		let mode = 'off';
		for (const m in MOD_CODE) if (MOD_CODE[m] === code) mode = mods[m] || 'off';
		k.classList.toggle('down', pressed.has(id) || mode === 'held');
		k.classList.toggle('oneshot', mode === 'oneshot');
		k.classList.toggle('locked', mode === 'locked');
		const ind = k.querySelector('.ind');
		if (ind && (mode === 'locked') !== !!ind.firstChild) {
			ind.textContent = '';
			if (mode === 'locked') ind.appendChild(glyph('lock_fill'));
		}
		k.classList.toggle('target', id === target);
	}
}

function flash(id) {
	const k = keyEls[id];
	if (!k) return;
	k.classList.add('flash');
	clearTimeout(k._flash);
	k._flash = setTimeout(() => k.classList.remove('flash'), 180);
}

function fit() {
	const stage = $('stage');
	const w = stage.clientWidth, h = stage.clientHeight;
	const s = Math.max(0.3, Math.min(w / BOARD_W, h / BOARD_H, 1.6));
	$('board').style.setProperty('--s', s.toFixed(3));
}
new ResizeObserver(fit).observe($('stage'));

// ---- typed text and the last key ----

function renderTyped() {
	$('typed-text').textContent = typed;
	$('typed-ph').hidden = typed !== '';
	const box = $('typed'), txt = $('typed-text');
	txt.style.transform = '';
	const over = txt.offsetLeft + txt.offsetWidth + 14 - box.clientWidth;
	if (over > 0) txt.style.transform = `translateX(${-over}px)`;
}

function renderLast(ev) {
	const last = $('last');
	last.textContent = '';
	const cap = el('span', 'cap');
	if (ev.value === -1) {
		const caps = state.layout.keys[ev.id];
		const chip = caps && CHIPS[caps.base.code];
		cap.textContent = chip ? chip[0] : '?';
		last.append(cap, el('span', 'code', 'MSC_SCAN 0x' + ev.scan.toString(16)));
		return;
	}
	const nodes = capNodes(ev);
	if (nodes.length) cap.append(...nodes);
	else cap.textContent = ev.code;
	last.append(cap, el('span', '', ev.name), el('span', 'code', String(ev.code)));
}

// ---- lessons (CardputerZero/Keyboard-Guide's eight exercises) ----

const LESSONS = [
	{ name: 'l1', prompt: 'p1', steps: [{ text: 'a' }] },
	{ name: 'l2', prompt: 'p2', info: ['hold'], steps: [{ text: 'A', shift: 'held' }] },
	{ name: 'l3', prompt: 'p3', info: ['tap'], steps: [{ text: 'A', shift: 'oneshot' }] },
	{
		name: 'l4', prompt: 'p4', info: ['double'],
		steps: [{ text: 'A', shift: 'locked' }, { text: 'B', shift: 'locked' }, { text: 'C', shift: 'locked' }, { unlock: 'shift' }],
	},
	{ name: 'l5', prompt: 'p5', info: ['tap', 'double', 'hold'], steps: [{ code: KEY_ENTER }] },
	{ name: 'l6', prompt: 'p6', chip: 'sym', steps: [{ text: '!' }, { text: '@' }, { text: '#' }, { unlock: 'sym' }] },
	{ name: 'l7', prompt: 'p7', chip: 'fn', steps: [{ code: 103 }, { code: 108 }, { code: 114 }, { code: 115 }, { unlock: 'fn' }] },
	{ name: 'l8', prompt: 'p8', steps: [...'Hi,M5Stack!'].map((c) => ({ text: c })) },
];
const INFO = {
	tap: ['smallcircle_fill_circle', 'mTap', 'mTapV'],
	double: ['lock_fill', 'mDouble', 'mDoubleV'],
	hold: ['arrow_down_to_line', 'mHold', 'mHoldV'],
};

let lessonIdx = 0;
let stepIdx = 0;
const doneLessons = new Set();
let advanceTimer = 0;
let errorTimer = 0;

function lesson() {
	return LESSONS[lessonIdx];
}

function step() {
	return lesson().steps[stepIdx];
}

function modeOk(actual, want) {
	if (mods.source === 'evdev') return actual !== 'off'; // no sticky state: any shift counts
	return actual === want;
}

// key that produces a text or code, and the layer it is on
function findKey(pred, prefer) {
	if (!state) return null;
	for (const L of new Set([...(prefer || []), 'base', 'shift', 'sym', 'fn'])) {
		for (const row of ROWS) for (const id of row) {
			if (id && state.layout.keys[id] && pred(state.layout.keys[id][L])) return [L, id];
		}
	}
	return null;
}

function currentTarget() {
	const s = lessonIdx < LESSONS.length ? step() : null;
	if (!s || !state) return null;
	if (s.unlock) return modKeyId(s.unlock);
	if (s.shift && mods.shift !== s.shift && !(mods.source === 'evdev' && mods.shift !== 'off')) return modKeyId('shift');
	// symbols on the sym layer (as printed), letters with shift
	const prefer = ['base', lesson().chip || (/[a-z]/i.test(s.text || '') ? 'shift' : 'sym')];
	const hit = s.text ? findKey((c) => c.text === s.text, prefer) : findKey((c) => c.code === s.code, prefer);
	if (!hit) return null;
	const [L, id] = hit;
	if (L === 'base' || mods[L] !== 'off') return id;
	return modKeyId(L);
}

function stepNodes(s) {
	if (s.unlock) return [glyph('lock_open'), el('small', '', s.unlock)];
	if (s.code === KEY_ENTER) return [glyph('return'), el('small', '', 'OK')];
	if (s.code) {
		const hit = findKey((c) => c.code === s.code);
		const c = hit && state.layout.keys[hit[1]][hit[0]];
		return c && c.icon ? [glyph(c.icon)] : [document.createTextNode(c ? c.label : String(s.code))];
	}
	return [document.createTextNode(s.text)];
}

function renderLesson() {
	const l = lesson();
	const done = stepIdx >= l.steps.length;
	$('lesson-num').textContent = String(lessonIdx + 1);
	let prompt = t(l.prompt);
	if (done) prompt = lessonIdx === LESSONS.length - 1 ? t('allDone') : t('done');
	else if (step().unlock) prompt = t('u' + step().unlock);
	else if (l.info && l.info.length > 1) prompt = t(l.prompt) + ' · ' + t('pok');
	$('lesson-prompt').textContent = prompt;
	$('lesson').classList.toggle('done', done);
	$('lesson-prev').disabled = lessonIdx === 0;
	$('lesson-next').disabled = lessonIdx === LESSONS.length - 1;

	const steps = $('steps');
	steps.textContent = '';
	if (!state) return;
	for (const name of l.info || []) {
		const [icon, a, b] = INFO[name];
		const chip = el('span', 'step info');
		chip.append(glyph(icon), el('small', '', t(a) + ' · ' + t(b)));
		steps.append(chip);
	}
	if (l.chip) {
		const chip = el('span', 'step info');
		chip.appendChild(el('span', 'chip ' + l.chip, l.chip));
		steps.append(chip);
	}
	if ((l.info && l.info.length === 1) || l.chip) steps.append(el('span', 'plus', '+'));
	l.steps.forEach((s, i) => {
		const chip = el('span', 'step' + (i < stepIdx ? ' ok' : i === stepIdx && !done ? ' current' : ''));
		chip.append(...stepNodes(s));
		steps.append(chip);
	});

	const items = $('lessons').children;
	for (let i = 0; i < items.length; i++) {
		items[i].setAttribute('aria-current', String(i === lessonIdx));
		items[i].classList.toggle('done', doneLessons.has(i));
		items[i].querySelector('.n').textContent = doneLessons.has(i) ? '' : String(i + 1);
		if (doneLessons.has(i) && !items[i].querySelector('.n').firstChild) items[i].querySelector('.n').appendChild(glyph('checkmark'));
		items[i].querySelector('.label').textContent = t(LESSONS[i].name);
	}
}

function buildLessonList() {
	const ol = $('lessons');
	ol.textContent = '';
	LESSONS.forEach((l, i) => {
		const li = el('li');
		li.append(el('span', 'n'), el('span', 'label'));
		li.onclick = () => {
			goLesson(i);
			if (narrow.matches) setSidebar(false);
		};
		ol.appendChild(li);
	});
}

function goLesson(i) {
	clearTimeout(advanceTimer);
	lessonIdx = Math.max(0, Math.min(LESSONS.length - 1, i));
	stepIdx = 0;
	renderLesson();
	renderKeyState();
	checkUnlock();
}

function advance() {
	stepIdx++;
	if (stepIdx >= lesson().steps.length) {
		doneLessons.add(lessonIdx);
		if (lessonIdx < LESSONS.length - 1) {
			const from = lessonIdx;
			advanceTimer = setTimeout(() => {
				if (lessonIdx === from) goLesson(from + 1);
			}, 1250);
		}
	}
	renderLesson();
	renderKeyState();
	checkUnlock();
}

function lessonError() {
	$('lesson').classList.add('error');
	clearTimeout(errorTimer);
	errorTimer = setTimeout(() => $('lesson').classList.remove('error'), 600);
}

function checkUnlock() {
	const s = lessonIdx < LESSONS.length && stepIdx < lesson().steps.length ? step() : null;
	if (s && s.unlock && mods[s.unlock] === 'off') advance();
}

function lessonKey(ev) {
	if (stepIdx >= lesson().steps.length) return;
	const s = step();
	if (s.unlock) return;
	const modifier = [KEY_SHIFT, 54, KEY_SYM, KEY_FN, KEY_CTRL, KEY_ALT].includes(ev.code);
	let ok = false;
	if (s.text !== undefined) ok = ev.text === s.text && (!s.shift || modeOk(ev.mods.shift, s.shift));
	else if (s.code) ok = ev.code === s.code;
	if (ok) advance();
	else if (!modifier && (ev.text || s.code)) lessonError();
}

// ---- state from the backend ----

function renderAvailability() {
	const empty = $('empty');
	let title = '', text = '';
	if (!connected || !state) [title, text] = [t('offline'), t('offlineText')];
	else if (!state.available) {
		const e = state.error === 'denied' ? 'denied' : state.error === 'read' ? 'read' : 'noDevice';
		[title, text] = [t(e), t(e + 'Text')];
	}
	empty.hidden = !title;
	$('empty-title').textContent = title;
	$('empty-text').textContent = text;
	$('board-wrap').hidden = !!title;
}

function applyState(s) {
	const fresh = !state || JSON.stringify(state.layout) !== JSON.stringify(s.layout);
	state = s;
	mods = s.mods || mods;
	pressed.clear();
	for (const id of s.pressed || []) pressed.add(id);
	if (fresh) buildBoard();
	else {
		renderFaces();
		renderKeyState();
	}
	renderAvailability();
	renderLesson();
}

function onKey(ev) {
	if (!state) return;
	if (ev.value === -1) {
		flash(ev.id);
		renderLast(ev);
		return;
	}
	if (ev.id) {
		if (ev.value > 0) pressed.add(ev.id);
		else pressed.delete(ev.id);
	}
	if (ev.value > 0) {
		if (ev.text) typed = (typed + ev.text).slice(-400);
		else if (ev.code === KEY_BACKSPACE) typed = [...typed].slice(0, -1).join('');
		else if (ev.code === KEY_ENTER) typed = '';
		renderTyped();
	}
	if (ev.value === 1) {
		renderLast(ev);
		lessonKey(ev);
	}
	renderKeyState();
}

function onMods(m) {
	const before = layer();
	const changed = m.shift !== mods.shift || m.sym !== mods.sym || m.fn !== mods.fn;
	mods = m;
	if (changed) manualLayer = null;
	if (layer() !== before) renderFaces();
	renderKeyState();
	checkUnlock();
}

let es = null;
function connect() {
	es = new EventSource('api/stream');
	es.addEventListener('state', (e) => {
		connected = true;
		applyState(JSON.parse(e.data));
	});
	es.addEventListener('key', (e) => onKey(JSON.parse(e.data)));
	es.addEventListener('mods', (e) => onMods(JSON.parse(e.data)));
	es.onerror = () => {
		if (es.readyState === EventSource.CLOSED) {
			connected = false;
			renderAvailability();
			setTimeout(connect, 3000);
		}
	};
}

// ---- chrome ----

function setSidebar(open) {
	$('sidebar').hidden = !open;
	$('sidebar-btn').setAttribute('aria-pressed', String(open));
}

$('sidebar-btn').onclick = () => setSidebar($('sidebar').hidden);
$('lesson-prev').onclick = () => goLesson(lessonIdx - 1);
$('lesson-next').onclick = () => goLesson(lessonIdx + 1);
$('lesson-restart').onclick = () => {
	doneLessons.delete(lessonIdx);
	goLesson(lessonIdx);
};
for (const b of $('layers').children) {
	b.onclick = () => {
		manualLayer = b.dataset.layer === autoLayer() ? null : b.dataset.layer;
		renderFaces();
		renderKeyState();
	};
}

zd.on('lang', () => {
	renderLesson();
	renderAvailability();
});

// like macOS, a narrow window collapses the sidebar (it then floats when opened)
const narrow = matchMedia('(max-width: 720px)');
narrow.addEventListener('change', () => setSidebar(!narrow.matches));
setSidebar(!narrow.matches);
buildLessonList();
renderLesson();
renderTyped();
renderAvailability();
connect();
