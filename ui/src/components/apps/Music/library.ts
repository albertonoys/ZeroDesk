import { api } from '🍎/lib/api.ts';

export type Track = {
	path: string;
	title: string;
	ext: string;
	size: number;
	modified: number;
	duration?: number;
};

type Entry = { name: string; path: string; dir: boolean; size: number; modified: string; hidden?: boolean };

export const AUDIO_EXT = ['mp3', 'm4a', 'aac', 'flac', 'wav', 'ogg', 'opus'];

/** Audio files under a directory on the Zero, a few levels deep. A missing folder is an empty library. */
export async function list_audio(root: string, depth = 3): Promise<Track[]> {
	const out: Track[] = [];
	async function walk(dir: string, level: number) {
		let res: { entries: Entry[] };
		try {
			res = await api<{ entries: Entry[] }>(`/api/fs/list?path=${encodeURIComponent(dir)}`);
		} catch {
			return;
		}
		for (const e of res.entries) {
			if (e.hidden) continue;
			if (e.dir) {
				if (level < depth) await walk(e.path, level + 1);
				continue;
			}
			const dot = e.name.lastIndexOf('.');
			const ext = dot > 0 ? e.name.slice(dot + 1).toLowerCase() : '';
			if (!AUDIO_EXT.includes(ext)) continue;
			out.push({
				path: e.path,
				title: e.name.slice(0, dot),
				ext,
				size: e.size,
				modified: Date.parse(e.modified),
			});
		}
	}
	await walk(root, 1);
	return out.sort((a, b) => a.title.localeCompare(b.title));
}

/** m:ss, or h:mm:ss for long files */
export function fmt_time(s: number): string {
	if (!isFinite(s) || s < 0) s = 0;
	const h = Math.floor(s / 3600);
	const m = Math.floor((s % 3600) / 60);
	const sec = Math.floor(s % 60);
	const ss = String(sec).padStart(2, '0');
	return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
}

/** Upload files into a directory on the Zero (created when missing), with progress 0–100. */
export function upload_files(dir: string, files: File[], progress: (pct: number) => void): Promise<void> {
	return new Promise((resolve, reject) => {
		const fd = new FormData();
		for (const f of files) fd.append('file', f, f.name);
		const xhr = new XMLHttpRequest();
		progress(0);
		xhr.upload.onprogress = (e) => e.lengthComputable && progress(Math.round((e.loaded / e.total) * 100));
		xhr.onloadend = () => (xhr.status === 200 ? resolve() : reject(new Error(xhr.responseText || `HTTP ${xhr.status}`)));
		xhr.open('POST', `/api/fs/upload?dir=${encodeURIComponent(dir)}`);
		xhr.send(fd);
	});
}
