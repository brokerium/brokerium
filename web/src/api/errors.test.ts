// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import { ApiError } from './client';
import { errorMessage, isApiError, isUnauthorized } from './errors';

describe('errorMessage', () => {
  it('uses the server message of API errors', () => {
    expect(errorMessage(new ApiError(400, 'bad_request', 'name is invalid'))).toBe('name is invalid');
    expect(errorMessage(new ApiError(403, 'forbidden', 'requires role admin'))).toBe('Permission denied: requires role admin');
  });

  it('explains network failures', () => {
    expect(errorMessage(new TypeError('Failed to fetch'))).toBe('Cannot reach the Brokerium server');
  });

  it('detects specific statuses', () => {
    const err = new ApiError(401, 'unauthenticated', 'login required');
    expect(isUnauthorized(err)).toBe(true);
    expect(isApiError(err, 404)).toBe(false);
    expect(isUnauthorized(new Error('x'))).toBe(false);
  });
});
