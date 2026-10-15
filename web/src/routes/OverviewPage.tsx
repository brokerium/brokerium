// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import {
  Anchor,
  Badge,
  Button,
  EmptyState,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { IconPlus, IconSearch, IconServer2 } from '@tabler/icons-react';
import { useMemo, useState } from 'react';
import { Link } from 'react-router';

import { useOverview } from '../api/clusters';
import type { ClusterSummary, Health } from '../api/types';
import { FlavorBadge, HealthBadge, ProviderBadge } from '../components/badges';
import { AddClusterModal } from '../components/clusters/AddClusterModal';
import { PageHeader } from '../components/PageHeader';
import { PageLoader, QueryError } from '../components/QueryState';
import { RelativeTime } from '../components/RelativeTime';
import { StatCard } from '../components/StatCard';
import { usePermissions } from '../hooks/usePermissions';
import { formatNumber } from '../lib/format';
import { bySeverity, severityColor } from '../lib/status';

function topIssue(summary: ClusterSummary): { text: string; color: string } | null {
  if (summary.error) return { text: summary.error, color: 'red' };
  const issue = [...(summary.health.issues ?? [])].sort(bySeverity)[0];
  return issue ? { text: issue.message, color: severityColor(issue.severity) } : null;
}

function Count({ value, danger }: { value: number; danger?: boolean }) {
  return (
    <Text size="sm" fw={value > 0 && danger ? 700 : undefined} c={value > 0 && danger ? 'red' : undefined}>
      {formatNumber(value)}
    </Text>
  );
}

function Brokers({ health }: { health: Health }) {
  const down = health.brokers - health.brokersAlive;
  return (
    <Group gap={6} wrap="nowrap">
      <Text size="sm" c={down > 0 ? 'red' : undefined} fw={down > 0 ? 700 : undefined}>
        {health.brokersAlive}/{health.brokers}
      </Text>
      {health.brokersFenced > 0 && (
        <Badge size="xs" color="red" variant="light">
          {health.brokersFenced} fenced
        </Badge>
      )}
      {health.brokersCordoned > 0 && (
        <Badge size="xs" color="yellow" variant="light">
          {health.brokersCordoned} cordoned
        </Badge>
      )}
    </Group>
  );
}

function Controllers({ health }: { health: Health }) {
  if (health.voters === 0) return <Text size="sm" c="dimmed">—</Text>;
  return (
    <Stack gap={0}>
      <Text size="sm">
        {health.voters} {health.voters === 1 ? 'voter' : 'voters'}
        {health.votersLagging > 0 && (
          <Text span c="yellow.7" fw={600}>
            {' '}
            · {health.votersLagging} lagging
          </Text>
        )}
      </Text>
      <Text size="xs" c={health.quorumLeader < 0 ? 'red' : 'dimmed'}>
        {health.quorumLeader < 0 ? 'no leader' : `leader ${health.quorumLeader}`}
      </Text>
    </Stack>
  );
}

