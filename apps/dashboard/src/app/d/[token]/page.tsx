'use client';
import { Suspense, useEffect, useRef, useState } from 'react';
import { useParams, useRouter, useSearchParams } from 'next/navigation';
import { Button } from '@/components/ui/button';
import type { components } from '@mail-tracker/api-client';
type Session = components['schemas']['ViewerSession'];
async function request<T>(path: string, body?: unknown): Promise<T> {
  const r = await fetch('/api/v1/viewer/' + path, {
    method: body ? 'POST' : 'GET',
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    cache: 'no-store',
  });
  if (!r.ok) {
    const e = await r.json().catch(() => ({ error: 'Could not load document' }));
    throw new Error(e.error);
  }
  return r.json();
}
function Viewer() {
  const { token } = useParams<{ token: string }>();
  const params = useSearchParams();
  const router = useRouter();
  const sid = params.get('session');
  const [session, setSession] = useState<Session | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [pages, setPages] = useState(0);
  const content = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let live = true;
    setError('');
    setLoading(true);
    setSession(null);
    (async () => {
      try {
        if (token === 'view' && sid) {
          const v = await request<Session>(sid);
          if (live) setSession(v);
        } else {
          const v = await request<Session>('sessions', { token });
          if (live) router.replace(`/d/view?session=${encodeURIComponent(v.id)}`);
        }
      } catch (e) {
        if (live) {
          setError(String(e));
          setLoading(false);
        }
      }
    })();
    return () => {
      live = false;
    };
  }, [token, sid, router]);
  useEffect(() => {
    if (!session || !content.current) return;
    let disposed = false;
    let timer: ReturnType<typeof setInterval> | undefined;
    let observer: IntersectionObserver | undefined;
    let task: import('pdfjs-dist').PDFDocumentLoadingTask | undefined;
    const holds = new Map<Element, ReturnType<typeof setTimeout>>();
    const visible = new Set<number>();
    const seen = new Set<number>();
    let lastInput = performance.now();
    let last = lastInput;
    let active = 0;
    const root = content.current;
    root.replaceChildren();
    const interact = () => {
      lastInput = performance.now();
    };
    const names = ['pointerdown', 'keydown', 'wheel', 'touchstart'];
    names.forEach((n) => window.addEventListener(n, interact, { passive: true }));
    const emit = (kind: string, extra: Record<string, unknown> = {}) => {
      void request(`${session.id}/events`, { batchId: crypto.randomUUID(), kind, ...extra }).catch(
        () => {},
      );
    };
    (async () => {
      try {
        if (session.contentType === 'application/pdf') {
          const pdfjs = await import('pdfjs-dist');
          pdfjs.GlobalWorkerOptions.workerSrc = '/pdf.worker.mjs';
          task = pdfjs.getDocument({
            url: `/api/v1/viewer/${session.id}/file`,
            withCredentials: true,
            isEvalSupported: false,
            cMapUrl: '/pdf-assets/cmaps/',
            cMapPacked: true,
            standardFontDataUrl: '/pdf-assets/standard_fonts/',
            wasmUrl: '/pdf-assets/wasm/',
          });
          const pdf = await task.promise;
          if (pdf.numPages > 200)
            throw new Error(
              'This PDF exceeds the 200-page viewer limit. Use the download option if available.',
            );
          if (disposed) return;
          setPages(pdf.numPages);
          for (let i = 1; i <= pdf.numPages; i++) {
            if (disposed) return;
            const page = await pdf.getPage(i);
            const basic = page.getViewport({ scale: 1 });
            const scale = Math.min(
              1.5,
              740 / basic.width,
              Math.sqrt(4000000 / (basic.width * basic.height)),
            );
            const vp = page.getViewport({ scale });
            const canvas = document.createElement('canvas');
            canvas.width = Math.ceil(vp.width);
            canvas.height = Math.ceil(vp.height);
            canvas.dataset.page = String(i);
            canvas.setAttribute('aria-label', `Page ${i}`);
            const ctx = canvas.getContext('2d');
            if (!ctx) throw new Error('Your browser cannot display this PDF.');
            root.append(canvas);
            await page.render({ canvas, canvasContext: ctx, viewport: vp }).promise;
            page.cleanup();
          }
        } else {
          await new Promise<void>((resolve, reject) => {
            const img = document.createElement('img');
            img.alt = session.filename;
            img.dataset.page = '1';
            img.onload = () => resolve();
            img.onerror = () => reject(new Error('The image could not be loaded.'));
            img.src = `/api/v1/viewer/${session.id}/file`;
            root.append(img);
          });
          if (disposed) return;
          setPages(1);
        }
        if (disposed) return;
        setLoading(false);
        emit('viewer_loaded');
        observer = new IntersectionObserver(
          (entries) => {
            for (const e of entries) {
              const page = Number((e.target as HTMLElement).dataset.page);
              if (e.isIntersecting && e.intersectionRatio >= 0.5) {
                visible.add(page);
                if (!seen.has(page) && !holds.has(e.target)) {
                  holds.set(
                    e.target,
                    setTimeout(() => {
                      holds.delete(e.target);
                      if (document.visibilityState === 'visible' && visible.has(page)) {
                        seen.add(page);
                        emit('page_exposure', { pages: [page] });
                      }
                    }, 1000),
                  );
                }
              } else {
                visible.delete(page);
                const h = holds.get(e.target);
                if (h) clearTimeout(h);
                holds.delete(e.target);
              }
            }
          },
          { threshold: [0, 0.5] },
        );
        Array.from(root.children).forEach((e) => observer!.observe(e));
        timer = setInterval(() => {
          const now = performance.now();
          const delta = Math.min(now - last, 1500);
          last = now;
          if (document.visibilityState !== 'visible' || now - lastInput > 30000) return;
          active += delta;
          if (active >= 10000) {
            emit('viewer_activity', { activeMs: Math.round(active), pages: [...visible] });
            active = 0;
          }
        }, 1000);
      } catch (e) {
        if (!disposed) {
          setError(String(e));
          setLoading(false);
        }
      }
    })();
    return () => {
      disposed = true;
      if (timer) clearInterval(timer);
      observer?.disconnect();
      holds.forEach((h) => clearTimeout(h));
      names.forEach((n) => window.removeEventListener(n, interact));
      void task?.destroy();
    };
  }, [session]);
  return (
    <main className="document-viewer">
      <header>
        <h1>{session?.filename || 'Shared document'}</h1>
        <p>
          This hosted viewer records visits, visible pages, approximate foreground activity and
          download requests. A forwarded link does not verify your identity.
        </p>
        {session?.allowDownload && (
          <Button asChild>
            <a href={`/api/v1/viewer/${session.id}/download`} download rel="noreferrer">
              Download original
            </a>
          </Button>
        )}
        {session && (
          <p className="muted">
            Session expires {new Date(session.expiresAt).toLocaleString()} · {pages || '…'} pages.
            Downloaded files are not tracked offline.
          </p>
        )}
      </header>
      {loading && <p role="status">Loading document…</p>}
      {error && (
        <div role="alert" className="list-state error">
          {error}
          <p>Try refreshing. If access has expired, ask the sender for a new link.</p>
          <Button onClick={() => window.location.reload()}>Retry</Button>
        </div>
      )}
      <div ref={content} className="viewer-pages" />
    </main>
  );
}
export default function Page() {
  return (
    <Suspense fallback={<p>Loading viewer…</p>}>
      <Viewer />
    </Suspense>
  );
}
