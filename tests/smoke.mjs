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
  const deliveryIndex = await pool.query(
    `SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND indexname='deliveries_conversation_idx'`,
  );
  assert.equal(deliveryIndex.rowCount, 1, 'delivery conversation index');
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
  await fetch(`${apiURL}/p/${pixel}.gif`);
  assert.equal(
    Number(
      (
        await pool.query('SELECT count(*) AS count FROM open_events WHERE delivery_id=$1', [
          deliveryID,
        ])
      ).rows[0].count,
    ),
    1,
    'rapid repeat pixel requests are coalesced',
  );
  await pool.query(
    `UPDATE deliveries SET last_open_recorded_at=now()-interval '6 minutes' WHERE id=$1`,
    [deliveryID],
  );
  await fetch(`${apiURL}/p/${pixel}.gif`);
  assert.equal(
    Number(
      (
        await pool.query('SELECT count(*) AS count FROM open_events WHERE delivery_id=$1', [
          deliveryID,
        ])
      ).rows[0].count,
    ),
    2,
    'later pixel request is recorded',
  );
  await pool.query(
    `UPDATE deliveries SET recorded_open_count=1000,last_open_recorded_at=now()-interval '6 minutes' WHERE id=$1`,
    [deliveryID],
  );
  await fetch(`${apiURL}/p/${pixel}.gif`);
  assert.equal(
    Number(
      (
        await pool.query('SELECT count(*) AS count FROM open_events WHERE delivery_id=$1', [
          deliveryID,
        ])
      ).rows[0].count,
    ),
    2,
    'pixel event limit is enforced',
  );
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
  assert.equal(body.events.length, 2);
  assert.deepEqual(body.deliveries[0].replyAllRecipients, []);
  const filename = 'report-📈.pdf';
  const upload = await fetch(`${apiURL}/api/v1/attachments`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${token}`,
      'content-type': 'application/pdf',
      'x-file-name': encodeURIComponent(filename),
      'x-file-name-encoding': 'percent',
    },
    body: Buffer.from('test attachment'),
  });
  assert.equal(upload.status, 200, 'Unicode attachment upload');
  const uploaded = await upload.json();
  const attachment = await pool.query('SELECT filename FROM attachments WHERE id=$1', [
    uploaded.id,
  ]);
  assert.equal(attachment.rows[0].filename, filename);
  const rawFilename = '100%+ready.pdf';
  const rawUpload = await fetch(`${apiURL}/api/v1/attachments`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${token}`,
      'content-type': 'application/pdf',
      'x-file-name': rawFilename,
    },
    body: Buffer.from('legacy client upload'),
  });
  assert.equal(rawUpload.status, 200, 'literal filename upload remains supported');
  const rawAttachment = await pool.query('SELECT filename FROM attachments WHERE id=$1', [
    (await rawUpload.json()).id,
  ]);
  assert.equal(rawAttachment.rows[0].filename, rawFilename);
  const plusUpload = await fetch(`${apiURL}/api/v1/attachments`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${token}`,
      'content-type': 'application/pdf',
      'x-file-name': 'budget+final%20draft.pdf',
      'x-file-name-encoding': 'percent',
    },
    body: Buffer.from('encoded filename with a literal plus'),
  });
  assert.equal(plusUpload.status, 200, 'percent-encoded filename with literal plus');
  const plusAttachment = await pool.query('SELECT filename FROM attachments WHERE id=$1', [
    (await plusUpload.json()).id,
  ]);
  assert.equal(plusAttachment.rows[0].filename, 'budget+final draft.pdf');

  const sendKey = randomUUID();
  const draft = {
    mailboxId: mailboxID,
    to: ['first@example.invalid'],
    cc: [],
    bcc: [],
    subject: 'Smoke send',
    html: '<p>Hello</p>',
  };
  const send = (message) =>
    fetch(`${apiURL}/api/v1/send`, {
      method: 'POST',
      headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
      body: JSON.stringify({ draft: message, sendMode: 'shared', idempotencyKey: sendKey }),
    });
  const sendStream = await fetch(`${apiURL}/api/v1/events`, {
    headers: { authorization: `Bearer ${token}` },
  });
  assert.equal(sendStream.status, 200, 'send status stream');
  const sendReader = sendStream.body.getReader();
  assert.match(new TextDecoder().decode((await sendReader.read()).value), /event: ready/);
  const firstSend = await send(draft);
  assert.equal(firstSend.status, 200, 'first send attempt was recorded');
  const firstResult = await firstSend.json();
  assert.equal(firstResult.deliveries[0].status, 'pending', 'send is accepted asynchronously');
  const sendChanged = await Promise.race([
    sendReader.read(),
    new Promise((_, reject) => setTimeout(() => reject(new Error('send status SSE missing')), 5000)),
  ]);
  assert.match(new TextDecoder().decode(sendChanged.value), /event: changed/);
  await sendReader.cancel();
  let failedConversation;
  for (let attempt = 0; attempt < 40; attempt++) {
    const response = await fetch(`${apiURL}/api/v1/conversations/${firstResult.conversationId}`, {
      headers: { authorization: `Bearer ${token}` },
    });
    failedConversation = await response.json();
    if (failedConversation.status === 'partial_or_failed') break;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert.equal(failedConversation.status, 'partial_or_failed', 'background send outcome persisted');
  const repeatSend = await send(draft);
  assert.equal(repeatSend.status, 200, 'same request may be retried');
  assert.equal((await repeatSend.json()).conversationId, firstResult.conversationId);
  const savedDraftRetry = await send({ ...draft, id: randomUUID() });
  assert.equal(savedDraftRetry.status, 200, 'saving the draft preserves the retry key');
  assert.equal((await savedDraftRetry.json()).conversationId, firstResult.conversationId);
  const changedSend = await send({ ...draft, to: ['other@example.invalid'] });
  assert.equal(changedSend.status, 409, 'same key cannot send changed content');
  await pool.query('UPDATE conversations SET request_hash=NULL WHERE id=$1', [
    firstResult.conversationId,
  ]);
  const legacyRepeat = await send(draft);
  assert.equal(legacyRepeat.status, 409, 'pre-upgrade send needs manual review');
  assert.match((await legacyRepeat.json()).error, new RegExp(firstResult.conversationId));
  console.log(
    'Smoke checks passed: login, authorization, pixel limits, Unicode upload, idempotency, and SSE.',
  );
} finally {
  if (mailboxID) await pool.query('DELETE FROM mailboxes WHERE id=$1', [mailboxID]);
  await pool.end();
}
