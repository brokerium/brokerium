// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

export const EMPTY = '—';

const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'] as const;

function trimZeros(text: string): string {
  return text.includes('.') ? text.replace(/\.?0+$/, '') : text;
}

function isNumber(value: number | null | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

/** Formats a byte count with binary units: 1536 -> "1.5 KiB". */
export function formatBytes(bytes: number | null | undefined, fractionDigits = 1): string {
  if (!isNumber(bytes)) return EMPTY;
  const sign = bytes < 0 ? '-' : '';
  let value = Math.abs(bytes);
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  if (unit === 0) return `${sign}${Math.round(value)} B`;
  // Rounding can carry over to the next unit (1023.96 KiB -> 1024.0 KiB).
  if (Number(value.toFixed(fractionDigits)) >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${sign}${trimZeros(value.toFixed(fractionDigits))} ${BYTE_UNITS[unit]}`;
}

export function formatBytesPerSecond(bytes: number | null | undefined): string {
  return isNumber(bytes) ? `${formatBytes(bytes)}/s` : EMPTY;
}

const integerFormat = new Intl.NumberFormat('en-US');
const compactFormat = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 });

export function formatNumber(value: number | null | undefined): string {
  return isNumber(value) ? integerFormat.format(value) : EMPTY;
}

export function formatCompact(value: number | null | undefined): string {
  return isNumber(value) ? compactFormat.format(value) : EMPTY;
}

/** Formats a rate such as messages per second with up to one decimal. */
export function formatRate(value: number | null | undefined, unit = '/s'): string {
  if (!isNumber(value)) return EMPTY;
  const text = value >= 100 ? integerFormat.format(Math.round(value)) : trimZeros(value.toFixed(1));
  return `${text}${unit}`;
}

export function formatPercent(value: number | null | undefined): string {
  if (!isNumber(value)) return EMPTY;
  return `${trimZeros(value.toFixed(value < 10 ? 1 : 0))}%`;
}

/** Returns part/total in percent, or null when the total is unknown. */
export function percentOf(part: number, total: number): number | null {
  if (!isNumber(part) || !isNumber(total) || total <= 0) return null;
  return Math.min(100, Math.max(0, (part / total) * 100));
}

/** Formats a duration in milliseconds: 450 ms, 12 s, 3 min 20 s, 1 h 5 min, 2 d 3 h. */
export function formatDuration(ms: number | null | undefined): string {
  if (!isNumber(ms) || ms < 0) return EMPTY;
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${trimZeros(seconds.toFixed(seconds < 10 ? 1 : 0))} s`;
  const totalSeconds = Math.floor(seconds);
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const secs = totalSeconds % 60;
  if (days > 0) return hours > 0 ? `${days} d ${hours} h` : `${days} d`;
  if (hours > 0) return minutes > 0 ? `${hours} h ${minutes} min` : `${hours} h`;
  return secs > 0 ? `${minutes} min ${secs} s` : `${minutes} min`;
}

export type TimeInput = string | number | Date | null | undefined;

/** Converts an ISO string, epoch milliseconds or Date to epoch milliseconds; null when unknown. */
export function toEpochMs(input: TimeInput): number | null {
  if (input === null || input === undefined || input === '') return null;
  const ms = input instanceof Date ? input.getTime() : typeof input === 'number' ? input : Date.parse(input);
  // Kafka uses -1 for "unknown"; Go zero times are far in the past.
  if (!Number.isFinite(ms) || ms <= 0) return null;
  return ms;
}

/** Formats a point in time relative to now: "just now", "12 s ago", "5 min ago", "in 3 h". */
export function formatRelativeTime(input: TimeInput, now: number = Date.now()): string {
  const ms = toEpochMs(input);
  if (ms === null) return EMPTY;
  const diff = now - ms;
  const abs = Math.abs(diff);
  if (abs < 1000) return 'just now';
  let text: string;
  if (abs < 60_000) text = `${Math.floor(abs / 1000)} s`;
  else if (abs < 3_600_000) text = `${Math.floor(abs / 60_000)} min`;
  else if (abs < 86_400_000) text = `${Math.floor(abs / 3_600_000)} h`;
  else if (abs < 30 * 86_400_000) text = `${Math.floor(abs / 86_400_000)} d`;
  else return formatDateTime(ms);
  return diff >= 0 ? `${text} ago` : `in ${text}`;
}

const dateTimeFormat = new Intl.DateTimeFormat(undefined, {
  year: 'numeric',
  month: 'short',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
});

export function formatDateTime(input: TimeInput): string {
  const ms = toEpochMs(input);
  return ms === null ? EMPTY : dateTimeFormat.format(ms);
}

/** Duration between two points in time; the end defaults to now. */
export function durationBetween(start: TimeInput, end?: TimeInput, now: number = Date.now()): number | null {
  const from = toEpochMs(start);
  if (from === null) return null;
  const to = toEpochMs(end) ?? now;
  return Math.max(0, to - from);
}

/** Splits a comma, space or newline separated list, trimming and removing duplicates. */
export function splitList(text: string): string[] {
  const seen = new Set<string>();
  for (const part of text.split(/[\s,;]+/)) {
    const item = part.trim();
    if (item) seen.add(item);
  }
  return [...seen];
}

export function pluralize(count: number, singular: string, plural = `${singular}s`): string {
  return `${formatNumber(count)} ${count === 1 ? singular : plural}`;
}

/** Builds a query string from defined values: {a: 1, b: undefined} -> "?a=1". */
export function queryString(params: Record<string, string | number | boolean | null | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    search.set(key, String(value));
  }
  const text = search.toString();
  return text ? `?${text}` : '';
}
