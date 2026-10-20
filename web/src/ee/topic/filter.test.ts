// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { describe, expect, it } from 'vitest';

import { EMPTY_FILTER, filterRequest } from './filter';

describe('filterRequest', () => {
  it('leaves an empty filter empty', () => {
    expect(JSON.parse(JSON.stringify(filterRequest(EMPTY_FILTER)))).toEqual({});
  });

  it('maps every field', () => {
    const req = filterRequest({
      ...EMPTY_FILTER,
      partition: '2',
      keyMode: 'regex',
      key: '^order-',
      value: 'PAID',
      headerKey: 'trace',
      headerValue: 'abc',
      ignoreCase: true,
      from: '2026-10-20T12:00:00',
    });
    expect(req).toMatchObject({
      partitions: [2],
      key: { regex: '^order-' },
      value: { contains: 'PAID' },
      header: { key: 'trace', value: { contains: 'abc' } },
      ignoreCase: true,
      fromTime: new Date('2026-10-20T12:00:00').getTime(),
    });
    expect(req.toTime).toBeUndefined();
  });

  it('matches any value of a header without a value text', () => {
    expect(filterRequest({ ...EMPTY_FILTER, headerKey: 'trace' }).header).toEqual({ key: 'trace', value: undefined });
  });
});
