'use client';
import { useState } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ownerRequest } from '@/components/analytics';
import { client } from '@/lib/api';
import { Goal } from '@/components/outcomes';
import { Shell } from '@/components/shell';
import { Button } from '@/components/ui/button';
type Report = {
  sent: number;
  imageActive: number;
  linked: number;
  clicked: number;
  replied: number;
  bounceReported: number;
  converted: number;
  prepared: number;
  unknown: number;
  from: string;
  to: string;
  policy: string;
};
function rate(n: number, d: number) {
  return d ? `${((100 * n) / d).toFixed(1)}% (${n}/${d})` : 'Not enough data';
}
export default function Reports() {
  const [goal, setGoal] = useState('');
  const [mailbox, setMailbox] = useState('');
  const [from, setFrom] = useState(() =>
    new Date(Date.now() - 30 * 86400000).toISOString().slice(0, 10),
  );
  const [to, setTo] = useState(() => new Date().toISOString().slice(0, 10));
  const [name, setName] = useState('');
  const [days, setDays] = useState(30);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const goals = useQuery({ queryKey: ['goals'], queryFn: () => ownerRequest<Goal[]>('/goals') });
  const mailboxes = useQuery({
    queryKey: ['mailboxes-health'],
    queryFn: () => ownerRequest<{ id: string; email: string }[]>('/mailboxes/health'),
  });
  const reminders = useQuery({
    queryKey: ['reminders'],
    queryFn: () =>
      ownerRequest<{ id: string; conversationId: string; subject: string; dueAt: string }[]>(
        '/reminders',
      ),
    refetchInterval: 30000,
  });
  let query = '';
  try {
    query = new URLSearchParams({
      goalId: goal,
      mailboxId: mailbox,
      from: new Date(from + 'T00:00:00').toISOString(),
      to: new Date(to + 'T23:59:59.999').toISOString(),
    }).toString();
  } catch {}
  const report = useQuery({
    queryKey: ['reports', query],
    enabled: !!query,
    queryFn: () => ownerRequest<Report>('/reports?' + query),
    refetchInterval: 30000,
  });
  return (
    <Shell>
      <div className="page-header">
        <div>
          <h1>Reports and goals</h1>
          <p>
            Rates use confirmed Gmail sends and delivery-level attribution. Recipient folder
            placement is unknown.
          </p>
        </div>
      </div>
      <div className="toolbar">
        <label>
          From <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label>
          Through <input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
        <label>
          Mailbox{' '}
          <select value={mailbox} onChange={(e) => setMailbox(e.target.value)}>
            <option value="">All mailboxes</option>
            {mailboxes.data?.map((m) => (
              <option key={m.id} value={m.id}>
                {m.email}
              </option>
            ))}
          </select>
        </label>
        <label>
          Goal{' '}
          <select value={goal} onChange={(e) => setGoal(e.target.value)}>
            <option value="">Any goal</option>
            {goals.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={busy || !query}
          variant="outline"
          onClick={async () => {
            setBusy(true);
            setError('');
            try {
              const { token } = await client();
              const r = await fetch('/api/v1/reports/export?' + query, {
                headers: { Authorization: `Bearer ${token}` },
              });
              if (!r.ok) throw new Error('Could not export report');
              const url = URL.createObjectURL(await r.blob());
              const a = document.createElement('a');
              a.href = url;
              a.download = 'mail-tracker-report.csv';
              a.click();
              URL.revokeObjectURL(url);
            } catch (e) {
              setError(String(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          Export CSV
        </Button>
      </div>
      <p className="muted">
        Dates use your browser timezone; the report stores UTC boundaries. Events are counted
        through the end of this range. Historical sends without confirmed timestamps are excluded.
      </p>
      {!query ? (
        <p role="alert">Choose valid dates.</p>
      ) : report.isPending ? (
        <p role="status">Loading report…</p>
      ) : report.isError ? (
        <p role="alert">
          {report.error.message} <Button onClick={() => report.refetch()}>Retry</Button>
        </p>
      ) : (
        report.data && (
          <>
            <div className="metric-grid">
              {[
                ['Confirmed sends', report.data.sent],
                ['Open detection rate', rate(report.data.imageActive, report.data.sent)],
                ['Tracked link click rate', rate(report.data.clicked, report.data.linked)],
                ['Reply rate', rate(report.data.replied, report.data.sent)],
                ['Conversion rate', rate(report.data.converted, report.data.sent)],
                ['Reported bounces', report.data.bounceReported],
              ].map(([label, value]) => (
                <div key={label} className="metric-card">
                  <span>{label}</span>
                  <strong>{value}</strong>
                </div>
              ))}
            </div>
            <p>
              {report.data.prepared} prepared and {report.data.unknown} unknown deliveries excluded.
              Shared sends count as one group delivery.
            </p>
            <p className="muted">{report.data.policy}</p>
          </>
        )
      )}
      <h2>Create a goal</h2>
      <label>
        Goal name{' '}
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Meeting booked"
          maxLength={100}
        />
      </label>
      <label>
        {' '}
        Attribution window (days){' '}
        <input
          type="number"
          min={1}
          max={365}
          value={days}
          onChange={(e) => setDays(Number(e.target.value))}
        />
      </label>
      <Button
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          setError('');
          try {
            await ownerRequest('/goals', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ name, windowDays: days }),
            });
            setName('');
            setNotice(
              'Goal created. Record outcomes from a conversation or connect a signed webhook.',
            );
            await goals.refetch();
          } catch (e) {
            setError(String(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        Create goal
      </Button>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      <h2>Follow-up reminders</h2>
      {reminders.isPending ? (
        <p>Loading reminders…</p>
      ) : reminders.isError ? (
        <p role="alert">{reminders.error.message}</p>
      ) : reminders.data?.length ? (
        reminders.data.map((r) => (
          <div className="detail-row" key={r.id}>
            <Link href={`/conversations/${r.conversationId}`}>{r.subject}</Link>
            <span>
              {new Date(r.dueAt).getTime() < Date.now() ? 'Due now' : 'Scheduled'} ·{' '}
              {new Date(r.dueAt).toLocaleString()}
            </span>
            <Button
              disabled={busy}
              variant="outline"
              onClick={async () => {
                setBusy(true);
                setError('');
                try {
                  await ownerRequest(`/reminders/${r.id}/done`, { method: 'POST' });
                  await reminders.refetch();
                } catch (e) {
                  setError(String(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              Mark done
            </Button>
          </div>
        ))
      ) : (
        <p>
          No active reminders. Set one on a conversation; detected replies or outcomes suppress
          reminders.
        </p>
      )}
    </Shell>
  );
}
