/**
 * Upscaler for the 320x170 LCD stream (and virtual screens).
 *
 * "pixel": nearest neighbour. The view picks an integer number of *device*
 *   pixels per LCD pixel and snaps the canvas to the device-pixel grid, so
 *   every LCD pixel is an exact N×N block: as sharp as the panel itself.
 * "smooth": fills the window. AI upscaling with WebSR (@websr/webgl, a small
 *   CNN trained for clean upscaling) on its own canvas; until it is ready, or
 *   where WebGL2 float targets are missing, a sharp-bilinear shader
 *   (integer prescale + bilinear for the fractional rest) on the main canvas.
 *   WebGPU is not used: it needs a secure context and ZeroDesk is plain HTTP.
 *
 * The device sends raw pixels; all scaling work happens here on the GPU.
 */
import WebSRWebGL from '@websr/webgl';

const VERT = `#version 300 es
in vec2 a;
out vec2 uv;
void main() {
	uv = vec2(a.x * 0.5 + 0.5, 0.5 - a.y * 0.5);
	gl_Position = vec4(a, 0.0, 1.0);
}`;

const FRAG = `#version 300 es
precision highp float;
uniform sampler2D tex;
uniform vec2 size;     // source size in texels
uniform vec2 out_size; // canvas size in device pixels
uniform int smooth_;
in vec2 uv;
out vec4 color;

void main() {
	if (smooth_ == 1) {
		// sharp-bilinear (as RetroArch's sharp-bilinear-simple): nearest within the
		// integer part of the scale, bilinear only across the output pixel that
		// straddles two texels; relies on LINEAR texture filtering
		vec2 scale = max(floor(out_size / size), vec2(1.0));
		vec2 texel = uv * size;
		vec2 center_dist = fract(texel) - 0.5;
		vec2 region = 0.5 - 0.5 / scale;
		vec2 f = (center_dist - clamp(center_dist, -region, region)) * scale + 0.5;
		color = vec4(texture(tex, (floor(texel) + f) / size).rgb, 1.0);
	} else {
		color = vec4(texelFetch(tex, ivec2(min(uv * size, size - 1.0)), 0).rgb, 1.0);
	}
}`;

export type UpscaleBackend = 'pixel' | 'sharp' | 'websr';

export class Upscaler {
	private gl: WebGL2RenderingContext;
	private tex: WebGLTexture;
	private uSize: WebGLUniformLocation;
	private uOut: WebGLUniformLocation;
	private uSmooth: WebGLUniformLocation;
	private w = 0;
	private h = 0;
	private rgba?: Uint8Array;
	private ro: ResizeObserver;
	private sr?: WebSRWebGL;
	private sr_state: 'off' | 'loading' | 'ready' | 'failed' = 'off';
	private sr_busy = false;
	private sr_again = false;
	private sr_drawn = false;
	smooth = true;

