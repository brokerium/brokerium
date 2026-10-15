// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

/** Color of a consumer group state as reported by Kafka (Stable, Empty, PreparingRebalance, ...). */
export function groupStateColor(state: string): string {
  switch (state.toLowerCase()) {
    case 'stable':
      return 'green';
    case 'empty':
      return 'gray';
    case 'preparingrebalance':
    case 'completingrebalance':
    case 'assigning':
    case 'reconciling':
      return 'yellow';
    case 'dead':
      return 'red';
    default:
      return 'blue';
  }
}

/** Offsets can only be reset while the group has no active members. */
export function canResetOffsets(state: string, members: number): boolean {
  const s = state.toLowerCase();
  return members === 0 && (s === 'empty' || s === 'dead' || s === '');
}
