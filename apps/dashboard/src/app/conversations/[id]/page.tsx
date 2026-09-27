'use client';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { Shell } from '@/components/shell';
import { client, unwrap } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
export default function ConversationPage() {
  const params = useParams<{ id: string }>();
  const detail = useQuery({
    queryKey: ['conversation', params.id],
    queryFn: async () => {
      const { api } = await client();
      return unwrap(
        await api.GET('/conversations/{conversationId}', {
          params: { path: { conversationId: params.id } },
        }),
      );
    },
  });
  const row = detail.data;
  return (
    <Shell>
      <div className="page-header">
        <div>
          <Link className="back" href="/">
            ← Conversations
          </Link>
          <h1>{row?.subject ?? 'Conversation'}</h1>
          <p>
            {row?.status === 'prepared'
              ? 'Prepared in Gmail. Send confirmation is unavailable for this path.'
              : row?.status === 'unknown'
                ? 'Send status unknown. Check Gmail Sent before trying again.'
                : 'Tracking is based on image requests and may miss opens.'}
          </p>
        </div>
      </div>
      {detail.isPending ? (
        <div className="list-state">Loading conversation…</div>
      ) : detail.isError ? (
        <div className="list-state error">
          Could not load conversation.{' '}
          <Button variant="link" size="sm" onClick={() => detail.refetch()}>
            Retry
          </Button>
        </div>
      ) : (
        row && (
          <>
            <h2>Deliveries</h2>
            <div className="detail-list">
              {row.deliveries.map((delivery) => (
                <div className="detail-row" key={delivery.id}>
                  <div>
                    <strong>{delivery.recipients.join(', ')}</strong>
                    <p>
                      {delivery.gmailMessageId
                        ? `Gmail ID: ${delivery.gmailMessageId}`
                        : delivery.status === 'prepared'
                          ? 'Pixel inserted in draft'
                          : 'No Gmail ID recorded'}
                    </p>
                  </div>
                  <div className="delivery-actions">
                    <Badge
                      variant={
                        row.events.some((e) => e.deliveryId === delivery.id)
                          ? 'default'
                          : 'secondary'
                      }
                    >
                      {row.events.some((e) => e.deliveryId === delivery.id)
                        ? 'Open detected'
                        : 'No open detected'}
                    </Badge>
                    {delivery.rfcMessageId && (
                      <Link href={`/compose?reply=${row.id}&delivery=${delivery.id}`}>Reply</Link>
                    )}
                    {delivery.rfcMessageId && delivery.replyAllRecipients.length > 1 && (
                      <Link href={`/compose?reply=${row.id}&delivery=${delivery.id}&all=1`}>
                        Reply all
                      </Link>
                    )}
                    <Badge variant="outline">
                      {delivery.status === 'unknown'
                        ? 'Send status unknown'
                        : delivery.status === 'prepared'
                          ? 'Prepared'
                          : delivery.status === 'sent'
                            ? 'Sent'
                            : delivery.status === 'failed'
                              ? 'Failed'
                              : 'Sending'}
                    </Badge>
                  </div>
                  {delivery.error && <p className="error">{delivery.error}</p>}
                </div>
              ))}
            </div>
            <h2>Image requests</h2>
            {row.events.length ? (
              <ol className="timeline">
                {row.events.map((event, i) => (
                  <li key={`${event.deliveryId}-${i}`}>
                    <time dateTime={event.at}>{new Date(event.at).toLocaleString()}</time>
                    <span>
                      Image requested for{' '}
                      {row.deliveries
                        .find((d) => d.id === event.deliveryId)
                        ?.recipients.join(', ') ?? 'delivery'}
                    </span>
                  </li>
                ))}
              </ol>
            ) : (
              <div className="list-state">
                No open detected. Images may be blocked or loaded by privacy tools.
              </div>
            )}
          </>
        )
      )}
    </Shell>
  );
}
