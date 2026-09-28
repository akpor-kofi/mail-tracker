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
        When the owner connects Gmail, this instance receives permission to send email through
        that account. It stores an encrypted Google refresh token so the owner can send later.
        It does not request permission to read incoming mail or search the inbox. Disconnecting
        a mailbox removes its stored token; the owner can also revoke access in Google Account
        settings.
      </p>
      <h2>Data stored by this instance</h2>
      <p>
        The database stores the owner account, connected mailbox addresses, message subjects,
        recipient addresses, delivery states, Gmail message and thread IDs, local drafts, and
        timestamps of tracking image requests. Uploaded attachments are stored on the instance
        while needed for drafts or sending and are removed after confirmed sending or cleanup.
        The public pixel endpoint does not intentionally store IP addresses, user-agent strings,
        or geolocation. Its unguessable request token is stored only as a hash.
      </p>
      <h2>What an open event means</h2>
      <p>
        A tracking image request is not proof that a person read the message. Mail clients may
        block, proxy, prefetch, or cache images. A lack of requests is not proof that a message
        remains unread. In a shared send, an image request cannot identify which recipient
        caused it.
      </p>
      <p>
        This instance sends data to Google only to authorize the connected mailbox and send
        messages the owner composes. An operator may add infrastructure that logs requests;
        consult that operator for its retention practices.
      </p>
      <p><Link href="/about">About Mail Tracker</Link></p>
    </main>
  );
}
