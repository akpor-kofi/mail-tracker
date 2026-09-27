# Operations and troubleshooting

## Backup and restore

Back up PostgreSQL, the `attachments` Docker volume, and `infra/.env` together. The instance secret is required to decrypt stored Gmail refresh tokens. For a database dump:

```sh
docker compose --env-file infra/.env -f infra/compose.yaml exec -T postgres pg_dump -U mailtracker -d mailtracker > mailtracker.sql
```

Restore into a fresh empty instance with the same `infra/.env` and attachment volume. Start PostgreSQL, restore with `psql -U mailtracker -d mailtracker < mailtracker.sql`, then start the other services. Test login, one connected mailbox, and an existing conversation afterward. Keep backup files out of the Git repository.

## Owner password recovery

The CLI reset uses Better Auth's password hasher, keeps the same user ID, and revokes sessions. Run from the server's shell, supplying the new password over stdin without writing it to shell history:

```sh
read -s 'NEW_PASSWORD?New password: '; echo
printf '%s' "$NEW_PASSWORD" | docker compose --env-file infra/.env -f infra/compose.yaml exec -T dashboard sh -lc 'cd apps/dashboard && pnpm reset-owner'
unset NEW_PASSWORD
```

This is a `zsh` example. Use your shell's equivalent hidden-input command if needed.

## Manual acceptance checks

Use two Gmail accounts and a recipient account you control:

1. Deploy to public HTTPS, create owner, connect both Gmail mailboxes, restart the API, and confirm both remain connected.
2. Send a single-recipient message from the dashboard, with an attachment. Confirm Gmail Sent contains the message and the dashboard shows Sent. Open it in a client with images enabled and confirm an event; repeat with images blocked and expect No open detected.
3. Send to two recipients in Separate sends and verify two Gmail messages and independent delivery rows. Send a Shared message with Cc/Bcc and verify one message and aggregate tracking.
4. Save and reopen a local draft. Send an outgoing reply from an existing sent conversation and confirm the Gmail thread, subject, and reply headers.
5. Install the add-on, pair the matching account, insert the pixel into a Gmail draft, send it, and inspect the delivered message's HTML in the recipient account. Confirm the image survives and is visually invisible. This check requires real accounts and cannot be established by a local build.
6. Restart the stack and confirm saved records remain. Restore a backup to a separate instance and repeat login and mailbox checks.

## Troubleshooting

- **No pixel event:** Check that `/p/{token}.gif` is publicly reachable over HTTPS, the recipient client loads remote images, and no proxy strips the image. Do not interpret absence as unread.
- **Connection expires:** Check OAuth consent screen publishing status. Testing-mode refresh tokens can expire after seven days. Reconnect the mailbox if needed.
- **Add-on pairing rejected:** Pair while signed into the Gmail account that matches the connected mailbox. Verify the Apps Script ID token audience in `GOOGLE_ADDON_CLIENT_ID` and the generated URL allowlist.
- **Send status unknown:** The API cannot know whether Gmail accepted a timed-out request. Inspect that account's Gmail Sent folder before any manual retry. The service never retries unknown outcomes automatically.
- **Caddy certificate pending:** Check DNS points to the host and TCP ports 80/443 are open. `docker compose ... logs caddy` gives the certificate error.
