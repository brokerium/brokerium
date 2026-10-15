// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

export type KafkaVersion = readonly [major: number, minor: number, patch: number];

/** Parses "4.3.1" (or "4.3") into numbers; returns null for anything else. */
export function parseKafkaVersion(text: string): KafkaVersion | null {
  const match = /^\s*(\d+)\.(\d+)(?:\.(\d+))?\s*$/.exec(text);
  if (!match) return null;
  return [Number(match[1]), Number(match[2]), Number(match[3] ?? 0)];
}

export function compareKafkaVersions(a: KafkaVersion, b: KafkaVersion): number {
  for (let i = 0; i < 3; i += 1) {
    if (a[i] !== b[i]) return a[i] - b[i];
  }
  return 0;
}

const KAFKA_20979_FIRST: KafkaVersion = [4, 1, 0];
const KAFKA_20979_FIXED_43: KafkaVersion = [4, 3, 2];

/**
 * KAFKA-20979 (time index truncation not flushed on clean shutdown) affects 4.1.0 and 4.3.0,
 * has no fix in 4.2.x and is fixed in 4.3.2 and 4.4.0. Versions from 4.1.0 up to 4.3.1 are
 * treated as affected.
 */
export function affectedByKafka20979(text: string): boolean {
  const version = parseKafkaVersion(text);
  if (!version) return false;
  return compareKafkaVersions(version, KAFKA_20979_FIRST) >= 0 && compareKafkaVersions(version, KAFKA_20979_FIXED_43) < 0;
}

export const KAFKA_20979_WARNING =
  'Apache Kafka 4.1.0 through 4.3.1 is affected by KAFKA-20979: on clean shutdown the time index truncation is not ' +
  'flushed to disk, so after a rolling restart retention can delete fresh segments. There is no fix in 4.2.x; ' +
  'use 4.3.2 or later, or 4.4.0.';
