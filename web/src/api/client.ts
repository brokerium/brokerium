// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { ApiErrorBody } from './types';

/** Error thrown for non-2xx responses. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

let onUnauthorized: () => void = () => {};

/** Registers the handler called on HTTP 401 (for example, redirect to /login). */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    credentials: 'include',
    headers: {
      Accept: 'application/json',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      // Required by the server on every state-changing request (CSRF protection).
      'X-Brokerium-Request': '1',
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401 && !path.startsWith('/auth/login')) {
    onUnauthorized();
  }
  if (res.status === 204) {
    return undefined as T;
  }
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const err = (data as ApiErrorBody | undefined)?.error;
    throw new ApiError(res.status, err?.code ?? 'http_error', err?.message ?? res.statusText);
  }
  return data as T;
}

/** POSTs a request and saves the response as a file, named by its Content-Disposition. */
async function download(path: string, body: unknown, fallbackName: string): Promise<void> {
  const res = await fetch(`/api/v1${path}`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'X-Brokerium-Request': '1' },
    body: JSON.stringify(body),
  });
  if (res.status === 401) onUnauthorized();
  if (!res.ok) {
    const err = ((await res.json().catch(() => undefined)) as ApiErrorBody | undefined)?.error;
    throw new ApiError(res.status, err?.code ?? 'http_error', err?.message ?? res.statusText);
  }
  const name = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')?.[1] ?? fallbackName;
  const url = URL.createObjectURL(await res.blob());
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1_000);
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
  del: <T>(path: string, body?: unknown) => request<T>('DELETE', path, body),
  download,
};
