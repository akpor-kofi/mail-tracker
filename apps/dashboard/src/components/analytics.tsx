'use client';
import { useState } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { client } from '@/lib/api';
import { Button } from '@/components/ui/button';
import type { components } from '@mail-tracker/api-client';
type Summary = components['schemas']['ActivitySummary'];
type Events = components['schemas']['ActivityPage'];
export async function ownerRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const { token } = await client();
  const response = await fetch('/api/v1' + path, {
    ...options,
    headers: { ...options.headers, Authorization: `Bearer ${token}` },
  });
  if (!response.ok) {
    const e = await response.json().catch(() => ({ error: 'Request failed' }));
    throw new Error(e.error || 'Request failed');
  }
  return response.json();
}
const date = (s: string | null) => (s ? new Date(s).toLocaleString() : 'No activity detected');
export function ConversationAnalytics({ id }: { id: string }) {
  const [kind, setKind] = useState('');
  const summary = useQuery({
    queryKey: ['analytics', id],
    queryFn: () => ownerRequest<Summary>(`/conversations/${id}/analytics`),
    refetchInterval: 15000,
  });
  const events = useInfiniteQuery({
    queryKey: ['activity', id, kind],
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      ownerRequest<Events>(
        `/conversations/${id}/events?cursor=${encodeURIComponent(pageParam)}&kind=${encodeURIComponent(kind)}`,
      ),
    getNextPageParam: (p) => p.nextCursor || undefined,
    refetchInterval: 15000,
  });
  return (
    <section aria-label="Conversation analytics">
      <h2>Activity overview</h2>
      <p>
        Image and link activity indicate requests, not confirmed reading or recipient identity.
        Delivery placement remains unknown unless a delivery report is received.
      </p>
      {summary.isPending ? (
        <p role="status">Loading analytics…</p>
      ) : summary.isError ? (
        <p role="alert">
          {summary.error.message} <Button onClick={() => summary.refetch()}>Retry</Button>
        </p>
      ) : (
        summary.data && (
          <>
            <div className="metric-grid">
              {[
                ['Estimated open sessions', summary.data.images.sessions],
                ['Image requests', summary.data.images.raw],
                ['Link requests', summary.data.clicks.raw],
                ['Estimated click sessions', summary.data.clicks.sessions],
                ['Document sessions', summary.data.documentSessions],
                ['Active viewer time', `${Math.round(summary.data.activeSeconds)}s`],
                ['Download requests', summary.data.downloads],
              ].map(([label, v]) => (
                <div className="metric-card" key={label}>
                  <span>{label}</span>
                  <strong>{v}</strong>
                </div>
              ))}
            </div>
            <p>
              First image activity: {date(summary.data.images.first)}
              <br />
              Last image activity: {date(summary.data.images.last)}
            </p>
            <p className="muted">
              {summary.data.images.automation} proxy/suspected automated image requests;{' '}
              {summary.data.clicks.automation} suspected automated link requests.{' '}
              {summary.data.images.legacy} historical deduplicated image events.{' '}
              {summary.data.policy}
            </p>
            {summary.data.capped && (
              <p role="status">Activity reached the collection limit. Counts are incomplete.</p>
            )}
            <h3>Tracked links</h3>
            {summary.data.links.length ? (
              <ul>
                {summary.data.links.map((l) => {
                  let label = l.destination;
                  try {
                    const u = new URL(label);
                    label = u.origin + u.pathname;
                  } catch {}
                  return (
                    <li key={l.id}>
                      {label} — {l.requests} requests, {l.eligible} unclassified requests (not
                      verified humans)
                    </li>
                  );
                })}
              </ul>
            ) : (
              <p>Link tracking was not enabled or there were no eligible links in this send.</p>
            )}
            <p className="muted">Updated {date(summary.data.asOf)}</p>
          </>
        )
      )}
      <h2>Activity timeline</h2>
      <label htmlFor="activity-filter">Event type </label>
      <select id="activity-filter" value={kind} onChange={(e) => setKind(e.target.value)}>
        <option value="">All activity</option>
        {[
          'image_request',
          'link_request',
          'viewer_loaded',
          'viewer_activity',
          'page_exposure',
          'download_request',
          'reply',
          'bounce',
          'conversion',
        ].map((k) => (
          <option key={k}>{k}</option>
        ))}
      </select>
      {events.isPending ? (
        <p role="status">Loading activity…</p>
      ) : events.isError ? (
        <p role="alert">
          {events.error.message} <Button onClick={() => events.refetch()}>Retry</Button>
        </p>
      ) : (
        <>
          <ol className="timeline">
            {events.data?.pages
              .flatMap((p) => p.items)
              .map((e) => (
                <li key={e.id}>
                  <time dateTime={e.at}>{date(e.at)}</time>
                  <span>
                    {e.kind.replaceAll('_', ' ')} · {e.source.replaceAll('_', ' ')} · delivery{' '}
                    {e.deliveryId.slice(0, 8)}
                    {Array.isArray(e.metadata?.pages) &&
                      e.metadata.pages.length > 0 &&
                      ` · pages ${e.metadata.pages.join(', ')}`}
                    {typeof e.metadata?.activeMs === 'number' &&
                      e.metadata.activeMs > 0 &&
                      ` · ${Math.round(e.metadata.activeMs / 1000)}s active`}
                    {typeof e.metadata?.browserFamily === 'string' &&
                      ` · ${e.metadata.browserFamily}`}
                  </span>
                </li>
              ))}
          </ol>
          {!events.data?.pages[0].items.length && (
            <p>No matching activity detected. Images may be blocked or cached.</p>
          )}
          {events.hasNextPage && (
            <Button disabled={events.isFetchingNextPage} onClick={() => events.fetchNextPage()}>
              Load older activity
            </Button>
          )}
        </>
      )}
    </section>
  );
}
