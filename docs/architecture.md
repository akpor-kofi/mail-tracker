# Architecture

```
Browser ── Caddy/HTTPS ── Next.js dashboard + Better Auth
                   │
                   ├── Go Fiber API ── PostgreSQL
                   │      ├── encrypted Gmail refresh tokens
                   │      ├── local attachment volume
                   │      ├── Gmail send API
                   │      └── open-event stream
                   └── public /p/{token}.gif
Gmail add-on ── verified Google identity token ── Go API
```

`apps/dashboard` owns login sessions and renders the Next.js UI. Its Better Auth JWT plugin issues a short-lived token. The browser attaches it to Go requests. Go fetches Better Auth's public JWKS, verifies Ed25519 signature, issuer, audience, expiration and the configured owner email. The browser keeps server data in TanStack Query. It listens to a Fiber server-sent event stream and refetches after activity or reconnection.

`apps/api` is a Go 1.25 module. `internal/accounts`, `internal/mailboxes`, `internal/correspondence`, and `internal/tracking` hold the domains. Domain rules have no HTTP or PostgreSQL imports. Application packages own interfaces for repositories, Gmail sending, files, and event delivery. `adapters` provide PostgreSQL, Gmail, local files, and the in-memory stream broker. Generated Fiber request/response types live in separate `internal/httpapi/{mailboxes,correspondence,tracking}` packages. Handwritten handlers live in the parent `httpapi` package, with one file per domain. The pixel, health, OAuth callback, attachment upload, and stream routes are direct Fiber routes.

`packages/api-contract/openapi.yaml` is the single contract. Operation tags select each domain's generated package. Generate Go from the repository root with:

```sh
pnpm generate:go
```

Generate TypeScript with `pnpm generate` from the root. CI regenerates both and rejects drift.

## Send states

A tracked send first creates a conversation and pending delivery rows in one transaction, then returns a Sending result. A background worker pool sends at most four deliveries at once across the instance and publishes status changes over the event stream. The idempotency key prevents a second API call from issuing another Gmail send for the same attempt. Access tokens are cached per mailbox until shortly before expiry, with concurrent refreshes sharing one request. Each delivery gets a random 256-bit pixel token; the database stores only its SHA-256 hash. Gmail success records IDs and status `sent`. Definite Gmail API rejection records `failed`. Lost responses and timeouts record `unknown`. Any pending row from an interrupted process becomes `unknown` at startup. Unknown rows are never retried automatically.

Separate sends produce one MIME message and pixel per To recipient; the application rejects Cc/Bcc in that mode. Shared sends preserve To/Cc/Bcc in one MIME message and pixel. Go sanitizes HTML, appends the pixel afterward, creates a plain-text alternative, and attaches files. RFC Message-ID and Gmail IDs are stored to support outgoing replies and Gmail threading.

The add-on's code is in `apps/gmail-addon`; `scripts/configure.mjs` writes the instance domain into its manifest. A one-use pairing code from the dashboard is exchanged alongside a Google ID token. The server checks that the token's Google subject matches the connected mailbox. Every prepare call validates the token again. The add-on inserts a 1×1 image; it cannot observe a definitive Gmail send event.

## Database ownership

Better Auth maintains its own tables in the same PostgreSQL database and runs its migration on dashboard startup. Numbered SQL files in `apps/api/migrations` own the mail tracker tables and run on Go startup. Both startup migrators acquire the same PostgreSQL advisory lock, so multiple replicas and the two apps migrate one at a time. The Go migrator holds one database session across its version check and each transactional file; it closes that session to release the lock. Migration work has no fixed 30-second startup deadline, so a long backfill can finish before the API begins serving. Back up the database, attachment volume, and secrets together.
