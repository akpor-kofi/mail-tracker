import { betterAuth } from 'better-auth';
import { admin, jwt } from 'better-auth/plugins';
import { Pool } from 'pg';

export const auth = betterAuth({
  baseURL: process.env.PUBLIC_URL!,
  secret: process.env.BETTER_AUTH_SECRET!,
  database: new Pool({ connectionString: process.env.DATABASE_URL! }),
  advanced: { database: { validateSchema: process.env.BUILD_PHASE !== '1' } },
  emailAndPassword: { enabled: true, disableSignUp: true },
  plugins: [jwt(), admin()],
});
