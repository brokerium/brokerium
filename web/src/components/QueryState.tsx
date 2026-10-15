// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Button, Center, Group, Loader, Stack, Text } from '@mantine/core';
import { IconAlertTriangle, IconRefresh } from '@tabler/icons-react';

import { errorMessage } from '../api/errors';

export function PageLoader({ label }: { label?: string }) {
  return (
    <Center py="xl" mih={200}>
      <Stack align="center" gap="xs">
        <Loader />
        {label && (
          <Text size="sm" c="dimmed">
            {label}
          </Text>
        )}
      </Stack>
    </Center>
  );
}

export function FullPageLoader() {
  return (
    <Center h="100vh">
      <Loader />
    </Center>
  );
}

interface QueryErrorProps {
  error: unknown;
  title?: string;
  onRetry?: () => void;
}

/** Inline error for a failed initial load. */
export function QueryError({ error, title = 'Could not load data', onRetry }: QueryErrorProps) {
  return (
    <Alert color="red" icon={<IconAlertTriangle size={18} />} title={title} variant="light">
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Text size="sm">{errorMessage(error)}</Text>
        {onRetry && (
          <Button size="xs" variant="light" color="red" leftSection={<IconRefresh size={14} />} onClick={onRetry}>
            Retry
          </Button>
        )}
      </Group>
    </Alert>
  );
}
