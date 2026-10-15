// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

export type ValueKind = 'empty' | 'json' | 'text' | 'binary';

export interface RenderedValue {
  kind: ValueKind;
  /** Full text: pretty-printed JSON, plain text or base64. */
  text: string;
}

/** Prepares a record key or value for display; JSON objects and arrays are pretty-printed. */
export function renderValue(value: string | null, encoding: 'utf8' | 'base64'): RenderedValue {
  if (value === null) return { kind: 'empty', text: '' };
  if (encoding === 'base64') return { kind: 'binary', text: value };
  const trimmed = value.trim();
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return { kind: 'json', text: JSON.stringify(JSON.parse(trimmed), null, 2) };
    } catch {
      // Not JSON after all; show as text.
    }
  }
  return { kind: 'text', text: value };
}

/** Single-line preview of a value, cut to the given length. */
export function previewValue(value: string | null, max = 120): string {
  if (value === null) return '';
  const line = value.replace(/\s+/g, ' ').trim();
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}

/** Pretty-prints any JSON-compatible value for detail panels. */
export function prettyJson(value: unknown): string {
  if (value === undefined) return '';
  try {
    return JSON.stringify(value, null, 2) ?? '';
  } catch {
    return String(value);
  }
}

/** True when a detail value carries information worth showing. */
export function hasContent(value: unknown): boolean {
  if (value === null || value === undefined || value === '') return false;
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === 'object') return Object.keys(value).length > 0;
  return true;
}
