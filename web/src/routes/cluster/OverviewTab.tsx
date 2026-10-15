// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Anchor, Group, SimpleGrid, Stack, Table, Text, ThemeIcon } from '@mantine/core';
import { IconAlertTriangle, IconCircleCheck } from '@tabler/icons-react';
import { Link } from 'react-router';

import { SeverityBadge } from '../../components/badges';
import { FeaturesTable } from '../../components/FeaturesTable';
import { ReplicaList } from '../../components/ReplicaList';
import { StatCard } from '../../components/StatCard';
import { formatNumber, formatPercent } from '../../lib/format';
import { faultTolerance, isDynamicQuorum } from '../../lib/quorum';
import { bySeverity } from '../../lib/status';
import { useClusterContext } from './clusterContext';
import { Section } from './common';

export default function OverviewTab() {
  const { view, snapshot, clusterId } = useClusterContext();
  const h = view.health;
  const issues = [...(h.issues ?? [])].sort(bySeverity);
  const quorum = snapshot?.quorum;
  const base = `/clusters/${encodeURIComponent(clusterId)}`;

  return (
    <Stack gap="md">
      <SimpleGrid cols={{ base: 2, sm: 3, lg: 6 }} spacing="sm">
        <StatCard
          label="Brokers alive"
          value={`${h.brokersAlive}/${h.brokers}`}
          color={h.brokersAlive < h.brokers ? 'red' : undefined}
          hint={[h.brokersFenced > 0 && `${h.brokersFenced} fenced`, h.brokersCordoned > 0 && `${h.brokersCordoned} cordoned`]
            .filter(Boolean)
            .join(' · ') || undefined}
        />
        <StatCard label="Topics" value={formatNumber(h.topics)} hint={`${formatNumber(h.partitions)} partitions`} />
        <StatCard
          label="Under-replicated"
          value={formatNumber(h.underReplicated)}
          color={h.underReplicated > 0 ? 'yellow.7' : undefined}
          hint={h.reassigning > 0 ? `${h.reassigning} reassigning` : undefined}
        />
        <StatCard label="Offline" value={formatNumber(h.offlinePartitions)} color={h.offlinePartitions > 0 ? 'red' : undefined} />
        <StatCard
          label="Under min ISR"
          value={formatNumber(h.underMinIsr)}
          color={h.underMinIsr > 0 ? 'red' : undefined}
          hint={`${formatNumber(h.atMinIsr)} at min ISR`}
        />
        <StatCard
          label="Max disk usage"
          value={h.diskUsedPercentMax > 0 ? formatPercent(h.diskUsedPercentMax) : '—'}
          color={h.diskUsedPercentMax >= 90 ? 'red' : h.diskUsedPercentMax >= 75 ? 'yellow.7' : undefined}
        />
      </SimpleGrid>

      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="md">
        <Section title="Health" description="Evaluated on every snapshot">
          {issues.length === 0 ? (
            <Group gap="xs">
              <ThemeIcon color="green" variant="light" radius="xl" size="sm">
                <IconCircleCheck size={14} />
              </ThemeIcon>
              <Text size="sm">No issues detected.</Text>
            </Group>
          ) : (
            <Stack gap="xs">
              {issues.map((issue) => (
                <Group key={`${issue.code}-${issue.message}`} gap="sm" wrap="nowrap" align="flex-start">
                  <SeverityBadge severity={issue.severity} />
                  <Stack gap={0}>
                    <Text size="sm">{issue.message}</Text>
                    <Text size="xs" c="dimmed" ff="monospace">
                      {issue.code}
                    </Text>
                  </Stack>
                </Group>
              ))}
            </Stack>
          )}
        </Section>

        <Section
          title="Controller quorum"
          actions={
            quorum ? (
              <Anchor component={Link} to={`${base}/controllers`} size="sm">
                Details
              </Anchor>
            ) : undefined
          }
        >
          {quorum ? (
            <SimpleGrid cols={{ base: 2, sm: 3 }} spacing="sm">
              <StatCard label="Leader" value={quorum.leaderId < 0 ? 'none' : quorum.leaderId} color={quorum.leaderId < 0 ? 'red' : undefined} />
              <StatCard label="Epoch" value={formatNumber(quorum.leaderEpoch)} />
              <StatCard label="High watermark" value={formatNumber(quorum.highWatermark)} />
              <StatCard
                label="Voters"
                value={quorum.voters.length}
                hint={`tolerates ${faultTolerance(quorum.voters.length)} failure${faultTolerance(quorum.voters.length) === 1 ? '' : 's'}`}
              />
              <StatCard
                label="Lagging voters"
                value={h.votersLagging}
                color={h.votersLagging > 0 ? 'yellow.7' : undefined}
              />
              <StatCard
                label="kraft.version"
                value={h.kraftVersion}
                hint={isDynamicQuorum(h.kraftVersion) ? 'dynamic quorum' : 'static quorum'}
              />
            </SimpleGrid>
          ) : (
            <Text size="sm" c="dimmed">
              The quorum is not described for this cluster.
            </Text>
          )}
        </Section>
      </SimpleGrid>

      <Section title="Features" description={`metadata.version ${h.metadataVersion || 'unknown'}`}>
        <FeaturesTable features={snapshot?.features ?? []} />
      </Section>

      {snapshot && snapshot.reassignments.length > 0 && (
        <Section title="Ongoing reassignments" description={`${snapshot.reassignments.length} partitions are moving`}>
          <Table.ScrollContainer minWidth={560}>
            <Table striped verticalSpacing={6}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Topic</Table.Th>
                  <Table.Th>Partition</Table.Th>
                  <Table.Th>Replicas</Table.Th>
                  <Table.Th>Adding</Table.Th>
                  <Table.Th>Removing</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {snapshot.reassignments.map((r) => (
                  <Table.Tr key={`${r.topic}-${r.partition}`}>
                    <Table.Td>
                      <Text size="sm" ff="monospace">
                        {r.topic}
                      </Text>
                    </Table.Td>
                    <Table.Td>{r.partition}</Table.Td>
                    <Table.Td>
                      <ReplicaList replicas={r.replicas} added={r.adding ?? []} />
                    </Table.Td>
                    <Table.Td>{(r.adding ?? []).join(', ') || '—'}</Table.Td>
                    <Table.Td>{(r.removing ?? []).join(', ') || '—'}</Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Section>
      )}

      {snapshot?.errors && snapshot.errors.length > 0 && (
        <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="Snapshot errors">
          <Stack gap={4}>
            {snapshot.errors.map((e) => (
              <Text key={e} size="sm" ff="monospace">
                {e}
              </Text>
            ))}
          </Stack>
        </Alert>
      )}

      {snapshot && (
        <Text size="xs" c="dimmed">
          Snapshot collected in {snapshot.durationMs} ms.
        </Text>
      )}
    </Stack>
  );
}
