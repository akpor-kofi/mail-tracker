import { spawn } from 'node:child_process';
import { Client } from 'pg';

// Shared with apps/api/internal/platform/db/db.go.
const migrationLock = [1297371723, 1];

const client = new Client({ connectionString: process.env.DATABASE_URL });
await client.connect();
try {
  await client.query('SELECT pg_advisory_lock($1::integer, $2::integer)', migrationLock);
  await new Promise((resolve, reject) => {
    const child = spawn('pnpm', ['--filter', 'dashboard', 'exec', 'auth', 'migrate', '--yes'], {
      stdio: 'inherit',
    });
    child.once('error', reject);
    child.once('exit', (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`Better Auth migration exited with ${signal ?? `code ${code}`}`));
    });
  });
} finally {
  // Session close also releases the lock if the migration command failed.
  await client.end();
}
