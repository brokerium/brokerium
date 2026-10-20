// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import type { Match, MessageFilter, Selection } from '../api/messageTools';

export type MatchMode = 'contains' | 'regex';

export interface FilterState {
  partition: string;
  keyMode: MatchMode;
  key: string;
  valueMode: MatchMode;
  value: string;
  headerKey: string;
  headerValue: string;
  ignoreCase: boolean;
  /** datetime-local values in local time. */
  from: string;
  to: string;
}

export const EMPTY_FILTER: FilterState = {
  partition: 'all',
  keyMode: 'contains',
  key: '',
  valueMode: 'contains',
  value: '',
  headerKey: '',
  headerValue: '',
  ignoreCase: false,
  from: '',
  to: '',
};

function match(mode: MatchMode, text: string): Match | undefined {
  if (!text) return undefined;
  return mode === 'regex' ? { regex: text } : { contains: text };
}

function millis(local: string): number | undefined {
  const ms = local ? new Date(local).getTime() : Number.NaN;
  return Number.isFinite(ms) ? ms : undefined;
}

/** The request fields of a filter. */
export function filterRequest(f: FilterState): Selection & MessageFilter {
  return {
    partitions: f.partition === 'all' ? undefined : [Number(f.partition)],
    fromTime: millis(f.from),
    toTime: millis(f.to),
    key: match(f.keyMode, f.key),
    value: match(f.valueMode, f.value),
    header: f.headerKey ? { key: f.headerKey, value: match('contains', f.headerValue) } : undefined,
    ignoreCase: f.ignoreCase || undefined,
  };
}
