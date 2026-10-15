// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

const TOPIC_NAME = /^[a-zA-Z0-9._-]+$/;

/** Returns an error message for an invalid Kafka topic name, or null when it is valid. */
export function topicNameError(name: string): string | null {
  if (!name) return 'Name is required';
  if (name === '.' || name === '..') return 'Name cannot be "." or ".."';
  if (name.length > 249) return 'Name is longer than 249 characters';
  if (!TOPIC_NAME.test(name)) return 'Use only letters, digits, ".", "_" and "-"';
  return null;
}

/** Parses "key=value" lines; returns null when a line is malformed. */
export function parseKeyValueLines(text: string): Record<string, string> | null {
  const result: Record<string, string> = {};
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const eq = line.indexOf('=');
    if (eq <= 0) return null;
    result[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return result;
}

/** Kafka principals look like "User:alice" or "Group:ops". */
export function principalError(principal: string): string | null {
  return /^[A-Za-z]+:.+$/.test(principal.trim()) ? null : 'Use the form Type:name, for example User:alice';
}
