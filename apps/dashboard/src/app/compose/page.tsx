'use client';
import { Suspense, useEffect, useState } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Shell } from '@/components/shell';
import { Editor } from '@/components/editor';
import { client, unwrap } from '@/lib/api';
import type { components } from '@mail-tracker/api-client';
type Draft = components['schemas']['DraftInput'];
const blank: Draft = { mailboxId: '', to: [], cc: [], bcc: [], subject: '', html: '<p></p>' };
function addresses(value: string) {
  return value
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean);
}
function Composer() {
  const search = useSearchParams();
  const router = useRouter();
  const qc = useQueryClient();
  const draftId = search.get('draft');
  const replyConversation = search.get('reply');
  const replyDelivery = search.get('delivery');
  const replyAll = search.get('all') === '1';
  const [draft, setDraft] = useState<Draft>(blank);
  const [toText, setToText] = useState('');
  const [ccText, setCcText] = useState('');
  const [bccText, setBccText] = useState('');
  const [mode, setMode] = useState<'shared' | 'separate' | ''>('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [result, setResult] = useState<components['schemas']['SendResult'] | null>(null);
  const mailboxes = useQuery({
    queryKey: ['mailboxes'],
    queryFn: async () => {
      const { api } = await client();
      return unwrap(await api.GET('/mailboxes'));
    },
  });
  const loaded = useQuery({
    queryKey: ['draft', draftId],
    enabled: !!draftId,
    queryFn: async () => {
      const { api } = await client();
      return unwrap(
        await api.GET('/drafts/{draftId}', { params: { path: { draftId: draftId! } } }),
      );
    },
  });
  useEffect(() => {
    if (loaded.data) {
      setDraft(loaded.data);
      setToText(loaded.data.to.join(', '));
      setCcText(loaded.data.cc.join(', '));
      setBccText(loaded.data.bcc.join(', '));
    }
  }, [loaded.data]);
  const reply = useQuery({
    queryKey: ['conversation', replyConversation],
    enabled: !!replyConversation,
    queryFn: async () => {
      const { api } = await client();
      return unwrap(
        await api.GET('/conversations/{conversationId}', {
          params: { path: { conversationId: replyConversation! } },
        }),
      );
    },
  });
  useEffect(() => {
    if (!reply.data) return;
    const delivery = reply.data.deliveries.find((d) => d.id === replyDelivery);
    if (!delivery) return;
    const to = replyAll ? delivery.recipients : delivery.recipients.slice(0, 1);
    setDraft((d) => ({
      ...d,
      mailboxId: reply.data!.mailboxId,
      to,
      subject: reply.data!.subject.startsWith('Re:')
        ? reply.data!.subject
        : `Re: ${reply.data!.subject}`,
      replyToMessageId: delivery.rfcMessageId,
      threadId: delivery.gmailThreadId,
    }));
    setToText(to.join(', '));
    if (replyAll) setMode('shared');
  }, [reply.data, replyDelivery, replyAll]);
  useEffect(() => {
    if (!draft.mailboxId && mailboxes.data?.length)
      setDraft((d) => ({ ...d, mailboxId: mailboxes.data![0].id }));
  }, [mailboxes.data, draft.mailboxId]);
  function update<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((d) => ({ ...d, [key]: value }));
  }
  async function save() {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const { api } = await client();
      const saved = unwrap(await api.POST('/drafts', { body: draft }));
      setDraft(saved);
      qc.invalidateQueries({ queryKey: ['drafts'] });
      setMessage('Draft saved on this instance.');
      router.replace(`/compose?draft=${saved.id}`);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function upload(files: FileList | null) {
    if (!files) return;
    setBusy(true);
    setError('');
    try {
      const { token } = await client();
      const ids = [...(draft.attachments ?? [])];
      for (const file of Array.from(files)) {
        const response = await fetch('/api/v1/attachments', {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${token}`,
            'Content-Type': file.type || 'application/octet-stream',
            'X-File-Name': file.name,
          },
          body: file,
        });
        if (!response.ok) throw new Error(`Attachment upload failed: ${file.name}`);
        ids.push((await response.json()).id);
      }
      update('attachments', ids);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function send() {
    setBusy(true);
    setError('');
    setMessage('');
    setResult(null);
    try {
      if (draft.to.length > 1 && !mode)
        throw new Error('Choose how to send to multiple recipients.');
      const sendMode = mode || 'shared';
      if (sendMode === 'separate' && (draft.cc.length || draft.bcc.length))
        throw new Error('Separate sends cannot include Cc or Bcc.');
      const { api } = await client();
      const sent = unwrap(
        await api.POST('/send', { body: { draft, sendMode, idempotencyKey: crypto.randomUUID() } }),
      );
      setResult(sent);
      qc.invalidateQueries({ queryKey: ['conversations'] });
      setMessage(
        sent.deliveries.every((d) => d.status === 'sent')
          ? 'Message sent.'
          : 'Some delivery outcomes need attention.',
      );
    } catch (e) {
      setError(
        `Send request did not complete: ${String(e)}. Check Gmail Sent before trying again.`,
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <Shell>
      <div className="page-header">
        <div>
          <h1>{draftId ? 'Edit draft' : 'Compose'}</h1>
          <p>Send from a connected Gmail account with a transparent tracking image.</p>
        </div>
      </div>
      {mailboxes.data?.length === 0 ? (
        <div className="list-state">Connect a Gmail account in Settings before composing.</div>
      ) : (
        <div className="compose-form">
          <label>
            From
            <select value={draft.mailboxId} onChange={(e) => update('mailboxId', e.target.value)}>
              {mailboxes.data?.map((m) => (
                <option value={m.id} key={m.id}>
                  {m.email}
                </option>
              ))}
            </select>
          </label>
          <label>
            To
            <input
              value={toText}
              onChange={(e) => {
                setToText(e.target.value);
                update('to', addresses(e.target.value));
              }}
              placeholder="person@example.com"
            />
          </label>
          <div className="form-grid">
            <label>
              Cc
              <input
                value={ccText}
                onChange={(e) => {
                  setCcText(e.target.value);
                  update('cc', addresses(e.target.value));
                }}
              />
            </label>
            <label>
              Bcc
              <input
                value={bccText}
                onChange={(e) => {
                  setBccText(e.target.value);
                  update('bcc', addresses(e.target.value));
                }}
              />
            </label>
          </div>
          <label>
            Subject
            <input value={draft.subject} onChange={(e) => update('subject', e.target.value)} />
          </label>
          <label>
            Message
            <Editor value={draft.html} onChange={(html) => update('html', html)} />
          </label>
          <label>
            Attachments
            <input type="file" multiple onChange={(e) => upload(e.target.files)} />
          </label>
          {!!draft.attachments?.length && (
            <p className="muted">{draft.attachments.length} attachment(s) uploaded</p>
          )}
          {draft.to.length > 1 && (
            <fieldset className="send-modes">
              <legend>How should this go to multiple people?</legend>
              <label>
                <input
                  type="radio"
                  name="mode"
                  checked={mode === 'separate'}
                  onChange={() => setMode('separate')}
                />{' '}
                Separate sends — one message and pixel per To recipient; no Cc or Bcc
              </label>
              <label>
                <input
                  type="radio"
                  name="mode"
                  checked={mode === 'shared'}
                  onChange={() => setMode('shared')}
                />{' '}
                Shared send — preserve To, Cc and Bcc; tracking is aggregate
              </label>
            </fieldset>
          )}
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          {message && (
            <p className="success" role="status">
              {message}
            </p>
          )}
          {result && (
            <div className="result-list">
              {result.deliveries.map((d) => (
                <p key={d.id}>
                  <strong>{d.recipients.join(', ')}</strong> —{' '}
                  {d.status === 'unknown' ? 'Send status unknown. Check Gmail Sent.' : d.status}
                  {d.error && `: ${d.error}`}
                </p>
              ))}
            </div>
          )}
          <div className="form-actions">
            <button className="secondary" disabled={busy || !draft.mailboxId} onClick={save}>
              Save draft
            </button>
            <button
              disabled={
                busy ||
                !draft.mailboxId ||
                !draft.to.length ||
                !draft.subject ||
                (draft.to.length > 1 && !mode)
              }
              onClick={send}
            >
              {busy ? 'Working…' : 'Track and send'}
            </button>
          </div>
        </div>
      )}
    </Shell>
  );
}
export default function ComposePage() {
  return (
    <Suspense fallback={<div className="app-loading">Loading composer…</div>}>
      <Composer />
    </Suspense>
  );
}