	constructor(
		private canvas: HTMLCanvasElement,
		/** separate canvas for WebSR (a canvas can only have one WebGL context) */
		private sr_canvas?: HTMLCanvasElement,
		/** which path is on screen, so the view can show the right canvas */
		private on_backend?: (b: UpscaleBackend) => void,
	) {
		const gl = canvas.getContext('webgl2', { alpha: false, antialias: false, desynchronized: true, preserveDrawingBuffer: true });
		if (!gl) throw new Error('WebGL2 unavailable');
		this.gl = gl;
		const prog = gl.createProgram()!;
		for (const [type, src] of [
			[gl.VERTEX_SHADER, VERT],
			[gl.FRAGMENT_SHADER, FRAG],
		] as const) {
			const s = gl.createShader(type)!;
			gl.shaderSource(s, src);
			gl.compileShader(s);
			if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) ?? 'shader');
			gl.attachShader(prog, s);
		}
		gl.linkProgram(prog);
		gl.useProgram(prog);
		const buf = gl.createBuffer();
		gl.bindBuffer(gl.ARRAY_BUFFER, buf);
		gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
		const loc = gl.getAttribLocation(prog, 'a');
		gl.enableVertexAttribArray(loc);
		gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
		this.tex = gl.createTexture()!;
		gl.bindTexture(gl.TEXTURE_2D, this.tex);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
		this.uSize = gl.getUniformLocation(prog, 'size')!;
		this.uOut = gl.getUniformLocation(prog, 'out_size')!;
		this.uSmooth = gl.getUniformLocation(prog, 'smooth_')!;

		// Backing store follows the on-screen size at device pixel ratio.
		this.ro = new ResizeObserver(() => this.resize());
		this.ro.observe(canvas);
	}

	private resize() {
		const dpr = window.devicePixelRatio || 1;
		const w = Math.max(1, Math.round(this.canvas.clientWidth * dpr));
		const h = Math.max(1, Math.round(this.canvas.clientHeight * dpr));
		if (this.canvas.width !== w || this.canvas.height !== h) {
			this.canvas.width = w;
			this.canvas.height = h;
			this.sr?.setOutputResolution({ width: w, height: h });
			this.draw();
		}
	}

	/** Upload rows [top, top+rows) of an RGBA frame of size w x h. */
	update(rgba: Uint8Array, w: number, h: number, top: number, rows: number) {
		const gl = this.gl;
		this.rgba = rgba;
		gl.bindTexture(gl.TEXTURE_2D, this.tex);
		if (w !== this.w || h !== this.h) {
			this.w = w;
			this.h = h;
			gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, rgba);
			return;
		}
		gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, top, w, rows, gl.RGBA, gl.UNSIGNED_BYTE, rgba.subarray(top * w * 4, (top + rows) * w * 4));
	}

	draw() {
		if (!this.w) return;
		const gl = this.gl;
		gl.viewport(0, 0, this.canvas.width, this.canvas.height);
		gl.uniform2f(this.uSize, this.w, this.h);
		gl.uniform2f(this.uOut, this.canvas.width, this.canvas.height);
		gl.uniform1i(this.uSmooth, this.smooth ? 1 : 0);
		gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
		if (!this.smooth) {
			this.on_backend?.('pixel');
			return;
		}
		if (this.sr_state === 'off') this.load_sr();
		if (this.sr_state === 'ready') this.render_sr();
		else this.on_backend?.('sharp');
	}

	private async load_sr() {
		if (!this.sr_canvas) return void (this.sr_state = 'failed');
		this.sr_state = 'loading';
		try {
			const gl = WebSRWebGL.initWebGL(this.sr_canvas);
			if (!gl) throw new Error('no WebGL2 float targets');
			const name = WebSRWebGL.supportsNetwork('websr/cnn-m', gl) ? 'websr/cnn-m' : 'websr/cnn-s';
			if (!WebSRWebGL.supportsNetwork(name, gl)) throw new Error('network unsupported');
			const buf = await (await fetch(`/websr/${name.slice(6)}.bin`)).arrayBuffer();
			this.sr = new WebSRWebGL({
				canvas: this.sr_canvas,
				gl,
				network_name: name,
				weights: WebSRWebGL.parseBinaryWeights(buf),
				outputResolution: { width: this.canvas.width, height: this.canvas.height },
			});
			this.sr_state = 'ready';
			if (this.smooth) this.render_sr();
		} catch (e) {
			console.warn('WebSR unavailable, using sharp-bilinear:', e);
			this.sr_state = 'failed';
		}
	}

	/** One WebSR pass of the current frame; frames arriving meanwhile coalesce. */
	private async render_sr() {
		if (!this.sr || !this.rgba) return;
		if (this.sr_busy) return void (this.sr_again = true);
		this.sr_busy = true;
		try {
			do {
				this.sr_again = false;
				const img = new ImageData(new Uint8ClampedArray(this.rgba), this.w, this.h);
				const bmp = await createImageBitmap(img);
				await this.sr.render(bmp);
				bmp.close();
				if (!this.sr_drawn) this.sr_drawn = true;
				if (this.smooth) this.on_backend?.('websr');
			} while (this.sr_again && this.smooth);
		} catch (e) {
			console.warn('WebSR render failed, using sharp-bilinear:', e);
			this.sr_state = 'failed';
			this.on_backend?.('sharp');
		} finally {
			this.sr_busy = false;
		}
	}

	destroy() {
		this.ro.disconnect();
		this.sr?.destroy();
		this.gl.getExtension('WEBGL_lose_context')?.loseContext();
	}
}
