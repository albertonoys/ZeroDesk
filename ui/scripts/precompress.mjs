import { fileURLToPath } from 'node:url';
// Writes foo.js.gz next to every text asset so the device serves
// pre-compressed files and never compresses at request time.
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { gzipSync, constants } from 'node:zlib';

const root = fileURLToPath(new URL('../../agent/web/', import.meta.url));
const exts = /\.(js|css|html|svg|json|wasm|txt)$/;
let saved = 0;
function walk(dir) {
	for (const name of readdirSync(dir)) {
		const p = join(dir, name);
		if (statSync(p).isDirectory()) walk(p);
		else if (exts.test(name)) {
			const raw = readFileSync(p);
			const gz = gzipSync(raw, { level: constants.Z_BEST_COMPRESSION });
			if (gz.length < raw.length * 0.9) {
				writeFileSync(p + '.gz', gz);
				saved += raw.length - gz.length;
			}
		}
	}
}
walk(root);
console.log(`precompressed, ${(saved / 1024).toFixed(0)} KB saved on the wire`);
