import { ws_url } from './api.ts';
import { Upscaler, type UpscaleBackend } from './upscale.ts';

/** RGB565 -> canvas RGBA (little-endian ABGR word) lookup, built once (256 KB). */
const LUT = (() => {
	const lut = new Uint32Array(65536);
	for (let v = 0; v < 65536; v++) {
		const r = (v >> 11) & 31;
		const g = (v >> 5) & 63;
		const b = v & 31;
		lut[v] =
			0xff000000 | (((b << 3) | (b >> 2)) << 16) | (((g << 2) | (g >> 4)) << 8) | ((r << 3) | (r >> 2));
	}
	return lut;
})();

export type ScreenStatus = 'connecting' | 'live' | 'nosignal' | 'offline';

/**
 * Streams the device LCD into a canvas. The device sends only the rows that
 * changed, uncompressed; decoding and drawing happen here, once per frame.
 */
export class ScreenStream {
	width = 320;
	height = 170;
	fps = 0;

	private ws?: WebSocket;
	private gpu?: Upscaler;
	private rgba?: Uint8Array;
	private px?: Uint32Array;
	private dirty_top = Infinity;
	private dirty_bottom = -1;
	private raf = 0;
	private closed = false;
	private retry = 0;
	private frames = 0;
	private fps_timer?: ReturnType<typeof setInterval>;

	constructor(
		private canvas: HTMLCanvasElement,
		private on_status: (s: ScreenStatus) => void,
		private on_size?: (w: number, h: number) => void,
		/** WebSocket path: /ws/screen (the real LCD) or /ws/vapp?id=… (a virtual copy) */
		private path = '/ws/screen',
		/** second canvas for AI upscaling (smooth mode) */
		private sr_canvas?: HTMLCanvasElement,
		private on_backend?: (b: UpscaleBackend) => void,
	) {
		this.connect();
		this.fps_timer = setInterval(() => {
			this.fps = this.frames;
			this.frames = 0;
		}, 1000);
	}

	private connect() {
		if (this.closed) return;
		this.on_status('connecting');
		const ws = new WebSocket(ws_url(this.path));
		ws.binaryType = 'arraybuffer';
		ws.onopen = () => {
			this.retry = 0;
			this.got_frame = false;
			// connected, but the device may not deliver pixels (no framebuffer)
			setTimeout(() => !this.got_frame && !this.closed && this.on_status('nosignal'), 2500);
		};
		ws.onmessage = (e) => {
			if (typeof e.data !== 'string') this.on_frame(e.data as ArrayBuffer);
		};
		ws.onclose = () => {
			if (this.closed) return;
			this.on_status('offline');
			const delay = Math.min(4000, 300 * 2 ** this.retry++);
			setTimeout(() => this.connect(), delay);
		};
		this.ws = ws;
	}

	private ensure(w: number, h: number) {
		if (this.rgba && this.width === w && this.height === h) return;
		this.width = w;
		this.height = h;
		this.rgba = new Uint8Array(w * h * 4);
		this.px = new Uint32Array(this.rgba.buffer);
		this.gpu ??= new Upscaler(this.canvas, this.sr_canvas, this.on_backend);
		this.gpu.smooth = this.smooth;
		this.gpu.update(this.rgba, w, h, 0, h);
		this.on_size?.(w, h);
	}

	private smooth = true;

	/** Smooth (fill the window, AI upscaled) or pixel (exact integer) upscaling. */
	set_smooth(on: boolean) {
		this.smooth = on;
		if (this.gpu) {
			this.gpu.smooth = on;
			this.gpu.draw();
		}
	}

	private got_frame = false;

	private on_frame(buf: ArrayBuffer) {
		if (!this.got_frame) {
			this.got_frame = true;
			this.on_status('live');
		}
		const dv = new DataView(buf);
		if (dv.getUint8(0) !== 2 || buf.byteLength < 10) return;
		const w = dv.getUint16(1, true);
		const h = dv.getUint16(3, true);
		const y0 = dv.getUint16(5, true);
		const n = dv.getUint16(7, true);
		this.ensure(w, h);
		const src = new Uint16Array(buf, 10, n * w);
		const px = this.px!;
		const base = y0 * w;
		for (let i = 0; i < src.length; i++) px[base + i] = LUT[src[i]];
		this.dirty_top = Math.min(this.dirty_top, y0);
		this.dirty_bottom = Math.max(this.dirty_bottom, y0 + n - 1);
		this.frames++;
		if (!this.raf) this.raf = requestAnimationFrame(this.flush);
	}

