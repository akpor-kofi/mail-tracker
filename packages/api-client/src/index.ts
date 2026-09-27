import createClient from 'openapi-fetch';
import type { paths } from './schema';

export function apiClient(token: string) {
  return createClient<paths>({ baseUrl: '/api/v1', headers: { Authorization: `Bearer ${token}` } });
}
export type { paths, components } from './schema';
