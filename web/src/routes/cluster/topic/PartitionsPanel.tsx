// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Badge, Group, Paper, Table, Text, Tooltip } from '@mantine/core';

import type { TopicDetail } from '../../../api/types';
import { ReplicaList } from '../../../components/ReplicaList';
import { isUnderReplicated, outOfSyncReplicas } from '../../../lib/distribution';
import { formatBytes, formatNumber } from '../../../lib/format';

export function PartitionsPanel({ detail }: { detail: TopicDetail }) {
  const partitions = [...detail.topic.partitions].sort((a, b) => a.id - b.id);
  return (
    <Paper withBorder radius="md">
      <Table.ScrollContainer minWidth={960}>
        <Table striped highlightOnHover verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Partition</Table.Th>
              <Table.Th>Leader</Table.Th>
              <Table.Th>Replicas</Table.Th>
              <Table.Th>ISR</Table.Th>
              <Table.Th ta="right">Size</Table.Th>
              <Table.Th ta="right">Start offset</Table.Th>
              <Table.Th ta="right">End offset</Table.Th>
              <Table.Th ta="right">Records</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {partitions.map((p) => {
              const offsets = detail.offsets[String(p.id)] ?? {};
              const records =
                offsets.start !== undefined && offsets.end !== undefined ? Math.max(0, offsets.end - offsets.start) : undefined;
              const missing = outOfSyncReplicas(p);
              return (
                <Table.Tr key={p.id}>
                  <Table.Td>
                    <Group gap={6}>
                      <Text fw={600}>{p.id}</Text>
                      {isUnderReplicated(p) && (
                        <Tooltip label={`Not in ISR: ${missing.join(', ')}`} withArrow>
                          <Badge size="xs" color="yellow" variant="light">
                            under-replicated
                          </Badge>
                        </Tooltip>
                      )}
                      {p.error && (
                        <Badge size="xs" color="red" variant="light" title={p.error}>
                          error
                        </Badge>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    {p.leader < 0 ? (
                      <Badge color="red" size="sm">
                        offline
                      </Badge>
                    ) : (
                      <Text fw={600} size="sm">
                        {p.leader}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <ReplicaList replicas={p.replicas} isr={p.isr} offline={p.offline} leader={p.leader} />
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace">
                      {p.isr.join(', ') || '—'}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="right">{formatBytes(p.sizeBytes)}</Table.Td>
                  <Table.Td ta="right">{formatNumber(offsets.start)}</Table.Td>
                  <Table.Td ta="right">{formatNumber(offsets.end)}</Table.Td>
                  <Table.Td ta="right">{formatNumber(records)}</Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Paper>
  );
}
