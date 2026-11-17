/**
 * GPU upscaler for the 320x170 LCD stream.
 *
 * "smooth": Catmull-Rom bicubic (9-tap, using hardware bilinear filtering),
 *   which keeps anti-aliased text edges round instead of stair-stepped.
 * "pixel": nearest neighbour, the original pixel look.
 *
 * The device sends raw pixels; all scaling work happens here on the GPU.
 */
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
uniform int smooth_;
in vec2 uv;
out vec4 color;

// Catmull-Rom with 9 bilinear taps (after Jorge Jimenez / MJP).
vec4 bicubic(vec2 uv) {
	vec2 pos = uv * size;
	vec2 c = floor(pos - 0.5) + 0.5;
	vec2 f = pos - c;
	vec2 w0 = f * (-0.5 + f * (1.0 - 0.5 * f));
	vec2 w1 = 1.0 + f * f * (-2.5 + 1.5 * f);
	vec2 w2 = f * (0.5 + f * (2.0 - 1.5 * f));
	vec2 w3 = f * f * (-0.5 + 0.5 * f);
	vec2 w12 = w1 + w2;
	vec2 o12 = w2 / w12;
	vec2 t0 = (c - 1.0) / size;
	vec2 t3 = (c + 2.0) / size;
	vec2 t12 = (c + o12) / size;
	vec4 r = vec4(0.0);
	r += texture(tex, vec2(t0.x, t0.y)) * w0.x * w0.y;
	r += texture(tex, vec2(t12.x, t0.y)) * w12.x * w0.y;
	r += texture(tex, vec2(t3.x, t0.y)) * w3.x * w0.y;
	r += texture(tex, vec2(t0.x, t12.y)) * w0.x * w12.y;
	r += texture(tex, vec2(t12.x, t12.y)) * w12.x * w12.y;
	r += texture(tex, vec2(t3.x, t12.y)) * w3.x * w12.y;
	r += texture(tex, vec2(t0.x, t3.y)) * w0.x * w3.y;
	r += texture(tex, vec2(t12.x, t3.y)) * w12.x * w3.y;
	r += texture(tex, vec2(t3.x, t3.y)) * w3.x * w3.y;
	return clamp(r, 0.0, 1.0);
}

void main() {
	if (smooth_ == 1) {
		// bicubic, then a light unsharp mask measured half a source texel
		// away, so anti-aliased text edges read crisper without halos
		vec3 c = bicubic(uv).rgb;
		vec2 d = 0.5 / size;
		vec3 n = bicubic(uv + vec2(d.x, 0.0)).rgb + bicubic(uv - vec2(d.x, 0.0)).rgb
			+ bicubic(uv + vec2(0.0, d.y)).rgb + bicubic(uv - vec2(0.0, d.y)).rgb;
		vec3 sharp = c + 0.55 * (c - n * 0.25);
		color = vec4(clamp(sharp, 0.0, 1.0), 1.0);
	} else {
		color = vec4(texelFetch(tex, ivec2(uv * size), 0).rgb, 1.0);
	}
}`;

export class Upscaler {
	private gl: WebGL2RenderingContext;
	private tex: WebGLTexture;
	private uSize: WebGLUniformLocation;
	private uSmooth: WebGLUniformLocation;
	private w = 0;
	private h = 0;
	private ro: ResizeObserver;
	smooth = true;

	constructor(private canvas: HTMLCanvasElement) {
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
			this.draw();
		}
	}

	/** Upload rows [top, top+rows) of an RGBA frame of size w x h. */
	update(rgba: Uint8Array, w: number, h: number, top: number, rows: number) {
		const gl = this.gl;
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
		gl.uniform1i(this.uSmooth, this.smooth ? 1 : 0);
		gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
	}

	destroy() {
		this.ro.disconnect();
		this.gl.getExtension('WEBGL_lose_context')?.loseContext();
	}
}
