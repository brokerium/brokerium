// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Paper, Text } from '@mantine/core';
import type { ReactNode } from 'react';

interface StatCardProps {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  /** Highlights the value, for example red for offline partitions. */
  color?: string;
}

export function StatCard({ label, value, hint, color }: StatCardProps) {
  return (
    <Paper withBorder p="sm" radius="md">
      <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
        {label}
      </Text>
      <Text size="xl" fw={700} c={color} mt={2}>
        {value}
      </Text>
      {hint && (
        <Text size="xs" c="dimmed" mt={2}>
          {hint}
        </Text>
      )}
    </Paper>
  );
}
