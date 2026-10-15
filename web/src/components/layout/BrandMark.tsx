// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Group, Stack, Text, ThemeIcon } from '@mantine/core';
import { IconServer2 } from '@tabler/icons-react';

/** Product name with its own mark; never a Kafka logo. */
export function BrandMark({ size = 'md' }: { size?: 'md' | 'lg' }) {
  const large = size === 'lg';
  return (
    <Group gap="xs" wrap="nowrap">
      <ThemeIcon size={large ? 44 : 32} radius="md" variant="filled">
        <IconServer2 size={large ? 26 : 20} />
      </ThemeIcon>
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
