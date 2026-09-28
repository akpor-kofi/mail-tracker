'use client';
import { FormEvent, useState } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { authClient } from '@/lib/auth-client';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
export default function SignIn() {
  const router = useRouter();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const result = await authClient.signIn.email({ email, password });
    setBusy(false);
    if (result.error) setError(result.error.message ?? 'Sign in failed');
    else router.replace('/');
  }
  return (
    <main className="sign-in">
      <form onSubmit={submit}>
        <h1>Sign in</h1>
        <p>Use the owner account created during setup.</p>
        <Label htmlFor="email">
          Email
          <Input
            id="email"
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </Label>
        <Label htmlFor="password">
          Password
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Label>
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        <Button disabled={busy} type="submit">
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
        <p><Link href="/about">About</Link> · <Link href="/privacy">Privacy</Link></p>
      </form>
    </main>
  );
}
