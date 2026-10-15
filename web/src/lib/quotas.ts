// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import type { QuotaEntity } from '../api/types';

export type QuotaEntityKind = 'user' | 'client-id' | 'user+client-id' | 'ip';

export interface QuotaKey {
  key: string;
  label: string;
  unit: 'bytes/s' | '%' | 'per s';
  /** Entity kinds the key applies to. */
  kinds: QuotaEntityKind[];
}

export const QUOTA_KEYS: QuotaKey[] = [
  { key: 'producer_byte_rate', label: 'Producer byte rate', unit: 'bytes/s', kinds: ['user', 'client-id', 'user+client-id'] },
  { key: 'consumer_byte_rate', label: 'Consumer byte rate', unit: 'bytes/s', kinds: ['user', 'client-id', 'user+client-id'] },
  { key: 'request_percentage', label: 'Request time', unit: '%', kinds: ['user', 'client-id', 'user+client-id'] },
  { key: 'controller_mutation_rate', label: 'Controller mutation rate', unit: 'per s', kinds: ['user', 'client-id', 'user+client-id'] },
  { key: 'connection_creation_rate', label: 'Connection creation rate', unit: 'per s', kinds: ['ip'] },
];

export function quotaEntityKind(entity: QuotaEntity[]): QuotaEntityKind {
  const types = entity.map((e) => e.type).sort();
  if (types.length === 2 && types[0] === 'client-id' && types[1] === 'user') return 'user+client-id';
  if (types[0] === 'ip') return 'ip';
  if (types[0] === 'client-id') return 'client-id';
  return 'user';
}

export function quotaEntityLabel(entity: QuotaEntity[]): string {
  return entity.map((e) => `${e.type}=${e.name ?? '<default>'}`).join(', ');
}

export function quotaEntityKey(entity: QuotaEntity[]): string {
  return [...entity]
    .sort((a, b) => a.type.localeCompare(b.type))
    .map((e) => `${e.type}:${e.name ?? '\u0000'}`)
    .join('|');
}

/**
 * Computes the PUT body from the previous and the edited values: keys with a value are set,
 * keys that had a value and were cleared are removed.
 */
export function quotaChanges(
  before: Record<string, number>,
  after: Record<string, number | null>,
): { set: Record<string, number>; remove: string[] } {
  const set: Record<string, number> = {};
  const remove: string[] = [];
  for (const [key, value] of Object.entries(after)) {
    if (value === null) {
      if (key in before) remove.push(key);
    } else if (before[key] !== value) {
      set[key] = value;
    }
  }
  return { set, remove };
}
