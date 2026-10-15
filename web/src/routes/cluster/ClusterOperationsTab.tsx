// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { EmptyState, Paper, Stack, Text } from '@mantine/core';
import { IconListDetails } from '@tabler/icons-react';

import { useClusterOperations } from '../../api/operations';
import { OperationsTable } from '../../components/operations/OperationsTable';
import { PageLoader, QueryError } from '../../components/QueryState';
import { useClusterContext } from './clusterContext';

export default function ClusterOperationsTab() {
  const { clusterId } = useClusterContext();
  const operations = useClusterOperations(clusterId);

  if (operations.isPending) return <PageLoader />;
  if (operations.error && !operations.data) {
    return <QueryError error={operations.error} onRetry={() => void operations.refetch()} />;
  }
  const list = operations.data ?? [];

  return (
    <Stack gap="sm">
      <Text size="sm" c="dimmed">
        One operation runs per cluster at a time. Start new operations from the Brokers, Controllers, Distribution and
        Upgrade tabs.
      </Text>
      <Paper withBorder radius="md">
        {list.length === 0 ? (
          <EmptyState
            py="xl"
            icon={<IconListDetails size={28} />}
            title="No operations yet"
            description="Planned, running and finished operations of this cluster appear here."
          />
        ) : (
          <OperationsTable operations={list} />
        )}
      </Paper>
    </Stack>
  );
}
