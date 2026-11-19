// Phone layout: on a narrow screen ZeroDesk is laid out like iOS instead of
// macOS: a home screen of app icons and one app at a time, full screen.
const QUERY = '(max-width: 640px), (max-height: 500px) and (pointer: coarse)';

export const phone = $state({
	active: typeof matchMedia === 'function' && matchMedia(QUERY).matches,
	/** the home screen is in front (apps stay open behind it, as on iOS) */
	home: true,
});

if (typeof matchMedia === 'function') {
	matchMedia(QUERY).addEventListener('change', (e) => (phone.active = e.matches));
}

export function go_home() {
	phone.home = true;
}
