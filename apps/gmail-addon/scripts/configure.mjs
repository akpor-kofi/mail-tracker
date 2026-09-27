import { readFileSync, writeFileSync } from 'node:fs';
const raw = process.argv[2];
if (!raw) {
  console.error('Usage: pnpm --filter gmail-addon configure https://mail.example.com');
  process.exit(1);
}
const url = new URL(raw);
if (url.protocol !== 'https:' || url.pathname !== '/' || url.search || url.hash)
  throw new Error('Use a public HTTPS origin without a path');
const origin = url.origin;
const manifest = readFileSync(
  new URL('../appsscript.template.json', import.meta.url),
  'utf8',
).replaceAll('__PUBLIC_URL__', origin);
writeFileSync(new URL('../src/appsscript.json', import.meta.url), manifest);
writeFileSync(
  new URL('../src/Config.gs', import.meta.url),
  `var PUBLIC_URL = ${JSON.stringify(origin)};\n`,
);
console.log(
  `Configured Apps Script for ${origin}. Push this folder to your own Apps Script project.`,
);
