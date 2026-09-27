'use client';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { Shell } from '@/components/shell';
import { client, unwrap } from '@/lib/api';
export default function Drafts() {
  const drafts = useQuery({
    queryKey: ['drafts'],
    queryFn: async () => {
      const { api } = await client();
      return unwrap(await api.GET('/drafts'));
    },
  });
  return (
    <Shell>
      <div className="page-header">
        <div>
          <h1>Drafts</h1>
          <p>These drafts are saved on this instance.</p>
        </div>
        <Link className="button" href="/compose">
          New draft
        </Link>
      </div>
      {drafts.isPending ? (
        <div className="list-state">Loading drafts…</div>
      ) : drafts.isError ? (
        <div className="list-state error">Could not load drafts.</div>
      ) : drafts.data?.length === 0 ? (
        <div className="list-state">No saved drafts.</div>
      ) : (
        <div className="conversation-list">
          {drafts.data?.map((d) => (
            <Link className="conversation-row" href={`/compose?draft=${d.id}`} key={d.id}>
              <span className="subject">{d.subject || '(no subject)'}</span>
              <span className="muted">{d.to.join(', ') || 'No recipients'}</span>
              <time dateTime={d.updatedAt}>{new Date(d.updatedAt).toLocaleString()}</time>
            </Link>
          ))}
        </div>
      )}
    </Shell>
  );
}
