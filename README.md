# Mail Tracker

Self-hosted Gmail open detection for one owner with multiple connected Gmail accounts. Licensed under [MIT](LICENSE). The repository contains a Next.js dashboard, a Go API, a self-deployed Gmail add-on, deployment files, and one shared OpenAPI contract.

**What it reports:** “No open detected” and “Open detected.” A transparent image request is not proof that a person read a message. Image blocking can hide a real open; mail privacy services can request an image without a human opening the message.

## Two ways to track

| Path | What happens | Send confirmation | Recipient tracking |
| --- | --- | --- | --- |
| Gmail add-on | Inserts a 1×1 transparent image at the end of the active Gmail draft | Remains **Prepared** until an image request; Gmail send cannot be confirmed | Aggregate for that draft |
| Track and Send | Composer queues a send through the chosen Gmail account using Gmail API | Starts **Sending**, then shows per-delivery **Sent**, **Failed**, or **Send status unknown** | One pixel per To recipient with **Separate sends**; aggregate with **Shared send** |

The app does not read incoming mail or detect incoming replies. Local composer drafts live on your instance, not Gmail Drafts. Outgoing replies can be prepared in Gmail or composed from a sent conversation in the dashboard.

## Requirements

- A server reachable at a public HTTPS domain, with DNS pointing to it and ports 80/443 open.
- Docker with Compose for deployment.
- Your own Google Cloud project and OAuth credentials. Clerk or another login provider does not replace the Gmail authorization flow.
- Node.js 22, pnpm 11, and Go 1.25 for local development.

## Deploy

1. Clone this repository with `git clone https://github.com/akpor-kofi/mail-tracker.git && cd mail-tracker`. Copy [`infra/.env.example`](infra/.env.example) to `infra/.env`, set `TRACKER_DOMAIN`, `PUBLIC_URL`, and `OWNER_EMAIL`, then fill the secrets. Generate values with `openssl rand -hex 32` for `POSTGRES_PASSWORD` and `BETTER_AUTH_SECRET`, and `openssl rand -base64 32` for `INSTANCE_SECRET`. Back up these exact values; losing the instance secret makes stored Gmail refresh tokens unreadable.
2. Follow [Google Cloud setup](docs/google-cloud.md) to fill `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `GOOGLE_ADDON_CLIENT_ID`.
3. Start the stack:

   ```sh
   docker compose --env-file infra/.env -f infra/compose.yaml up -d --build
   ```

4. Create the only owner account on the server. This command prompts for its password:

   ```sh
   docker compose --env-file infra/.env -f infra/compose.yaml exec dashboard sh -lc 'cd apps/dashboard && pnpm exec auth create-admin --email "$OWNER_EMAIL" --name Owner --role admin'
   ```

5. Open `PUBLIC_URL`, sign in, and connect each Gmail account in **Settings**. Follow [Gmail add-on setup](docs/gmail-addon.md) for draft mode. Verify both paths with a message to an account you control before relying on them.

The dashboard, Better Auth, API, pixel URL, and OAuth callback share one domain through Caddy. Caddy obtains HTTPS certificates when DNS and ports are ready. The API does not serve the dashboard directly.

## Development

```sh
pnpm install
pnpm generate
pnpm typecheck
pnpm build
cd apps/api && GOTOOLCHAIN=go1.25.1 go test ./...
```

The API requires the environment variables in `infra/.env.example`; point `DATABASE_URL` at a local PostgreSQL instance. Better Auth schema migrations run on dashboard container startup. The Go service applies its own numbered SQL migrations on startup. The source OpenAPI file is [`packages/api-contract/openapi.yaml`](packages/api-contract/openapi.yaml). Run `pnpm generate` and the pinned `oapi-codegen` command in [architecture](docs/architecture.md) after changing it.

The dashboard uses shadcn/ui components and a small theme in `apps/dashboard/src/app/styles.css`. Run `pnpm dlx shadcn@latest add COMPONENT` from `apps/dashboard` when adding another shared control; its configuration is in `apps/dashboard/components.json`.

## Operations

- [Architecture and ports](docs/architecture.md)
- [Google Cloud setup](docs/google-cloud.md)
- [Gmail add-on setup](docs/gmail-addon.md)
- [Backup, restore, recovery, and troubleshooting](docs/operations.md)
- [Privacy and tracking limits](docs/privacy.md)

The source code is open under MIT: anyone can clone, inspect, modify, and run their own instance. Each owner brings their own domain, database, Google OAuth project, and Apps Script deployment. There is no central relay. A shared Google Workspace Marketplace add-on is not part of this release because the add-on must allowlist each self-hoster's domain.

## Verification status

The automated build and local tests run without Google credentials. To run the container smoke test after creating a disposable owner, set `TEST_OWNER_EMAIL` and `TEST_OWNER_PASSWORD` and execute `node tests/smoke.mjs` inside the dashboard container. The Gmail add-on's image insertion and mail-client behavior require a real account and recipient clients; follow the manual checks in [operations](docs/operations.md). Do not assume an image survived sending until that check succeeds on your own Gmail setup.
