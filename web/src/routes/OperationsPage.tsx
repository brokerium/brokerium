// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { EmptyState, Group, MultiSelect, Paper, Select, Stack } from '@mantine/core';
import { IconListDetails } from '@tabler/icons-react';
import { useMemo, useState } from 'react';

import { useOverview } from '../api/clusters';
import { operationTitle, useOperations, useOperationTypes } from '../api/operations';
import type { OperationStatus } from '../api/types';
import { OperationsTable } from '../components/operations/OperationsTable';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';

const STATUSES: OperationStatus[] = ['planned', 'pending', 'running', 'paused', 'succeeded', 'failed', 'cancelled'];

export default function OperationsPage() {
  const operations = useOperations(100);
  const { data: clusters } = useOverview();
  const { data: types } = useOperationTypes();
  const [statuses, setStatuses] = useState<string[]>([]);
  const [clusterId, setClusterId] = useState<string | null>(null);
  const [type, setType] = useState<string | null>(null);

  const list = useMemo(
    () =>
      (operations.data ?? [])
        .filter(
          (op) =>
            (statuses.length === 0 || statuses.includes(op.status)) &&
            (!clusterId || op.clusterId === clusterId) &&
            (!type || op.type === type),
        )
        .sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
    [operations.data, statuses, clusterId, type],
  );
  const typeOptions = [...new Set((operations.data ?? []).map((op) => op.type))].map((t) => ({
    value: t,
    label: operationTitle(t, types),
  }));

  return (
    <>
      <PageHeader title="Operations" description="Latest 100 operations across all clusters" />
      <Stack gap="md">
        <Group gap="sm">
          <MultiSelect
            placeholder={statuses.length === 0 ? 'All statuses' : undefined}
            data={STATUSES}
            value={statuses}
            onChange={setStatuses}
            clearable
            w={320}
          />
          <Select
            placeholder="All clusters"
            data={(clusters ?? []).map((c) => ({ value: c.cluster.id, label: c.cluster.name }))}
            value={clusterId}
            onChange={setClusterId}
            clearable
            searchable
            w={220}
          />
          <Select placeholder="All types" data={typeOptions} value={type} onChange={setType} clearable w={220} />
        </Group>
        {operations.isPending && <PageLoader />}
        {operations.error && !operations.data && (
          <QueryError error={operations.error} onRetry={() => void operations.refetch()} />
        )}
        {operations.data && (
          <Paper withBorder radius="md">
            {list.length === 0 ? (
              <EmptyState
                py="xl"
                icon={<IconListDetails size={28} />}
                title="No operations"
                description="Operations such as rebalances, drains and upgrades are planned from a cluster page."
              />
            ) : (
              <OperationsTable operations={list} withCluster />
            )}
          </Paper>
        )}
      </Stack>
    </>
  );
}
