import { spawn } from 'node:child_process';

const email = process.env.OWNER_EMAIL;
const password = process.env.BOOTSTRAP_PASSWORD;
if (!email || !password) {
  throw new Error('OWNER_EMAIL and BOOTSTRAP_PASSWORD are required for one-time owner creation');
}

await new Promise((resolve, reject) => {
  const child = spawn(
    'pnpm',
    [
      '--filter', 'dashboard', 'exec', 'auth', 'create-admin',
      '--email', email, '--password', password, '--name', 'Owner', '--role', 'admin', '--yes',
    ],
    { stdio: 'inherit' },
  );
  child.once('error', reject);
  child.once('exit', (code, signal) => {
    if (code === 0) resolve();
    else reject(new Error(`Owner creation exited with ${signal ?? `code ${code}`}`));
  });
});