export default function OverviewPage() {
  const { data, error, isPending, refetch, dataUpdatedAt } = useOverview();
  const { canOperate } = usePermissions();
  const [adding, { open, close }] = useDisclosure(false);
  const [search, setSearch] = useState('');

  const clusters = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (data ?? [])
      .filter((c) => !q || c.cluster.name.toLowerCase().includes(q) || c.cluster.kafkaClusterId.toLowerCase().includes(q))
      .sort((a, b) => a.cluster.name.localeCompare(b.cluster.name));
  }, [data, search]);

  const counts = useMemo(() => {
    const all = data ?? [];
    return {
      total: all.length,
      ok: all.filter((c) => c.health.status === 'ok').length,
      warning: all.filter((c) => c.health.status === 'warning').length,
      critical: all.filter((c) => c.health.status === 'critical' || c.health.status === 'unreachable').length,
    };
  }, [data]);

  const addButton = canOperate && (
    <Button leftSection={<IconPlus size={16} />} onClick={open}>
      Add cluster
    </Button>
  );

  return (
    <>
      <PageHeader
        title="Clusters"
        description={
          dataUpdatedAt ? (
            <>
              Refreshed every 10 s · updated <RelativeTime value={dataUpdatedAt} size="sm" c="dimmed" />
            </>
          ) : (
            'All registered clusters'
          )
        }
        actions={addButton}
      />
      <AddClusterModal opened={adding} onClose={close} />

      {isPending && <PageLoader />}
      {error && !data && <QueryError error={error} onRetry={() => void refetch()} />}

      {data && data.length === 0 && (
        <Paper withBorder p="xl" radius="md">
          <EmptyState
            icon={<IconServer2 size={28} />}
            title="No clusters yet"
            description="Register an existing Kafka cluster by its bootstrap servers, or connect nodes with brokerium-agent."
          >
            {addButton && <EmptyState.Actions>{addButton}</EmptyState.Actions>}
          </EmptyState>
        </Paper>
      )}

      {data && data.length > 0 && (
        <Stack gap="md">
          <SimpleGrid cols={{ base: 2, md: 4 }} spacing="sm">
            <StatCard label="Clusters" value={counts.total} />
            <StatCard label="Healthy" value={counts.ok} color={counts.ok > 0 ? 'green' : undefined} />
            <StatCard label="Warnings" value={counts.warning} color={counts.warning > 0 ? 'yellow.7' : undefined} />
            <StatCard
              label="Critical or unreachable"
              value={counts.critical}
              color={counts.critical > 0 ? 'red' : undefined}
            />
          </SimpleGrid>

          <Group>
            <TextInput
              placeholder="Search by name or cluster ID"
              leftSection={<IconSearch size={14} />}
              value={search}
              onChange={(e) => setSearch(e.currentTarget.value)}
              w={320}
            />
          </Group>

          <Paper withBorder radius="md">
            <Table.ScrollContainer minWidth={1200}>
              <Table highlightOnHover verticalSpacing="sm">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Cluster</Table.Th>
                    <Table.Th>Type</Table.Th>
                    <Table.Th>Health</Table.Th>
                    <Table.Th>Brokers</Table.Th>
                    <Table.Th>Controllers</Table.Th>
                    <Table.Th>metadata.version</Table.Th>
                    <Table.Th>Partitions</Table.Th>
                    <Table.Th>Under-replicated</Table.Th>
                    <Table.Th>Offline</Table.Th>
                    <Table.Th>Under min ISR</Table.Th>
                    <Table.Th>Top issue</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {clusters.map((summary) => {
                    const { cluster, health } = summary;
                    const issue = topIssue(summary);
                    return (
                      <Table.Tr key={cluster.id}>
                        <Table.Td>
                          <Stack gap={0}>
                            <Anchor component={Link} to={`/clusters/${encodeURIComponent(cluster.id)}`} fw={600}>
                              {cluster.name}
                            </Anchor>
                            <Text size="xs" c="dimmed" ff="monospace">
                              {cluster.kafkaClusterId || cluster.bootstrapServers[0]}
                            </Text>
                          </Stack>
                        </Table.Td>
                        <Table.Td>
                          <Group gap={4} wrap="nowrap">
                            <ProviderBadge provider={cluster.provider} />
                            <FlavorBadge flavor={cluster.flavor} />
                          </Group>
                        </Table.Td>
                        <Table.Td>
                          <HealthBadge status={health.status} />
                        </Table.Td>
                        <Table.Td>
                          <Brokers health={health} />
                        </Table.Td>
                        <Table.Td>
                          <Controllers health={health} />
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" ff="monospace">
                            {health.metadataVersion || '—'}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Count value={health.partitions} />
                        </Table.Td>
                        <Table.Td>
                          <Count value={health.underReplicated} danger />
                        </Table.Td>
                        <Table.Td>
                          <Count value={health.offlinePartitions} danger />
                        </Table.Td>
                        <Table.Td>
                          <Count value={health.underMinIsr} danger />
                        </Table.Td>
                        <Table.Td maw={320}>
                          {issue ? (
                            <Tooltip label={issue.text} multiline maw={400} withArrow openDelay={300}>
                              <Text size="sm" c={issue.color === 'yellow' ? 'yellow.8' : issue.color} lineClamp={2}>
                                {issue.text}
                              </Text>
                            </Tooltip>
                          ) : (
                            <Text size="sm" c="dimmed">
                              No issues
                            </Text>
                          )}
                        </Table.Td>
                      </Table.Tr>
                    );
                  })}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </Paper>
        </Stack>
      )}
    </>
  );
}
