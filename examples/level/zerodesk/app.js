// Level: example ZeroDesk web app (docs/APP-SPEC.md). Uses the SDK for
// theme, language, storage and notifications, and its own backend for the IMU.
const TEXT = {
	en: { roll: 'Roll', pitch: 'Pitch', zero: 'Set Zero', reset: 'Reset', level: 'Level', no_sensor: 'No accelerometer', is_level: 'The device is level.' },
	zh: { roll: '横滚', pitch: '俯仰', zero: '置零', reset: '复位', level: '水平仪', no_sensor: '没有加速度计', is_level: '设备已水平。' },
};
const $ = (id) => document.getElementById(id);
let lang = 'en';
let offset = { roll: 0, pitch: 0 };
let last = null;
let level_since = 0;
let notified = false;

function tr(key) {
	return (TEXT[lang] || TEXT.en)[key];
}

function apply_lang(l) {
	lang = l === 'zh' ? 'zh' : 'en';
	document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
	for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = tr(el.dataset.i18n);
	zd.window.setTitle(tr('level'));
}

function show(t) {
	const roll = t.roll - offset.roll;
	const pitch = t.pitch - offset.pitch;
	$('roll').textContent = `${roll.toFixed(1)}°`;
	$('pitch').textContent = `${pitch.toFixed(1)}°`;
	// the bubble floats to the high side; ±10° reaches the rim
	const k = 39 / 10;
	const x = Math.max(-39, Math.min(39, -roll * k));
	const y = Math.max(-39, Math.min(39, pitch * k));
	const bubble = $('bubble');
	bubble.style.transform = `translate(${(x / 22) * 100}%, ${(y / 22) * 100}%)`;
	const is_level = Math.abs(roll) < 0.5 && Math.abs(pitch) < 0.5;
	bubble.classList.toggle('level', is_level);
	if (is_level) {
		level_since ||= Date.now();
		if (!notified && Date.now() - level_since > 2000) {
			notified = true;
			zd.notify({ title: tr('level'), body: tr('is_level') }).catch(() => {});
		}
	} else {
		level_since = 0;
		if (Math.abs(roll) > 2 || Math.abs(pitch) > 2) notified = false;
	}
}

async function poll() {
	try {
		const res = await fetch('api/tilt', { cache: 'no-store' });
		if (!res.ok) throw new Error((await res.json()).error);
		last = await res.json();
		$('status').textContent = '';
		show(last);
	} catch (e) {
		$('status').textContent = tr('no_sensor');
	}
	setTimeout(poll, 150);
}

$('zero').onclick = async () => {
	if (!last) return;
	offset = { roll: last.roll, pitch: last.pitch };
	await zd.storage.set('offset', offset);
	show(last);
};
$('reset').onclick = async () => {
	offset = { roll: 0, pitch: 0 };
	await zd.storage.remove('offset');
	if (last) show(last);
};

zd.ready.then(async (info) => {
	apply_lang(info.lang);
	zd.on('lang', apply_lang);
	offset = (await zd.storage.get('offset')) || offset;
	poll();
});
