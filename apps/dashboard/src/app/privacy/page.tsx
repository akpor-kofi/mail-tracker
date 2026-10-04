import Link from 'next/link';

export default function PrivacyPage() {
  return (
    <main style={{ maxWidth: 680, margin: '4rem auto', padding: '0 1.5rem', lineHeight: 1.65 }}>
      <h1>Privacy policy</h1>
      <p>
        This Mail Tracker instance is operated by its owner. It has no shared relay or central
        telemetry. The source code is available on GitHub so each operator can host their own
        instance.
      </p>
      <h2>Google account access</h2>
      <p>
        When the owner connects Gmail, this instance receives permission to send email through that
        account. It stores an encrypted Google refresh token so the owner can send later. Read
        access is optional: the owner can separately grant permission to sync tracked Gmail threads,
        match replies and process delivery reports. Send-only accounts keep working without this
        grant. Disconnecting a mailbox removes its stored token; the owner can also revoke access in
        Google Account settings.
      </p>
      <h2>Data stored by this instance</h2>
      <p>
        The database stores the owner account, connected mailbox addresses, message subjects,
        recipient addresses, delivery states, Gmail message and thread IDs, local drafts, and
        timestamps of image and link requests, browser-family and automation classifications, goals
        and recorded outcomes. Hosted document viewers record sessions, visible pages, approximate
        foreground active time and download requests. Hosted files are private in the operator’s
        object-storage bucket and remain until deleted by the owner. A share link grants access to
        anyone who has it until expiry or revocation; it does not verify recipient identity.
        Uploaded native attachments are stored on the instance while needed for drafts or sending
        and are removed after confirmed sending or cleanup. The application does not store raw IP
        addresses, full user-agent strings, browser fingerprints, or geolocation. Its unguessable
        request token is stored only as a hash.
      </p>
      <h2>What an open event means</h2>
      <p>
        A tracking image request is not proof that a person read the message. Mail clients may
        block, proxy, prefetch, or cache images. A lack of requests is not proof that a message
        remains unread. In a shared send, an image request cannot identify which recipient caused
        it.
      </p>
      <p>
        This instance communicates with Google to authorize the connected mailbox, send composed
        messages, and, only when read access is granted, sync relevant threads. Synced message
        headers and snippets are retained; full incoming message bodies are not stored. An operator
        may add infrastructure that logs requests; consult that operator for its retention
        practices. Detailed events currently remain until their conversation or document is deleted;
        expired links stop access but do not automatically delete the original document.
      </p>
      <p>
        <Link href="/about">About Mail Tracker</Link>
      </p>
    </main>
  );
}
