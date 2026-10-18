// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Group, Stack, Text } from '@mantine/core';
import { BrokeriumMark } from './BrokeriumMark';

/** Product name with its own mark; never a Kafka logo. */
export function BrandMark({ size = 'md' }: { size?: 'md' | 'lg' }) {
  const large = size === 'lg';
  return (
    <Group gap="xs" wrap="nowrap">
      <BrokeriumMark size={large ? 44 : 32} />
      <Stack gap={0}>
        <Text fw={800} size={large ? 'xl' : 'lg'} lh={1.1}>
          Brokerium
        </Text>
        <Text size="xs" c="dimmed" lh={1.1}>
          for Apache Kafka
        </Text>
      </Stack>
    </Group>
  );
}
