// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { useNow } from '../hooks/useNow';
import { durationBetween, EMPTY, formatDuration, type TimeInput, toEpochMs } from '../lib/format';

/** Duration from start to end, or to now while the end is unknown. */
export function LiveDuration({ start, end }: { start: TimeInput; end?: TimeInput }) {
  const now = useNow();
  if (toEpochMs(start) === null) return <>{EMPTY}</>;
  return <>{formatDuration(durationBetween(start, end, now))}</>;
}
