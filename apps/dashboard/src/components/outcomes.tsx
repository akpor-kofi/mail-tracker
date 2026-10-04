'use client';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ownerRequest } from '@/components/analytics';
import { Button } from '@/components/ui/button';
import Link from 'next/link';
import type { components } from '@mail-tracker/api-client';
export type Goal = components['schemas']['Goal'];
type Outcome = components['schemas']['Outcome'];
export function ConversationOutcomes({
  id,
  deliveries,
}: {
  id: string;
  deliveries: { id: string; recipients: string[] }[];
}) {
  const [goal, setGoal] = useState('');
  const [delivery, setDelivery] = useState('');
  const [due, setDue] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const goals = useQuery({ queryKey: ['goals'], queryFn: () => ownerRequest<Goal[]>('/goals') });
  const outcomes = useQuery({
    queryKey: ['outcomes', id],
    queryFn: () => ownerRequest<Outcome[]>(`/conversations/${id}/outcomes`),
    refetchInterval: 15000,
  });
  const messages = useQuery({
    queryKey: ['messages', id],
    queryFn: () =>
      ownerRequest<
        { id: string; sender: string; snippet: string; direction: string; at: string }[]
      >(`/conversations/${id}/messages`),
    refetchInterval: 30000,
  });
  async function act(work: () => Promise<unknown>, success: string) {
    setBusy(true);
    setError('');
    try {
      await work();
      setNotice(success);
      await outcomes.refetch();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <section>
        <h2>Outcomes</h2>
        <p>
          Manual outcomes are recorded by you. Webhook outcomes come from a signed integration.
          Sent-based rates exclude prepared and unknown sends.
        </p>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        {notice && <p role="status">{notice}</p>}
        <label>
          Goal{' '}
          <select value={goal} onChange={(e) => setGoal(e.target.value)}>
            <option value="">Select goal</option>
            {goals.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          {' '}
          Delivery{' '}
          <select value={delivery} onChange={(e) => setDelivery(e.target.value)}>
            <option value="">Select delivery</option>
            {deliveries.map((d) => (
              <option key={d.id} value={d.id}>
                {d.recipients.join(', ')}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={busy}
          onClick={() => {
            if (!goal || !delivery) {
              setError('Select a goal and delivery first.');
              return;
            }
            void act(
              () =>
                ownerRequest(`/conversations/${id}/outcomes`, {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({
                    goalId: goal,
                    deliveryId: delivery,
                    occurredAt: new Date().toISOString(),
                  }),
                }),
              'Manual outcome recorded.',
            );
          }}
        >
          Record outcome
        </Button>
        <p>
          <Link href="/reports">Create goals and view conversion reports</Link>
        </p>
        {goals.isError && (
          <p role="alert">
            Could not load goals. <Button onClick={() => goals.refetch()}>Retry</Button>
          </p>
        )}
        {outcomes.isPending ? (
          <p>Loading outcomes…</p>
        ) : outcomes.isError ? (
          <p role="alert">{outcomes.error.message}</p>
        ) : outcomes.data?.length ? (
          outcomes.data.map((o) => (
            <div className="detail-row" key={o.id}>
              <span>
                {goals.data?.find((g) => g.id === o.goalId)?.name || 'Goal'} ·{' '}
                {o.source === 'manual' ? 'Manually recorded' : 'Signed webhook'} ·{' '}
                {new Date(o.occurredAt).toLocaleString()} {o.reversedAt ? '· Reversed' : ''}
              </span>
              {!o.reversedAt && (
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() =>
                    act(
                      () => ownerRequest(`/outcomes/${o.id}/reverse`, { method: 'POST' }),
                      'Outcome reversed; rates will update.',
                    )
                  }
                >
                  Reverse outcome
                </Button>
              )}
            </div>
          ))
        ) : (
          <p>No outcomes recorded.</p>
        )}
      </section>
      <section>
        <h2>Follow-up reminder</h2>
        <p>
          Dashboard reminder only; no email is sent. A detected reply or active outcome suppresses
          the reminder.
        </p>
        <label>
          When <input type="datetime-local" value={due} onChange={(e) => setDue(e.target.value)} />
        </label>
        <Button
          disabled={busy}
          onClick={() => {
            const at = new Date(due);
            if (!due || Number.isNaN(at.getTime())) {
              setError('Choose a reminder time.');
              return;
            }
            void act(
              () =>
                ownerRequest(`/conversations/${id}/reminders`, {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ dueAt: at.toISOString() }),
                }),
              'Reminder created. View it in Reports.',
            );
          }}
        >
          Set reminder
        </Button>
      </section>
      <section>
        <h2>Synced Gmail messages</h2>
        <p>
          Enable optional read synchronization in Settings to detect replies and delivery reports.
          Matching uses Gmail thread IDs and reply headers; unrelated inbox messages are not stored
          here.
        </p>
        {messages.isPending ? (
          <p>Loading messages…</p>
        ) : messages.isError ? (
          <p role="alert">{messages.error.message}</p>
        ) : messages.data?.length ? (
          <ol className="timeline">
            {messages.data.map((m) => (
              <li key={m.id}>
                <time dateTime={m.at}>{new Date(m.at).toLocaleString()}</time>
                <span>
                  {m.direction} · {m.sender}
                  <br />
                  {m.snippet}
                </span>
              </li>
            ))}
          </ol>
        ) : (
          <p>
            No matched messages synced. The add-on's prepared drafts cannot be reconciled by subject
            alone.
          </p>
        )}
      </section>
    </>
  );
}
