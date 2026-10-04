import { createRequire } from 'node:module';
import { copyFileSync, cpSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
const require = createRequire(import.meta.url);
const target = new URL('../public/', import.meta.url);
mkdirSync(target, { recursive: true });
const worker = require.resolve('pdfjs-dist/build/pdf.worker.mjs');
copyFileSync(worker, new URL('pdf.worker.mjs', target));
const root = dirname(dirname(worker));
for (const folder of ['cmaps', 'standard_fonts', 'wasm'])
  cpSync(join(root, folder), new URL('pdf-assets/' + folder, target), { recursive: true });
