import { persisted } from './persisted.svelte.ts';

/** Daytime 07:00–19:00 is light, the rest of the day dark. */
function scheme_for_time(d = new Date()): 'light' | 'dark' {
	const h = d.getHours();
	return h >= 7 && h < 19 ? 'light' : 'dark';
}

export type Scheme = 'light' | 'dark';
export type Appearance = 'auto' | 'light' | 'dark';
export type Lang = 'zh' | 'en';


export const preferences = persisted('zerodesk:preferences', {
	reduced_motion: matchMedia('(prefers-reduced-motion)').matches as boolean,
	/** what the user picked; `theme.scheme` is the scheme actually shown */
	appearance: 'auto' as Appearance,
	theme: { scheme: scheme_for_time() as Scheme },
	lang: 'en' as Lang,
	device_scale: 3 as number,
	screen_smooth: true as boolean,
});

function apply_scheme(next: Scheme) {
	if (preferences.theme.scheme === next) return;
	const doc = document as Document & { startViewTransition?: (cb: () => void) => unknown };
	if (doc.startViewTransition && !preferences.reduced_motion) {
		doc.startViewTransition(() => (preferences.theme.scheme = next));
	} else {
		preferences.theme.scheme = next;
	}
}

$effect.root(() => {
	// follow the appearance choice; in auto mode re-check the clock every minute
	$effect(() => {
		const a = preferences.appearance;
		apply_scheme(a === 'auto' ? scheme_for_time() : a);
		if (a !== 'auto') return;
		const timer = setInterval(() => apply_scheme(scheme_for_time()), 60_000);
		return () => clearInterval(timer);
	});
	$effect(() => {
		const { classList } = document.body;
		classList.remove('light', 'dark');
		classList.add(preferences.theme.scheme);
		document.documentElement.style.colorScheme = preferences.theme.scheme;
	});
	$effect(() => {
		document.documentElement.lang = preferences.lang === 'zh' ? 'zh-CN' : 'en';
	});
});

/** App icons have a light and a dark version, like macOS 26/27; they follow the scheme. */
export function icon_look(): 'default' | 'dark' {
	return preferences.theme.scheme === 'dark' ? 'dark' : 'default';
}

/** Flip between light and dark, leaving auto mode. */
export function toggle_scheme() {
	preferences.appearance = preferences.theme.scheme === 'light' ? 'dark' : 'light';
}

export function toggle_lang() {
	preferences.lang = preferences.lang === 'zh' ? 'en' : 'zh';
}
