// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Anchor, Group, Table, Text } from '@mantine/core';
import { Link, useNavigate } from 'react-router';

import { useOverview } from '../../api/clusters';
import { operationTitle, useOperationTypes } from '../../api/operations';
import type { Operation } from '../../api/types';
import { IrreversibleBadge, OperationStatusBadge } from '../badges';
import { LiveDuration } from '../LiveDuration';
import { RelativeTime } from '../RelativeTime';

interface OperationsTableProps {
  operations: Operation[];
  /** Show the cluster column (all-clusters list). */
  withCluster?: boolean;
}

export function OperationsTable({ operations, withCluster = false }: OperationsTableProps) {
  const navigate = useNavigate();
  const { data: types } = useOperationTypes();
  const { data: clusters } = useOverview();
  const clusterName = (id: string) => clusters?.find((c) => c.cluster.id === id)?.cluster.name ?? id;

  return (
    <Table.ScrollContainer minWidth={900}>
      <Table highlightOnHover verticalSpacing="xs">
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Operation</Table.Th>
            {withCluster && <Table.Th>Cluster</Table.Th>}
            <Table.Th>Status</Table.Th>
            <Table.Th>Summary</Table.Th>
            <Table.Th>Created by</Table.Th>
            <Table.Th>Created</Table.Th>
            <Table.Th>Duration</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {operations.map((op) => {
            const to = `/operations/${encodeURIComponent(op.id)}`;
            return (
              <Table.Tr key={op.id} style={{ cursor: 'pointer' }} onClick={() => navigate(to)}>
                <Table.Td>
                  <Group gap={6} wrap="nowrap">
                    <Anchor component={Link} to={to} size="sm" fw={600} onClick={(e) => e.stopPropagation()}>
                      {operationTitle(op.type, types)}
                    </Anchor>
                    {op.irreversible && <IrreversibleBadge size="xs" />}
                  </Group>
                </Table.Td>
                {withCluster && (
                  <Table.Td>
                    <Anchor
                      component={Link}
                      to={`/clusters/${encodeURIComponent(op.clusterId)}`}
                      size="sm"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {clusterName(op.clusterId)}
                    </Anchor>
                  </Table.Td>
                )}
                <Table.Td>
                  <OperationStatusBadge status={op.status} />
                </Table.Td>
                <Table.Td maw={420}>
                  <Text size="sm" lineClamp={2} c={op.error ? 'red' : undefined}>
                    {op.error || op.summary || op.plan?.summary || '—'}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{op.createdBy || '—'}</Text>
                </Table.Td>
                <Table.Td>
                  <RelativeTime value={op.createdAt} />
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{op.startedAt ? <LiveDuration start={op.startedAt} end={op.finishedAt} /> : '—'}</Text>
                </Table.Td>
              </Table.Tr>
            );
          })}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
