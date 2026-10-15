// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Text, type TextProps, Tooltip } from '@mantine/core';

import { useNow } from '../hooks/useNow';
import { EMPTY, formatDateTime, formatRelativeTime, type TimeInput, toEpochMs } from '../lib/format';

interface RelativeTimeProps extends TextProps {
  value: TimeInput;
  /** Text shown when the time is unknown. */
  fallback?: string;
}

/** "12 s ago" with the absolute time in a tooltip; updates every second. */
export function RelativeTime({ value, fallback = EMPTY, size = 'sm', ...props }: RelativeTimeProps) {
  const now = useNow();
  if (toEpochMs(value) === null) {
    return (
      <Text component="span" size={size} c="dimmed" {...props}>
        {fallback}
      </Text>
    );
  }
  return (
    <Tooltip label={formatDateTime(value)} openDelay={300}>
      <Text component="span" size={size} style={{ whiteSpace: 'nowrap' }} {...props}>
        {formatRelativeTime(value, now)}
      </Text>
    </Tooltip>
  );
}
