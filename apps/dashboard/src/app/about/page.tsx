import Link from 'next/link';

export default function AboutPage() {
  return (
    <main style={{ maxWidth: 680, margin: '4rem auto', padding: '0 1.5rem', lineHeight: 1.65 }}>
      <h1>Mail Tracker</h1>
      <p>
        Mail Tracker is an open source, self-hosted tool for sending Gmail messages and
        detecting when a recipient&apos;s mail client requests a tracking image.
      </p>
      <p>
        The owner connects their Gmail account with Google OAuth. The app requests permission
        to send email on that account&apos;s behalf; it does not request access to read the inbox.
        The owner can also install a Gmail add-on to prepare a draft for tracking.
      </p>
      <p>
        Image requests are an imperfect signal. The dashboard uses “Open detected” and
        “No open detected” because images may be blocked, cached, or fetched by a proxy.
      </p>
      <p>
        <Link href="/privacy">Privacy policy</Link> ·{' '}
        <a href="https://github.com/akpor-kofi/mail-tracker">Source code</a> ·{' '}
        <Link href="/sign-in">Owner sign in</Link>
      </p>
    </main>
  );
}
