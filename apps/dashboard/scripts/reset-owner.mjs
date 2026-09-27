import pg from 'pg';
import { hashPassword } from 'better-auth/crypto';

const email = process.env.OWNER_EMAIL;
const databaseURL = process.env.DATABASE_URL;
if (!email || !databaseURL) throw new Error('OWNER_EMAIL and DATABASE_URL are required');
let password = '';
for await (const chunk of process.stdin) password += chunk.toString();
password = password.trimEnd();
if (password.length < 12) throw new Error('New password must have at least 12 characters');
const pool = new pg.Pool({ connectionString: databaseURL });
const client = await pool.connect();
try {
  await client.query('BEGIN');
  const users = await client.query('SELECT id FROM "user" WHERE lower(email)=lower($1)', [email]);
  if (users.rowCount !== 1) throw new Error('Configured owner account not found');
  const id = users.rows[0].id;
  const hash = await hashPassword(password);
  const changed = await client.query(
    'UPDATE account SET password=$1 WHERE "userId"=$2 AND "providerId"=$3',
    [hash, id, 'credential'],
  );
  if (changed.rowCount !== 1) throw new Error('Credential account not found');
  await client.query('DELETE FROM session WHERE "userId"=$1', [id]);
  await client.query('COMMIT');
  console.log('Owner password reset and all sessions revoked.');
} catch (error) {
  await client.query('ROLLBACK');
  throw error;
} finally {
  client.release();
  await pool.end();
}
