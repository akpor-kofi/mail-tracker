import type { Metadata } from 'next';
import { Providers } from '@/components/providers';
import './styles.css';
export const metadata: Metadata = {
  title: 'Mail Tracker',
  description: 'Self-hosted tracking for your Gmail messages',
};
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
