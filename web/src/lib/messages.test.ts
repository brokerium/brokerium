// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { describe, expect, it } from 'vitest';

import { hasContent, isBase64, prettyJson, previewValue, renderValue } from './messages';

describe('isBase64', () => {
  it('accepts padded, unpadded and wrapped base64', () => {
    for (const text of ['', '/wA=', '/wA', 'AP8=', 'aGVsbG8gd29ybGQ=', 'aGVs\nbG8=\n']) expect(isBase64(text), text).toBe(true);
  });

  it('rejects other alphabets and impossible lengths', () => {
    for (const text of ['%%%', 'A', 'ab=c', 'abc===', '-_8=']) expect(isBase64(text), text).toBe(false);
  });
});

describe('renderValue', () => {
  it('pretty-prints JSON objects', () => {
    expect(renderValue('{"a":1,"b":[true]}', 'utf8')).toEqual({ kind: 'json', text: '{\n  "a": 1,\n  "b": [\n    true\n  ]\n}' });
  });

  it('keeps plain text and invalid JSON as text', () => {
    expect(renderValue('hello', 'utf8').kind).toBe('text');
    expect(renderValue('{not json', 'utf8').kind).toBe('text');
    expect(renderValue('42', 'utf8').kind).toBe('text');
  });

  it('marks base64 payloads as binary and null as empty', () => {
    expect(renderValue('AAEC', 'base64')).toEqual({ kind: 'binary', text: 'AAEC' });
    expect(renderValue(null, 'utf8')).toEqual({ kind: 'empty', text: '' });
  });
});

describe('previewValue', () => {
  it('collapses whitespace and truncates', () => {
    expect(previewValue('{\n  "a": 1\n}')).toBe('{ "a": 1 }');
    expect(previewValue('abcdef', 4)).toBe('abc…');
    expect(previewValue(null)).toBe('');
  });
});

describe('detail helpers', () => {
  it('detects empty details', () => {
    expect(hasContent(null)).toBe(false);
    expect(hasContent({})).toBe(false);
    expect(hasContent([])).toBe(false);
    expect(hasContent({ a: 1 })).toBe(true);
    expect(hasContent(0)).toBe(true);
  });

  it('pretty-prints JSON values', () => {
    expect(prettyJson({ a: 1 })).toBe('{\n  "a": 1\n}');
    expect(prettyJson(undefined)).toBe('');
  });
});
