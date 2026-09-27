'use client';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect } from 'react';
import { authClient } from '@/lib/auth-client';
export function Shell({ children }: { children: React.ReactNode }) {
  const { data, isPending } = authClient.useSession();
  const pathname = usePathname();
  const router = useRouter();
  useEffect(() => {
    if (!isPending && !data) router.replace('/sign-in');
  }, [data, isPending, router]);
  if (isPending || !data) return <div className="app-loading">Loading your workspace…</div>;
  return (
    <div className="layout">
      <aside className="sidebar">
        <Link href="/" className="brand">
          Mail Tracker
        </Link>
        <nav aria-label="Main navigation">
          {[
            ['/', 'Conversations'],
            ['/compose', 'Compose'],
            ['/drafts', 'Drafts'],
            ['/settings', 'Settings'],
          ].map(([href, label]) => (
            <Link key={href} href={href} aria-current={pathname === href ? 'page' : undefined}>
              {label}
            </Link>
          ))}
        </nav>
        <div className="sidebar-foot">
          <span>{data.user.email}</span>
          <button
            className="text-button"
            onClick={async () => {
              await authClient.signOut();
              router.replace('/sign-in');
            }}
          >
            Sign out
          </button>
        </div>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}
