/**
 * Overall progress (0..1) of an install streamed by the Zero (agent/store.go):
 * - the package download, "[download] <percent>" (App Store only): 0 → 40 %
 * - apt, when dependencies are missing: its preparation lines, then
 *   "dlstatus:" (dependencies downloading) and "pmstatus:" (dpkg) lines
 * - dpkg, when nothing is missing: "processing:" and "status:" lines
 * null until anything has been reported.
 */
export function install_progress(log: string, has_download: boolean): number | null {
	const last = (re: RegExp) => {
		const all = [...log.matchAll(re)];
		return all.length ? Math.min(100, Math.max(0, parseFloat(all[all.length - 1][1]))) / 100 : null;
	};
	const has = (re: RegExp) => re.test(log);

	// dpkg's own stages
	if (has(/^status: [^:]+: installed/m)) return 1;
	if (has(/^processing: configure:/m)) return 0.88;
	if (has(/^status: [^:]+: unpacked/m)) return 0.78;
	if (has(/^processing: (install|upgrade):/m)) return 0.6;

	// apt
	const pm = last(/^pmstatus:[^:\n]*:([\d.]+):/gm);
	if (pm !== null) return 0.55 + pm * 0.45;
	const dl = last(/^dlstatus:[^:\n]*:([\d.]+):/gm);
	if (dl !== null) return 0.52 + dl * 0.03;
	if (has(/^Reading state information/m)) return 0.52;
	if (has(/^Building dependency tree/m)) return 0.48;
	if (has(/^Reading package lists/m)) return 0.43;

	if (!has_download) return null;
	const own = last(/^\[download\] (\d+)/gm);
	return own === null ? null : own * 0.4;
}

/** The human-readable part of the newest apt status line. */
export function install_step(log: string): string {
	const all = [...log.matchAll(/^(?:pm|dl)status:[^:\n]*:[\d.]+:(.*)$/gm)];
	return all.length ? all[all.length - 1][1].trim() : '';
}
