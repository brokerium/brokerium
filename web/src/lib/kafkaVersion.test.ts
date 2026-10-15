// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import { affectedByKafka20979, compareKafkaVersions, parseKafkaVersion } from './kafkaVersion';

describe('parseKafkaVersion', () => {
  it('parses release versions', () => {
    expect(parseKafkaVersion('4.3.1')).toEqual([4, 3, 1]);
    expect(parseKafkaVersion(' 3.9 ')).toEqual([3, 9, 0]);
  });

  it('rejects anything else', () => {
    expect(parseKafkaVersion('latest')).toBeNull();
    expect(parseKafkaVersion('4.3.1-rc0')).toBeNull();
    expect(parseKafkaVersion('')).toBeNull();
  });

  it('compares versions numerically', () => {
    expect(compareKafkaVersions([4, 10, 0], [4, 9, 9])).toBeGreaterThan(0);
    expect(compareKafkaVersions([4, 3, 1], [4, 3, 1])).toBe(0);
  });
});

describe('affectedByKafka20979', () => {
  it.each([
    ['4.0.0', false],
    ['4.1.0', true],
    ['4.2.1', true],
    ['4.3.0', true],
    ['4.3.1', true],
    ['4.3.2', false],
    ['4.4.0', false],
    ['garbage', false],
  ])('%s -> %s', (version, expected) => {
    expect(affectedByKafka20979(version)).toBe(expected);
  });
});
