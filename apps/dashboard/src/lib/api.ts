import { apiClient } from '@mail-tracker/api-client';
import { authClient } from './auth-client';
export async function client() {
  const { data, error } = await authClient.token();
  if (error || !data?.token) throw new Error('Your session expired. Sign in again.');
  return { api: apiClient(data.token), token: data.token };
}
export function unwrap<T>(result: { data?: T; error?: unknown }): T {
  if (result.error || !result.data)
    throw new Error(
      typeof result.error === 'object' && result.error && 'error' in result.error
        ? String(result.error.error)
        : 'Request failed',
    );
  return result.data;
}
