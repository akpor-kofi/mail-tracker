'use client';
import Link from 'next/link';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Shell } from '@/components/shell';
import { client, unwrap } from '@/lib/api';
import { ownerRequest } from '@/components/analytics';
import type { components } from '@mail-tracker/api-client';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';

type Conversation = components['schemas']['Conversation'];
function statusLabel(row: Conversation) {
  if (row.status === 'prepared') return 'Prepared';
  if (row.status === 'unknown') return 'Send status unknown';
  if (row.status === 'partial_or_failed') return 'Partial or failed send';
  if (row.status === 'pending') return 'Sending';
  return 'Sent';
}
export default function Dashboard() {
  const [mailboxId, setMailboxId] = useState('');
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [notice, setNotice] = useState('');
  const qc = useQueryClient();
  const mailboxes = useQuery({
    queryKey: ['mailboxes'],
    queryFn: async () => {
      const { api } = await client();
      return unwrap(await api.GET('/mailboxes'));
    },
  });
  const conversations = useQuery({
    queryKey: ['conversations', mailboxId, offset],
    queryFn: async () => {
      const page = await ownerRequest<{ items: Conversation[]; hasMore: boolean }>(
        `/conversation-pages?mailboxId=${encodeURIComponent(mailboxId)}&offset=${offset}`,
      );
      setHasMore(page.hasMore);
      return page.items;
    },
  });
  useEffect(() => {
    let stopped = false;
    let controller: AbortController | undefined;
    async function connect() {
      while (!stopped) {
        try {
          const { token } = await client();
          controller = new AbortController();
          const response = await fetch('/api/v1/events', {
            headers: { Authorization: `Bearer ${token}` },
            signal: controller.signal,
          });
          if (!response.ok || !response.body) throw new Error('Stream disconnected');
          await qc.invalidateQueries({ queryKey: ['conversations'] });
          const reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = '';
          while (!stopped) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            let index;
            while ((index = buffer.indexOf('\n\n')) >= 0) {
              const packet = buffer.slice(0, index);
              buffer = buffer.slice(index + 2);
              if (packet.includes('event: changed')) {
                setNotice('Activity updated');
                await qc.invalidateQueries({ queryKey: ['conversations'] });
                await qc.invalidateQueries({ queryKey: ['conversation'] });
              }
            }
          }
        } catch {
          if (!stopped) setNotice('Live updates disconnected. Reconnecting…');
        }
        if (!stopped) await new Promise((resolve) => setTimeout(resolve, 5000));
      }
    }
    connect();
    return () => {
      stopped = true;
      controller?.abort();
    };
  }, [qc]);
  return (
    <Shell>
      <div className="page-header">
        <div>
          <h1>Conversations</h1>
          <p>Open detection depends on the recipient&apos;s mail client loading images.</p>
        </div>
        <Button asChild>
          <Link href="/compose">Compose</Link>
        </Button>
      </div>
      <div className="toolbar">
        <div className="filter-field">
          <Label htmlFor="mailbox-filter">Mailbox</Label>
          <Select
            value={mailboxId || 'all'}
            onValueChange={(value) => {
              setMailboxId(value === 'all' ? '' : value);
              setOffset(0);
            }}
          >
            <SelectTrigger id="mailbox-filter" className="w-full">
              <SelectValue placeholder="All mailboxes" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All mailboxes</SelectItem>
              {mailboxes.data?.map((m) => (
                <SelectItem key={m.id} value={m.id}>
                  {m.email}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          variant="outline"
          onClick={() => conversations.refetch()}
          disabled={conversations.isFetching}
        >
          Refresh
        </Button>
      </div>
      {notice && (
        <Alert className="activity-notice" role="status">
          <AlertDescription>{notice}</AlertDescription>
          <Button variant="link" size="sm" onClick={() => setNotice('')}>
            Dismiss
          </Button>
        </Alert>
      )}
      {conversations.isPending ? (
        <div className="list-state">Loading conversations…</div>
      ) : conversations.isError ? (
        <div className="list-state error">
          Could not load conversations.{' '}
          <Button variant="link" size="sm" onClick={() => conversations.refetch()}>
            Retry
          </Button>
        </div>
      ) : conversations.data?.length === 0 ? (
        <div className="list-state">
          <strong>No tracked messages yet</strong>
          <p>Connect a Gmail account, then send from the composer or prepare a draft in Gmail.</p>
          <Link href="/settings">Connect Gmail</Link>
        </div>
      ) : (
        <div className="conversation-list" role="list">
          {conversations.data?.map((row) => (
            <Link
              role="listitem"
              href={`/conversations/${row.id}`}
              className="conversation-row"
              key={row.id}
            >
              <span className="subject">{row.subject}</span>
              <span className="muted">
                {mailboxes.data?.find((m) => m.id === row.mailboxId)?.email ?? 'Mailbox'}
              </span>
              <Badge className="status" variant="outline">
                {statusLabel(row)}
              </Badge>
              <Badge variant={row.openStatus === 'open_detected' ? 'default' : 'secondary'}>
                {row.openStatus === 'open_detected' ? 'Open detected' : 'No open detected'}
              </Badge>
              <time dateTime={row.updatedAt}>{new Date(row.updatedAt).toLocaleString()}</time>
            </Link>
          ))}
        </div>
      )}
      <div className="toolbar">
        <Button
          variant="outline"
          disabled={offset === 0 || conversations.isFetching}
          onClick={() => setOffset(Math.max(0, offset - 50))}
        >
          Previous
        </Button>
        <span>Page {offset / 50 + 1}</span>
        <Button
          variant="outline"
          disabled={!hasMore || conversations.isFetching}
          onClick={() => setOffset(offset + 50)}
        >
          Next
        </Button>
      </div>
    </Shell>
  );
}
