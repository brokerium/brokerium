// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import { quotaChanges, quotaEntityKind, quotaEntityLabel } from './quotas';

describe('quota entities', () => {
  it('detects the entity kind', () => {
    expect(quotaEntityKind([{ type: 'user', name: 'alice' }])).toBe('user');
    expect(quotaEntityKind([{ type: 'client-id', name: null }, { type: 'user', name: 'alice' }])).toBe('user+client-id');
    expect(quotaEntityKind([{ type: 'ip', name: '10.0.0.1' }])).toBe('ip');
  });

  it('labels default entities', () => {
    expect(quotaEntityLabel([{ type: 'user', name: null }])).toBe('user=<default>');
  });
});

describe('quotaChanges', () => {
  it('sets changed values and removes cleared ones', () => {
    expect(
      quotaChanges(
        { producer_byte_rate: 1024, consumer_byte_rate: 2048 },
        { producer_byte_rate: 4096, consumer_byte_rate: null, request_percentage: null },
      ),
    ).toEqual({ set: { producer_byte_rate: 4096 }, remove: ['consumer_byte_rate'] });
  });
});
