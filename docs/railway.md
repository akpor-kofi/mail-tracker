# Deploy on Railway

Railway uses four services in one project: `Postgres`, `api`, `dashboard`, and a small public `edge` router. Only `edge` needs a public domain. The router sends `/api/v1/*`, `/oauth/google/callback`, and `/p/*` to Go, and everything else to Next.js. Railway handles HTTPS; the router listens on private HTTP port 8080. Keep the public domain stable after creating Google OAuth credentials.

## Create the project

1. Create a private Railway project and add its standard PostgreSQL template. Keep the database on its persistent volume and private network. Enable Railway volume backups before relying on stored mailboxes or conversations.
2. Add three services from this repository's `main` branch:

   | Service | Root directory | Dockerfile path | Port | Volume |
   | --- | --- | --- | --- | --- |
   | `api` | `/apps/api` | `Dockerfile` | 8080 | `/app/data` |
   | `dashboard` | repository root | `apps/dashboard/Dockerfile` | 3000 | none |
   | `edge` | repository root | `infra/railway-edge/Dockerfile` | 8080 | none |

3. Give `edge` a Railway public domain targeting port 8080. Use `https://YOUR_EDGE_DOMAIN` as `PUBLIC_URL`. The API and dashboard stay private. The service names must remain `api` and `dashboard` because the router uses their Railway private DNS names.
4. Set these Railway service variables. Railway variable references are entered literally and resolved inside the project:

   | Service | Variables |
   | --- | --- |
   | `api` | `DATABASE_URL=${{Postgres.DATABASE_URL}}`, `PUBLIC_URL`, `INSTANCE_SECRET`, `OWNER_EMAIL`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_ADDON_CLIENT_ID`, `JWKS_URL=http://dashboard.railway.internal:3000/api/auth/jwks`, `ATTACHMENT_DIR=/app/data`, `PORT=8080` |
   | `dashboard` | `DATABASE_URL=${{Postgres.DATABASE_URL}}`, `PUBLIC_URL`, `BETTER_AUTH_SECRET`, `OWNER_EMAIL`, `PORT=3000` |
   | `edge` | `PORT=8080` |

   Generate `INSTANCE_SECRET` with `openssl rand -base64 32` and `BETTER_AUTH_SECRET` with `openssl rand -hex 32`. Save both in a password manager. Losing `INSTANCE_SECRET` makes stored Gmail refresh tokens unreadable. Keep all secrets in Railway variables, away from Git.
5. In your Google Cloud project, enable the Gmail API and create an external OAuth web client. Add `PUBLIC_URL/oauth/google/callback` as its exact authorized redirect URI. Follow [Google Cloud setup](google-cloud.md). Deploy the three services after setting their variables.
6. Create the only dashboard owner account with the dashboard container's `auth create-admin` command, then sign in at `PUBLIC_URL` and connect your Gmail account. Follow [Gmail add-on setup](gmail-addon.md) if you want the Gmail draft workflow.

The dashboard and Go service each migrate their schema at startup, using a shared PostgreSQL advisory lock. A restart keeps the database and attachment volume. Do not delete either volume when redeploying.

## Idle use and shutdown

Railway's **App Sleeping** setting can be enabled separately on `edge`, `api`, and `dashboard`. After an idle period, those services sleep and wake on incoming traffic. A pixel request can wake them, so tracking remains available while you are away. An open dashboard maintains an event stream and keeps the app active. PostgreSQL and its volume remain provisioned; app sleeping is a compute-saving convenience, not a full project power switch. A cold start may delay the first page or pixel request.

To stop tracking completely, remove or scale down the app deployments in Railway and redeploy them when needed. While stopped, mail-client image requests cannot be recorded. Keep database and volume backups before any shutdown or destructive change. See [operations](operations.md) for backups and restore.
