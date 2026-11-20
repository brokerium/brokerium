// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

import { Group, Progress, SimpleGrid, Table, Text } from '@mantine/core';

import { useRates } from '../../api/clusters';
import type { Snapshot } from '../../api/types';
import { diskUsedPercent } from '../../lib/distribution';
import { formatBytes, formatBytesPerSecond, formatDuration, formatPercent, formatRate } from '../../lib/format';
import { diskUsageColor } from '../../lib/status';
import { NoSnapshot, Section } from '../../routes/cluster/common';

const TOP_TOPICS = 10;

interface CurrentValuesProps {
  clusterId: string;
  snapshot: Snapshot | undefined;
}

/** The rates Brokerium estimates now and the disk usage of the latest snapshot, without Prometheus. */
export function CurrentValues({ clusterId, snapshot }: CurrentValuesProps) {
  const rates = useRates(clusterId);
  if (!snapshot) return <NoSnapshot />;
  const brokers = [...snapshot.brokers].sort((a, b) => a.id - b.id);
  const topics = Object.entries(rates.data?.topics ?? {})
    .sort(([, a], [, b]) => b.bytesInPerSec - a.bytesInPerSec || b.messagesInPerSec - a.messagesInPerSec)
    .slice(0, TOP_TOPICS);
  const window = rates.data?.windowSeconds
    ? `Estimated from offsets and log sizes over the last ${formatDuration(rates.data.windowSeconds * 1000)}`
    : 'Rates appear after two snapshots';

  return (
    <SimpleGrid cols={{ base: 1, xl: 2 }} spacing="md">
      <Section title="Brokers now" description={`${window}; a broker counts the partitions it holds a replica of`}>
        <Table.ScrollContainer minWidth={520}>
          <Table verticalSpacing={6}>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Broker</Table.Th>
                <Table.Th>Bytes in</Table.Th>
                <Table.Th>Messages in</Table.Th>
                <Table.Th>Disk</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {brokers.map((b) => {
                const rate = rates.data?.brokers[String(b.id)];
                const percent = diskUsedPercent(b);
                return (
                  <Table.Tr key={b.id}>
                    <Table.Td>
                      <Text size="sm" fw={600}>
                        {b.id}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {b.host}
                      </Text>
                    </Table.Td>
                    <Table.Td>{formatBytesPerSecond(rate?.bytesInPerSec)}</Table.Td>
                    <Table.Td>{formatRate(rate?.messagesInPerSec, ' msg/s')}</Table.Td>
                    <Table.Td>
                      {percent === null ? (
                        <Text size="sm" c="dimmed">
                          {formatBytes(b.dataBytes)} data
                        </Text>
                      ) : (
                        <Group gap={6} wrap="nowrap" w={200}>
                          <Progress value={percent} color={diskUsageColor(percent)} size="md" style={{ flex: 1 }} />
                          <Text size="xs" w={110} ta="right">
                            {formatBytes(b.diskTotalBytes - b.diskUsableBytes)} · {formatPercent(percent)}
                          </Text>
                        </Group>
                      )}
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Section>
      <Section title="Busiest topics now" description={window}>
        {topics.length === 0 ? (
          <Text size="sm" c="dimmed">
            No traffic measured yet.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={420}>
            <Table verticalSpacing={6}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Topic</Table.Th>
                  <Table.Th>Bytes in</Table.Th>
                  <Table.Th>Messages in</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {topics.map(([name, rate]) => (
                  <Table.Tr key={name}>
                    <Table.Td>
                      <Text size="sm" ff="monospace">
                        {name}
                      </Text>
                    </Table.Td>
                    <Table.Td>{formatBytesPerSecond(rate.bytesInPerSec)}</Table.Td>
                    <Table.Td>{formatRate(rate.messagesInPerSec, ' msg/s')}</Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Section>
    </SimpleGrid>
  );
}
