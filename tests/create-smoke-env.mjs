import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { randomBytes } from 'node:crypto';

const target = new URL('../infra/.env', import.meta.url);
if (existsSync(target)) throw new Error('infra/.env already exists; refusing to overwrite it');
const template = readFileSync(new URL('../infra/.env.example', import.meta.url), 'utf8');
const env = template
  .replaceAll('mail.example.com', 'example.invalid')
  .replace('replace-with-long-random-value', randomBytes(32).toString('hex'))
  .replace('replace-with-at-least-32-random-characters', randomBytes(48).toString('hex'))
  .replace('replace-with-base64-encoded-32-byte-key', randomBytes(32).toString('base64'))
  .replace('you@example.com', 'owner@example.invalid')
  .replace(
    'your-web-oauth-client.apps.googleusercontent.com',
    'dummy-web.apps.googleusercontent.com',
  )
  .replace('your-web-oauth-secret', 'dummy-secret')
  .replace(
    'your-apps-script-oauth-client.apps.googleusercontent.com',
    'dummy-addon.apps.googleusercontent.com',
  );
writeFileSync(target, env, { mode: 0o600 });