	private flush = () => {
		this.raf = 0;
		if (!this.gpu || !this.rgba || this.dirty_bottom < 0) return;
		const top = this.dirty_top;
		const rows = this.dirty_bottom - top + 1;
		this.gpu.update(this.rgba, this.width, this.height, top, rows);
		this.gpu.draw();
		this.dirty_top = Infinity;
		this.dirty_bottom = -1;
	};

	key(code: number, value: 0 | 1 | 2) {
		if (this.ws?.readyState === WebSocket.OPEN) {
			this.ws.send(JSON.stringify({ t: 'key', c: code, v: value }));
		}
	}

	/** Touch at (x, y) in screen pixels; apps on a virtual screen only. */
	pointer(x: number, y: number, down: boolean) {
		if (this.ws?.readyState === WebSocket.OPEN) {
			this.ws.send(JSON.stringify({ t: 'ptr', x: Math.round(x), y: Math.round(y), d: down }));
		}
	}

	/**
	 * How the current frame looks: `lit` is false when it is all black (the
	 * screen is off), `dark` is the share of near-black pixels in the middle,
	 * `locked` means the lock screen's prompt is showing.
	 */
	frame_stats(): { lit: boolean; dark: number; locked: boolean; unlock_key: number; home: boolean } {
		const px = this.rgba;
		if (!px) return { lit: false, dark: 1, locked: false, unlock_key: 15, home: false };
		let dark = 0;
		let n = 0;
		let max = 0;
		const y0 = Math.round(this.height * 0.18);
		const y1 = Math.round(this.height * 0.9);
		for (let y = y0; y < y1; y += 4) {
			for (let x = 0; x < this.width; x += 4) {
				const i = (y * this.width + x) * 4;
				const l = (px[i] * 3 + px[i + 1] * 6 + px[i + 2]) / 10;
				if (l < 28) dark++;
				if (l > max) max = l;
				n++;
			}
		}
		// The lock screen asks for ENTER or TAB at random ("Press ENTER to unlock" /
		// "Press TAB to unlock", yellow, top left). The ENTER line is longer: its
		// last column is past x = 157, the TAB one ends at 148.
		let end = 0;
		for (let y = 3; y < 20; y++) {
			for (let x = 0; x < 215; x++) {
				const i = (y * this.width + x) * 4;
				if (px[i] > 150 && px[i + 1] > 110 && px[i + 2] < 100 && x > end) end = x;
			}
		}
		// The launcher's home screen has its carousel arrows at both edges (x < 14
		// and x >= 306, around y = 87) and nothing else in those columns. The
		// pages drawn inside the launcher (CLI, Python, Settings, games) do not.
		const bright = (x0: number, x1: number, y0: number, y1: number) => {
			let c = 0;
			for (let y = y0; y < y1; y++)
				for (let x = x0; x < x1; x++) {
					const i = (y * this.width + x) * 4;
					if ((px[i] + px[i + 1] + px[i + 2]) / 3 > 100) c++;
				}
			return c;
		};
		const home =
			this.width === 320 &&
			end <= 100 &&
			bright(0, 14, 74, 100) >= 20 &&
			bright(306, 320, 74, 100) >= 20 &&
			bright(0, 14, 24, 70) + bright(0, 14, 104, 150) + bright(306, 320, 24, 70) + bright(306, 320, 104, 150) === 0;
		// the launcher's "ZERO" in the same place ends near x = 40
		return { lit: max > 16, dark: n ? dark / n : 1, locked: end > 100, unlock_key: end > 157 ? 28 : 15, home };
	}

	/** Press and release, for on-screen buttons. */
	tap(code: number) {
		this.key(code, 1);
		setTimeout(() => this.key(code, 0), 60);
	}

	/** PNG of exactly what is on screen (the upscaled view). */
	snapshot(): Promise<Blob | null> {
		const c = this.sr_canvas && this.sr_canvas.style.visibility === 'visible' ? this.sr_canvas : this.canvas;
		return new Promise((r) => c.toBlob(r, 'image/png'));
	}

	close() {
		this.closed = true;
		this.gpu?.destroy();
		clearInterval(this.fps_timer);
		cancelAnimationFrame(this.raf);
		this.ws?.close();
	}
}
