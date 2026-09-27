'use client';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Shell } from '@/components/shell';
import { client, unwrap } from '@/lib/api';
export default function Settings() {
  const qc = useQueryClient();
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const mailboxes = useQuery({
    queryKey: ['mailboxes'],
    queryFn: async () => {
      const { api } = await client();
      return unwrap(await api.GET('/mailboxes'));
    },
  });
  async function connect() {
    try {
      setError('');
      const { api } = await client();
      const data = unwrap(await api.POST('/mailboxes/connect'));
      location.href = data.url;
    } catch (e) {
      setError(String(e));
    }
  }
  async function pair(mailboxId: string) {
    try {
      setError('');
      const { api } = await client();
      const data = unwrap(await api.POST('/pairing-codes', { body: { mailboxId } }));
      setCode(data.code);
    } catch (e) {
      setError(String(e));
    }
  }
  async function remove(mailboxId: string) {
    if (!confirm('Remove this Gmail connection and its tracked records?')) return;
    try {
      setError('');
      const { api } = await client();
      const result = await api.DELETE('/mailboxes/{mailboxId}', {
        params: { path: { mailboxId } },
      });
      if (!(result as { response: Response }).response.ok)
        throw new Error('Could not remove mailbox');
      qc.invalidateQueries({ queryKey: ['mailboxes'] });
      qc.invalidateQueries({ queryKey: ['conversations'] });
    } catch (e) {
      setError(String(e));
    }
  }
  return (
    <Shell>
      <div className="page-header">
        <div>
          <h1>Mailboxes</h1>
          <p>Each Gmail account needs its own Google authorization.</p>
        </div>
        <button onClick={connect}>Connect Gmail</button>
      </div>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {code && (
        <div className="inline-notice">
          <strong>One-use pairing code:</strong> <code>{code}</code>
          <span>Enter it in the Gmail add-on within 10 minutes.</span>
        </div>
      )}
      {mailboxes.isPending ? (
        <div className="list-state">Loading mailboxes…</div>
      ) : mailboxes.isError ? (
        <div className="list-state error">Could not load mailboxes.</div>
      ) : mailboxes.data?.length === 0 ? (
        <div className="list-state">No Gmail accounts connected.</div>
      ) : (
        <div className="detail-list">
          {mailboxes.data?.map((m) => (
            <div className="detail-row" key={m.id}>
              <div>
                <strong>{m.email}</strong>
                <p>Connected {new Date(m.connectedAt).toLocaleDateString()}</p>
              </div>
              <div className="actions">
                <button className="secondary" onClick={() => pair(m.id)}>
                  Pair add-on
                </button>
                <button className="danger" onClick={() => remove(m.id)}>
                  Remove
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
      <section className="settings-help">
        <h2>Gmail add-on</h2>
        <p>
          Install the self-deployed Apps Script add-on, then pair each connected Gmail account using
          a code above. The add-on can prepare a Gmail draft, but cannot confirm when Gmail sends
          it.
        </p>
      </section>
    </Shell>
  );
}
