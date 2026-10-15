// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import { parseKeyValueLines, principalError, topicNameError } from './validation';

describe('topicNameError', () => {
  it.each([
    ['orders', null],
    ['orders.v2_eu-1', null],
    ['', 'Name is required'],
    ['..', 'Name cannot be "." or ".."'],
    ['a b', 'Use only letters, digits, ".", "_" and "-"'],
    ['x'.repeat(250), 'Name is longer than 249 characters'],
  ])('%s -> %s', (name, expected) => {
    expect(topicNameError(name)).toBe(expected);
  });
});

describe('parseKeyValueLines', () => {
  it('parses key=value lines and skips comments', () => {
    expect(parseKeyValueLines('retention.ms=1000\n# note\n\ncleanup.policy = compact')).toEqual({
      'retention.ms': '1000',
      'cleanup.policy': 'compact',
    });
  });

  it('rejects malformed lines', () => {
    expect(parseKeyValueLines('retention.ms')).toBeNull();
    expect(parseKeyValueLines('=1')).toBeNull();
  });
});

describe('principalError', () => {
  it('accepts typed principals', () => {
    expect(principalError('User:alice')).toBeNull();
    expect(principalError('alice')).not.toBeNull();
  });
});
