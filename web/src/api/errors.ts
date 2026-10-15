// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { ApiError } from './client';

export function isApiError(error: unknown, status?: number): error is ApiError {
  return error instanceof ApiError && (status === undefined || error.status === status);
}

export function isUnauthorized(error: unknown): boolean {
  return isApiError(error, 401);
}

/** fetch() rejects with a TypeError when the server cannot be reached. */
export function isNetworkError(error: unknown): boolean {
  return error instanceof TypeError;
}

/** Text shown to the user for any error thrown by the API client. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403 && error.code === 'forbidden') return `Permission denied: ${error.message}`;
    return error.message || `${error.status} ${error.code}`;
  }
  if (isNetworkError(error)) return 'Cannot reach the Brokerium server';
  if (error instanceof Error) return error.message;
  return String(error);
}
