'use client';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Shell } from '@/components/shell';
import { ownerRequest } from '@/components/analytics';
import { client, unwrap } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog';
export default function Settings() {
  const qc = useQueryClient();
  const health = useQuery({
    queryKey: ['mailboxes-health'],
    queryFn: () =>
      ownerRequest<
        {
          id: string;
          email: string;
          syncEnabled: boolean;
          syncStatus: string;
          lastSyncAt: string | null;
          addonPaired: boolean;
          error: string;
        }[]
      >('/mailboxes/health'),
    refetchInterval: 30000,
  });
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
        <Button onClick={connect}>Connect Gmail</Button>
      </div>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {code && (
        <Alert className="pairing-notice" role="status">
          <AlertTitle>One-use pairing code</AlertTitle>
          <AlertDescription>
            <code>{code}</code> Enter it in the Gmail add-on within 10 minutes.
          </AlertDescription>
        </Alert>
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
                <Button variant="outline" onClick={() => pair(m.id)}>
                  Pair add-on
                </Button>
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="destructive">Remove</Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Remove this Gmail connection?</AlertDialogTitle>
                      <AlertDialogDescription>
                        This deletes its tracked messages and stored connection from this instance.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction variant="destructive" onClick={() => remove(m.id)}>
                        Remove connection
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
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
      <section>
        <h2>Connection and reply synchronization</h2>
        <p>
          Read synchronization is optional. Enabling it requests Gmail read access to match replies
          and delivery reports. Send-only connections keep working without it. Google verification
          requirements apply to broader public use.
        </p>
        {health.isPending ? (
          <p>Loading connection health…</p>
        ) : health.isError ? (
          <p role="alert">{health.error.message}</p>
        ) : (
          health.data?.map((m) => (
            <div className="detail-row" key={m.id}>
              <div>
                <strong>{m.email}</strong>
                <p>
                  Add-on {m.addonPaired ? 'paired' : 'not paired'} ·{' '}
                  {m.syncEnabled ? `Read sync: ${m.syncStatus}` : 'Send-only connection'}
                </p>
                <p>
                  {m.lastSyncAt
                    ? `Last synced ${new Date(m.lastSyncAt).toLocaleString()}`
                    : 'No successful read sync yet'}
                </p>
                {m.error && m.syncEnabled && <p role="status">{m.error}</p>}
              </div>
              {m.syncEnabled ? (
                <Button
                  variant="outline"
                  onClick={async () => {
                    try {
                      await ownerRequest(`/mailboxes/${m.id}/pause-sync`, { method: 'POST' });
                      await health.refetch();
                    } catch (e) {
                      setError(String(e));
                    }
                  }}
                >
                  Pause sync
                </Button>
              ) : (
                <Button
                  variant="outline"
                  onClick={async () => {
                    try {
                      const v = await ownerRequest<{ url: string }>('/mailboxes/connect-read', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ mailboxId: m.id }),
                      });
                      location.href = v.url;
                    } catch (e) {
                      setError(String(e));
                    }
                  }}
                >
                  Reconnect with read access
                </Button>
              )}
            </div>
          ))
        )}
      </section>
    </Shell>
  );
}
