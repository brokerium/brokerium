// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Button, EmptyState } from '@mantine/core';
import { IconMapOff } from '@tabler/icons-react';
import { Link } from 'react-router';

export default function NotFoundPage() {
  return (
    <EmptyState
      mt="xl"
      icon={<IconMapOff size={28} />}
      title="Page not found"
      description="The page you are looking for does not exist."
    >
      <EmptyState.Actions>
        <Button component={Link} to="/">
          Go to clusters
        </Button>
      </EmptyState.Actions>
    </EmptyState>
  );
}
