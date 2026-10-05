# Analytics, documents and outcomes

The [implementation roadmap](plans/analytics-implementation.html) describes the full phased product. This release implements conversation activity, dashboard and manually inserted add-on links, private PDF/image sharing, goals/manual outcomes/signed webhooks, optional Gmail polling, cohort reports and manual reminders.

## Railway document storage

The API uses `github.com/akpor-kofi/raildrop/sdk/go v0.1.0` and its `s3store.NewRailwayBucketStorage` adapter. Files are immutable keys under `private/documents/`; the browser never receives S3 credentials or a bucket URL. The Go API authorizes every byte-range request against an unexpired, unrevoked share and cookie-bound viewer session. The Next.js dashboard hosts PDF.js and its worker/fonts/CMaps locally.

In Railway, create a private `documents` bucket and put these references on the API service:

```
RAILDROP_BUCKET=${{documents.BUCKET}}
RAILDROP_ENDPOINT=${{documents.ENDPOINT}}
RAILDROP_REGION=${{documents.REGION}}
RAILDROP_ACCESS_KEY_ID=${{documents.ACCESS_KEY_ID}}
RAILDROP_SECRET_ACCESS_KEY=${{documents.SECRET_ACCESS_KEY}}
RAILDROP_FORCE_PATH_STYLE=false
```

Keep credentials out of source/client variables. API startup probes the private prefix and logs `private document bucket ready`. Unsupported/empty files and uploads over 20 MiB are rejected. Images are limited to 40 million pixels. PDF content is isolated through PDF.js canvas rendering, with evaluation disabled and a 200-page viewer limit; header recognition is not a malware scan. Do not open untrusted originals outside the viewer without your usual safeguards.

Compose shares expire after 30 days. Standalone library shares can expire in 1–365 days and offer/hide the download button. The URL is returned once; stored hashes cannot recover it. Delete revokes access and queues object removal; hourly cleanup also removes abandoned uploads. Already downloaded bytes and existing on-screen content cannot be recalled.

## Events and rates

New image/link requests preserve raw observations up to the per-delivery cap. Classifier v1 uses proxy/scanner user-agent hints and records parsed browser family; raw IPs/full user agents are not persisted. The old deduplicated image endpoint remains compatible, and historical events are labeled legacy. Estimated sessions use five-minute gaps per delivery and kind, excluding known image proxies/suspected automation. They do not count verified people or exact reads.

Reports use confirmed-send timestamps in `[from,to)`, unique deliveries, and events observed by the report end. Image-active and reply rates use sent deliveries; click rate uses linked sent deliveries; conversion rate uses sent deliveries and each goal's send-based attribution window. Prepared/unknown sends are separately excluded. Historical sends lacking confirmed timestamps are not backfilled into cohorts. Shared recipients are aggregate. Empty denominators show unavailable rates. CSV exports contain fixed labels, counts and ISO timestamps; no user content becomes spreadsheet formulas.

An event transaction also queues a durable dashboard invalidation. A crash before acknowledgement can repeat an invalidation; it cannot discard the committed queue item. Run one API replica with the current in-memory SSE broker. Detail polling reconciles every 15 seconds. PostgreSQL pub/sub is needed before multiple-replica live fan-out. Sending itself is never blindly retried after an uncertain provider result.

## Conversion webhook integration

Set a dedicated random `CONVERSION_WEBHOOK_SECRET` on the API service. Your booking/form/purchase backend must preserve the optional `mt_ref` from an attribution-enabled tracked destination in its own first-party session, then submit the reference after a real outcome. Cross-domain purchases cannot be inferred from a redirect.

`POST /api/v1/webhooks/conversions` takes:

```json
{
  "goalId": "goal-uuid",
  "externalId": "booking-provider-event-123",
  "occurredAt": "2026-10-04T12:00:00Z",
  "attributionRef": "opaque-mt_ref-value",
  "value": "120.00",
  "currency": "USD"
}
```

`externalId` is required and deduplicated per owner/source; reusing it with different data fails. Optional money uses decimal strings and uppercase three-letter currency; omit both for non-monetary goals. The goal determines the owner. A webhook cannot use `deliveryId` to force attribution. It needs an eligible prior click on the matching reference within the goal window; otherwise the real event is retained unattributed. A reference attributes a delivery, not a proven individual. Manual marks and reversals are available from conversation detail.

Sign the exact UTF-8 JSON bytes, without reserializing after signing:

```js
import { createHmac } from 'node:crypto';
const body = JSON.stringify(event);
const timestamp = String(Math.floor(Date.now() / 1000));
const signature = createHmac('sha256', process.env.CONVERSION_WEBHOOK_SECRET)
  .update(timestamp + '.' + body)
  .digest('hex');
await fetch(process.env.MAIL_TRACKER_URL + '/api/v1/webhooks/conversions', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'X-Webhook-Timestamp': timestamp,
    'X-Webhook-Signature': signature,
  },
  body,
});
```

The API enforces a five-minute timestamp tolerance, constant-time signature verification, a 64 KiB payload limit and rate limits. A provider integration must supply its real outcome and server secret; no third-party provider has been connected automatically.

## Gmail sync and reminders

Existing accounts stay send-only. Settings shows pairing/sync health and allows a separate read-access reconnect for the same Google account. Add `gmail.readonly` to your OAuth consent configuration/test account access as necessary. Google authorization must grant it before sync starts. Restricted-scope verification requirements depend on your deployment and Google application setup.

If Gmail rejects a dashboard send with `ACCESS_TOKEN_SCOPE_INSUFFICIENT`, use **Settings → Reconnect Gmail** for that mailbox and grant sending permission. This reconnect preserves the account ID, tracked messages, add-on pairing, and the current read-sync choice. Google grants are validated before replacing stored credentials; incomplete grants leave the previous connection intact. Legacy connections with no recorded scopes show an unverified permission state. Scope failures are definite failed sends, retain the draft, and are never automatically resent. Other Gmail 403 errors (for example quota failures) remain separate.

A minute worker polls history, handles expired history with a bounded 30-day/1,000-message recovery, and matches only tracked Gmail threads. Replies require recipient sender/reference matches and exclude own/obvious automatic mail. DSN bounces require failed delivery status and matching recipients/references. It cannot establish recipient spam placement. Missing evidence stays unknown. Sync errors become reconnect/health states. Manual reminders appear in Reports and are suppressed after a recorded reply or unreversed outcome; they do not send email or push notifications.

## Deployment and follow-up work

Apply migrations 005–007 under the existing shared migration lock. Deploy API, dashboard and edge routing for `/c/*` together; keep `/p/*` and previous tokens. Back up database, bucket and instance secret through the existing operational workflow. Added tables are additive; rollback must preserve new tables and previously shared document/link routes.

Tests cover database-backed ownership, event deduplication, crash-safe invalidation, source classification, private upload/share/session revocation and deletion, cookie/origin/byte-range authorization, webhook signature/replay handling, eligible attribution/window checks, report reversals and optional OAuth scope isolation. Builds copy PDF.js assets into the standalone dashboard image.

Remaining roadmap work includes campaign/recipient reporting dimensions, permanent retention rollups/pruning, automatic document expiry deletion, recipient-verified shares, measured load/SLO and backup restore drills, multi-replica fan-out, Gmail push/watch, and provider-specific conversion connectors. Fingerprinting, spam seed tests, sequences and CRM/team features remain separately scoped. There is no automatic recipient fingerprint or spam verdict in this release.
