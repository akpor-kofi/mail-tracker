import assert from 'node:assert/strict';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import pg from 'pg';

const authURL = process.env.TEST_AUTH_URL || 'http://dashboard:3000';
const apiURL = process.env.TEST_API_URL || 'http://api:8080';
const origin = process.env.PUBLIC_URL;
const email = process.env.TEST_OWNER_EMAIL;
const password = process.env.TEST_OWNER_PASSWORD;
if (!origin || !email || !password || !process.env.DATABASE_URL)
  throw new Error('PUBLIC_URL, TEST_OWNER_EMAIL, TEST_OWNER_PASSWORD, DATABASE_URL required');
const pool = new pg.Pool({ connectionString: process.env.DATABASE_URL });
let mailboxID;
try {
  const login = await fetch(`${authURL}/api/auth/sign-in/email`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', origin, host: new URL(origin).host },
    body: JSON.stringify({ email, password }),
  });
  assert.equal(login.status, 200, 'owner sign-in');
  const cookie = login.headers.get('set-cookie')?.split(';')[0];
  assert.ok(cookie);
  const tokenResponse = await fetch(`${authURL}/api/auth/token`, {
    headers: { cookie, host: new URL(origin).host },
  });
  assert.equal(tokenResponse.status, 200, 'JWT issue');
  const { token } = await tokenResponse.json();
  assert.ok(token);
  assert.equal((await fetch(`${apiURL}/api/v1/mailboxes`)).status, 401, 'unauthorized request');
  assert.equal(
    (await fetch(`${apiURL}/api/v1/mailboxes`, { headers: { authorization: `Bearer ${token}` } }))
      .status,
    200,
    'owner request',
  );
  const owner = (await pool.query('SELECT id FROM "user" WHERE email=$1', [email])).rows[0].id;
  mailboxID = randomUUID();
  const conversationID = randomUUID();
  const deliveryID = randomUUID();
  const pixel = randomBytes(32).toString('base64url');
  const hash = createHash('sha256').update(pixel).digest();
  await pool.query(
    `INSERT INTO mailboxes(id,owner_id,google_sub,email,encrypted_refresh_token) VALUES($1,$2,$3,$4,$5)`,
    [mailboxID, owner, `smoke-${mailboxID}`, 'smoke@example.invalid', Buffer.from('test')],
  );
  await pool.query(
    `INSERT INTO conversations(id,owner_id,mailbox_id,subject,mode,status) VALUES($1,$2,$3,'Smoke test','addon','prepared')`,
    [conversationID, owner, mailboxID],
  );
  await pool.query(
    `INSERT INTO deliveries(id,conversation_id,recipients,pixel_token_hash,status) VALUES($1,$2,$3,$4,'prepared')`,
    [deliveryID, conversationID, ['recipient@example.invalid'], hash],
  );
  const stream = await fetch(`${apiURL}/api/v1/events`, {
    headers: { authorization: `Bearer ${token}` },
  });
  assert.equal(stream.status, 200, 'event stream');
  const reader = stream.body.getReader();
  const first = await reader.read();
  assert.match(new TextDecoder().decode(first.value), /event: ready/);
  const image = await fetch(`${apiURL}/p/${pixel}.gif`);
  assert.equal(image.status, 200, 'pixel response');
  assert.equal(image.headers.get('content-type'), 'image/gif');
  assert.equal((await image.arrayBuffer()).byteLength, 43);
  const changed = await Promise.race([
    reader.read(),
    new Promise((_, reject) => setTimeout(() => reject(new Error('SSE update missing')), 5000)),
  ]);
  assert.match(new TextDecoder().decode(changed.value), /event: changed/);
  await reader.cancel();
  const detail = await fetch(`${apiURL}/api/v1/conversations/${conversationID}`, {
    headers: { authorization: `Bearer ${token}` },
  });
  assert.equal(detail.status, 200);
  const body = await detail.json();
  assert.equal(body.openStatus, 'open_detected');
  assert.equal(body.events.length, 1);
  assert.deepEqual(body.deliveries[0].replyAllRecipients, []);
  console.log('Smoke checks passed: login, JWKS, authorization, pixel, persistence, and SSE.');
} finally {
  if (mailboxID) await pool.query('DELETE FROM mailboxes WHERE id=$1', [mailboxID]);
  await pool.end();
}
