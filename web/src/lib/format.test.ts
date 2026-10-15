// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import {
  durationBetween,
  formatBytes,
  formatBytesPerSecond,
  formatDuration,
  formatNumber,
  formatPercent,
  formatRate,
  formatRelativeTime,
  percentOf,
  pluralize,
  queryString,
  splitList,
  toEpochMs,
} from './format';

describe('formatBytes', () => {
  it.each([
    [0, '0 B'],
    [512, '512 B'],
    [1023, '1023 B'],
    [1024, '1 KiB'],
    [1536, '1.5 KiB'],
    [10 * 1024 * 1024, '10 MiB'],
    [5.25 * 1024 ** 3, '5.3 GiB'],
    [3 * 1024 ** 4, '3 TiB'],
    [-2048, '-2 KiB'],
  ])('formats %d as %s', (input, expected) => {
    expect(formatBytes(input)).toBe(expected);
  });

  it('carries rounding over to the next unit', () => {
    expect(formatBytes(1024 * 1024 - 1)).toBe('1 MiB');
  });

  it('respects the requested precision', () => {
    expect(formatBytes(1234567, 2)).toBe('1.18 MiB');
  });

  it('renders unknown values as a dash', () => {
    expect(formatBytes(undefined)).toBe('—');
    expect(formatBytes(null)).toBe('—');
    expect(formatBytes(Number.NaN)).toBe('—');
  });

  it('formats throughput', () => {
    expect(formatBytesPerSecond(2048)).toBe('2 KiB/s');
  });
});

describe('formatDuration', () => {
  it.each([
    [0, '0 ms'],
    [450, '450 ms'],
    [1500, '1.5 s'],
    [12_000, '12 s'],
    [60_000, '1 min'],
    [200_000, '3 min 20 s'],
    [3_600_000, '1 h'],
    [3_900_000, '1 h 5 min'],
    [2 * 86_400_000 + 3 * 3_600_000, '2 d 3 h'],
  ])('formats %d ms as %s', (input, expected) => {
    expect(formatDuration(input)).toBe(expected);
  });

  it('renders negative or unknown durations as a dash', () => {
    expect(formatDuration(-1)).toBe('—');
    expect(formatDuration(undefined)).toBe('—');
  });
});

describe('formatRelativeTime', () => {
  const now = Date.parse('2026-10-15T12:00:00Z');

  it.each([
    ['2026-10-15T12:00:00Z', 'just now'],
    ['2026-10-15T11:59:48Z', '12 s ago'],
    ['2026-10-15T11:55:00Z', '5 min ago'],
    ['2026-10-15T09:00:00Z', '3 h ago'],
    ['2026-10-13T12:00:00Z', '2 d ago'],
    ['2026-10-15T12:10:00Z', 'in 10 min'],
  ])('formats %s as %s', (input, expected) => {
    expect(formatRelativeTime(input, now)).toBe(expected);
  });

  it('accepts epoch milliseconds', () => {
    expect(formatRelativeTime(now - 7_000, now)).toBe('7 s ago');
  });

  it('treats Kafka "unknown" timestamps and Go zero times as unknown', () => {
    expect(formatRelativeTime(-1, now)).toBe('—');
    expect(formatRelativeTime('0001-01-01T00:00:00Z', now)).toBe('—');
    expect(formatRelativeTime(null, now)).toBe('—');
  });
});

describe('time helpers', () => {
  it('converts inputs to epoch milliseconds', () => {
    expect(toEpochMs('1970-01-01T00:00:01Z')).toBe(1000);
    expect(toEpochMs(new Date(5000))).toBe(5000);
    expect(toEpochMs('not a date')).toBeNull();
  });

  it('computes durations with an open end', () => {
    const now = 10_000;
    expect(durationBetween(4000, 9000, now)).toBe(5000);
    expect(durationBetween(4000, null, now)).toBe(6000);
    expect(durationBetween(null, 9000, now)).toBeNull();
  });
});

describe('number helpers', () => {
  it('formats integers, rates and percentages', () => {
    expect(formatNumber(1234567)).toBe('1,234,567');
    expect(formatRate(12.34)).toBe('12.3/s');
    expect(formatRate(1234.5)).toBe('1,235/s');
    expect(formatPercent(7.25)).toBe('7.3%');
    expect(formatPercent(42.4)).toBe('42%');
  });

  it('computes bounded percentages', () => {
    expect(percentOf(25, 100)).toBe(25);
    expect(percentOf(150, 100)).toBe(100);
    expect(percentOf(1, 0)).toBeNull();
  });

  it('pluralizes nouns', () => {
    expect(pluralize(1, 'broker')).toBe('1 broker');
    expect(pluralize(3, 'broker')).toBe('3 brokers');
    expect(pluralize(2, 'replica', 'replicas')).toBe('2 replicas');
  });
});

describe('splitList and queryString', () => {
  it('splits bootstrap server lists', () => {
    expect(splitList(' a:9092, b:9092\nc:9092 ,a:9092,, ')).toEqual(['a:9092', 'b:9092', 'c:9092']);
    expect(splitList('')).toEqual([]);
  });

  it('builds query strings from defined values', () => {
    expect(queryString({ partition: -1, from: 'earliest', offset: undefined, limit: 50 })).toBe(
      '?partition=-1&from=earliest&limit=50',
    );
    expect(queryString({ clusterId: '' })).toBe('');
  });
});
