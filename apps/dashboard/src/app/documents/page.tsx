'use client';
import { useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Shell } from '@/components/shell';
import { ownerRequest } from '@/components/analytics';
import { Button } from '@/components/ui/button';
import type { components } from '@mail-tracker/api-client';
type Document = components['schemas']['HostedDocument'];
type Share = components['schemas']['DocumentShare'];
export default function Documents() {
  const qc = useQueryClient();
  const file = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [url, setURL] = useState('');
  const [days, setDays] = useState(30);
  const [download, setDownload] = useState(true);
  const [deleteID, setDeleteID] = useState('');
  const [selected, setSelected] = useState('');
  const docs = useQuery({
    queryKey: ['documents'],
    queryFn: () => ownerRequest<Document[]>('/documents'),
    refetchInterval: 15000,
  });
  const shares = useQuery({
    queryKey: ['shares', selected],
    enabled: !!selected,
    queryFn: () => ownerRequest<Share[]>(`/documents/${selected}/shares`),
  });
  async function upload() {
    const f = file.current?.files?.[0];
    if (!f) {
      setError('Choose a PDF, JPEG or PNG file first.');
      return;
    }
    if (f.size > 20 * 1024 * 1024) {
      setError('Files must be no larger than 20 MB.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await ownerRequest('/documents', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/octet-stream',
          'X-File-Name': encodeURIComponent(f.name),
        },
        body: f,
      });
      setNotice('Document uploaded privately. Select it in Compose to send a tracked link.');
      if (file.current) file.current.value = '';
      await qc.invalidateQueries({ queryKey: ['documents'] });
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function share(id: string) {
    setBusy(true);
    setError('');
    try {
      const s = await ownerRequest<Share>(`/documents/${id}/shares`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ days, allowDownload: download }),
      });
      setURL(s.url || '');
      setSelected(id);
      await qc.invalidateQueries({ queryKey: ['shares', id] });
      setNotice(
        'Share created. Anyone with this link can access it until expiry or revocation. Activity from this standalone share is shown in the document library.',
      );
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Shell>
      <div className="page-header">
        <div>
          <h1>Documents</h1>
          <p>
            Private hosted PDFs and images. Email tracked links to measure viewer activity; normal
            attachments cannot report offline opens.
          </p>
        </div>
      </div>
      <label htmlFor="document-upload">PDF, JPEG or PNG (maximum 20 MB)</label>
      <input
        ref={file}
        id="document-upload"
        type="file"
        accept="application/pdf,image/jpeg,image/png"
      />
      <Button disabled={busy} onClick={upload}>
        {busy ? 'Working…' : 'Upload document'}
      </Button>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      {url && (
        <div className="list-state">
          <label htmlFor="share-url">New share link (copy it now)</label>
          <input
            id="share-url"
            readOnly
            value={url}
            onFocus={(e) => e.target.select()}
            className="w-full"
          />
          <p>
            For recipient-level conversation analytics, select the document in Compose instead.
            Separate sends create a distinct share for each delivery.
          </p>
        </div>
      )}
      <div className="toolbar">
        <label>
          Share expiry in days{' '}
          <input
            type="number"
            min={1}
            max={365}
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
          />
        </label>
        <label>
          <input
            type="checkbox"
            checked={download}
            onChange={(e) => setDownload(e.target.checked)}
          />{' '}
          Offer a download button
        </label>
      </div>
      <p className="muted">
        Download controls hide the button; recipients can still capture displayed content. These
        links do not verify recipient identity.
      </p>
      {docs.isPending ? (
        <p role="status">Loading documents…</p>
      ) : docs.isError ? (
        <p role="alert">
          {docs.error.message} <Button onClick={() => docs.refetch()}>Retry</Button>
        </p>
      ) : !docs.data?.length ? (
        <div className="list-state">Upload your first document to create a tracked share.</div>
      ) : (
        <div className="detail-list">
          {docs.data.map((d) => (
            <article key={d.id} className="detail-row">
              <div>
                <strong>{d.filename}</strong>
                <p>
                  {(d.sizeBytes / 1024 / 1024).toFixed(1)} MB · {d.sessions} viewer sessions ·{' '}
                  {Math.round(d.activeSeconds)}s approximate active time · {d.downloads} download
                  requests
                </p>
              </div>
              <Button disabled={busy} onClick={() => share(d.id)}>
                Create share
              </Button>
              <Button variant="outline" onClick={() => setSelected(d.id)}>
                Manage shares
              </Button>
              <Button variant="outline" disabled={busy} onClick={() => setDeleteID(d.id)}>
                Delete document
              </Button>
              {deleteID === d.id && (
                <div role="alert">
                  <p>
                    Delete this document and revoke all its links? This cannot be undone. Private
                    file removal runs within one hour.
                  </p>
                  <Button
                    disabled={busy}
                    onClick={async () => {
                      setBusy(true);
                      setError('');
                      try {
                        await ownerRequest(`/documents/${d.id}`, { method: 'DELETE' });
                        setDeleteID('');
                        setSelected('');
                        setURL('');
                        await qc.invalidateQueries({ queryKey: ['documents'] });
                        setNotice('Document deleted. All shares have stopped working.');
                      } catch (e) {
                        setError(String(e));
                      } finally {
                        setBusy(false);
                      }
                    }}
                  >
                    Confirm deletion
                  </Button>
                  <Button variant="outline" onClick={() => setDeleteID('')}>
                    Cancel
                  </Button>
                </div>
              )}
            </article>
          ))}
        </div>
      )}
      {selected && (
        <section>
          <h2>Shares</h2>
          {shares.isPending ? (
            <p>Loading shares…</p>
          ) : shares.isError ? (
            <p role="alert">{shares.error.message}</p>
          ) : (
            shares.data?.map((s) => (
              <div key={s.id} className="detail-row">
                <span>
                  {s.id.slice(0, 8)} · expires {new Date(s.expiresAt).toLocaleString()} ·{' '}
                  {s.revokedAt ? 'Revoked' : s.allowDownload ? 'Downloads offered' : 'Viewer only'}
                </span>
                {!s.revokedAt && (
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={async () => {
                      setBusy(true);
                      setError('');
                      try {
                        await ownerRequest(`/document-shares/${s.id}/revoke`, { method: 'POST' });
                        await shares.refetch();
                        setNotice('Share revoked. Existing viewer sessions no longer have access.');
                      } catch (e) {
                        setError(String(e));
                      } finally {
                        setBusy(false);
                      }
                    }}
                  >
                    Revoke access
                  </Button>
                )}
              </div>
            ))
          )}
        </section>
      )}
    </Shell>
  );
}
