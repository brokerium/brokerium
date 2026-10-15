// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Button, Container, Group, Stack, Text } from '@mantine/core';
import { IconAlertTriangle, IconRefresh } from '@tabler/icons-react';
import { isRouteErrorResponse, Link, useRouteError } from 'react-router';

function describe(error: unknown): { title: string; message: string; reload: boolean } {
  if (isRouteErrorResponse(error)) {
    return { title: `${error.status} ${error.statusText}`, message: String(error.data ?? ''), reload: false };
  }
  const message = error instanceof Error ? error.message : String(error);
  // A lazy chunk that no longer exists after an upgrade of the server.
  if (/dynamically imported module|Importing a module script failed|Failed to fetch/i.test(message)) {
    return { title: 'A new version of Brokerium is available', message: 'Reload the page to continue.', reload: true };
  }
  return { title: 'Something went wrong', message, reload: true };
}

export function RouteError() {
  const error = useRouteError();
  const { title, message, reload } = describe(error);
  return (
    <Container size="sm" py="xl">
      <Alert color="red" icon={<IconAlertTriangle size={20} />} title={title}>
        <Stack gap="sm">
          <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
            {message}
          </Text>
          <Group gap="xs">
            {reload && (
              <Button size="xs" color="red" leftSection={<IconRefresh size={14} />} onClick={() => window.location.reload()}>
                Reload
              </Button>
            )}
            <Button size="xs" variant="default" component={Link} to="/">
              Go to clusters
            </Button>
          </Group>
        </Stack>
      </Alert>
    </Container>
  );
}
