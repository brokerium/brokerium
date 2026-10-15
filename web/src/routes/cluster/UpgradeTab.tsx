// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Badge, Group, SimpleGrid, Stack, Table, Text, TextInput } from '@mantine/core';
import { IconAlertOctagon, IconAlertTriangle, IconArrowBigUpLines, IconFlagCheck } from '@tabler/icons-react';
import { useState } from 'react';

import { useNodes } from '../../api/nodes';
import { FeaturesTable } from '../../components/FeaturesTable';
import { GatedButton } from '../../components/Gated';
import { usePlanner } from '../../components/operations/plannerContext';
import { PageLoader, QueryError } from '../../components/QueryState';
import { RelativeTime } from '../../components/RelativeTime';
import { StatCard } from '../../components/StatCard';
import { affectedByKafka20979, KAFKA_20979_WARNING, parseKafkaVersion } from '../../lib/kafkaVersion';
import { isDynamicQuorum } from '../../lib/quorum';
import { useClusterContext } from './clusterContext';
import { Section, TabNotAvailable } from './common';

function AgentNodes({ clusterId }: { clusterId: string }) {
  const nodes = useNodes(clusterId);
  if (nodes.isPending) return <PageLoader />;
  if (nodes.error && !nodes.data) return <QueryError error={nodes.error} onRetry={() => void nodes.refetch()} />;
  const list = [...(nodes.data ?? [])].sort((a, b) => (a.kafkaNodeId ?? 0) - (b.kafkaNodeId ?? 0));
  if (list.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No agent nodes are attached to this cluster. Versions of nodes without brokerium-agent are not known.
      </Text>
    );
  }
  return (
    <Table.ScrollContainer minWidth={760}>
      <Table striped verticalSpacing="xs">
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Node</Table.Th>
            <Table.Th>Role</Table.Th>
            <Table.Th>Kafka node id</Table.Th>
            <Table.Th>Active version</Table.Th>
            <Table.Th>Installed versions</Table.Th>
            <Table.Th>Kafka</Table.Th>
            <Table.Th>Agent</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {list.map((n) => {
            const version = n.status.activeVersion ?? '';
            return (
              <Table.Tr key={n.id}>
                <Table.Td>
                  <Text size="sm" fw={600}>
                    {n.name}
                  </Text>
                  <Text size="xs" c="dimmed">
                    {n.hostname}
                  </Text>
                </Table.Td>
                <Table.Td>{n.role || '—'}</Table.Td>
                <Table.Td>{n.kafkaNodeId ?? '—'}</Table.Td>
                <Table.Td>
                  <Group gap={6}>
                    <Text size="sm" ff="monospace">
                      {version || '—'}
                    </Text>
                    {affectedByKafka20979(version) && (
                      <Badge size="xs" color="orange" variant="light" title="Affected by KAFKA-20979">
                        KAFKA-20979
                      </Badge>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {(n.status.installedVersions ?? []).join(', ') || '—'}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Badge size="sm" variant="light" color={n.status.running ? 'green' : 'gray'}>
                    {n.status.running ? 'running' : 'stopped'}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap={6}>
                    <Badge size="sm" variant="dot" color={n.connected ? 'green' : 'red'}>
                      {n.connected ? 'connected' : 'offline'}
                    </Badge>
                    <RelativeTime value={n.lastSeenAt} size="xs" c="dimmed" />
                  </Group>
                </Table.Td>
              </Table.Tr>
            );
          })}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

export default function UpgradeTab() {
  const { clusterId, caps, view, snapshot } = useClusterContext();
  const planner = usePlanner();
  const [version, setVersion] = useState('4.4.0');

  if (!caps.upgradeVersion && !caps.finalizeFeatures) return <TabNotAvailable what="Version management" />;

  const h = view.health;
  const parsed = parseKafkaVersion(version);
  const risky = parsed !== null && affectedByKafka20979(version);
  const metadataFeature = snapshot?.features.find((f) => f.name === 'metadata.version');
  const finalizable = (snapshot?.features ?? []).some((f) => f.finalized < f.supportedMax);
  const upgradeAvailability = planner.availability('rolling-upgrade');
  const finalizeAvailability = planner.availability('finalize-upgrade');

  return (
    <Stack gap="md">
      <SimpleGrid cols={{ base: 2, md: 4 }} spacing="sm">
        <StatCard label="metadata.version" value={h.metadataVersion || '—'} hint={metadataFeature ? `level ${metadataFeature.finalized}` : undefined} />
        <StatCard label="kraft.version" value={h.kraftVersion} hint={isDynamicQuorum(h.kraftVersion) ? 'dynamic quorum' : 'static quorum'} />
        <StatCard label="Brokers" value={`${h.brokersAlive}/${h.brokers}`} />
        <StatCard label="Controllers" value={h.voters || '—'} />
      </SimpleGrid>

      <Alert color="orange" variant="light" icon={<IconAlertTriangle size={18} />} title="Known issue KAFKA-20979">
        {KAFKA_20979_WARNING}
      </Alert>

      <Section title="Agent nodes" description="Kafka versions reported by brokerium-agent on the nodes of this cluster">
        <AgentNodes clusterId={clusterId} />
      </Section>

      <Section
        title="Rolling upgrade"
        description="Installs the target version and restarts nodes one at a time behind the health gate. metadata.version stays unchanged until you finalize, so the upgrade can still be rolled back."
      >
        <Group align="flex-end" gap="sm">
          <TextInput
            label="Target Kafka version"
            value={version}
            onChange={(e) => setVersion(e.currentTarget.value)}
            error={version && !parsed ? 'Use a release version such as 4.4.0' : undefined}
            w={220}
            styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
          />
          <GatedButton
            availability={upgradeAvailability}
            leftSection={<IconArrowBigUpLines size={16} />}
            disabled={!parsed}
            loading={planner.pendingKey === 'rolling-upgrade'}
            onClick={() => void planner.plan('rolling-upgrade', { version: version.trim() })}
          >
            Plan rolling upgrade
          </GatedButton>
        </Group>
        {risky && (
          <Alert color="red" icon={<IconAlertTriangle size={18} />} mt="sm">
            {version} is affected by KAFKA-20979. Choose 4.3.2 or later, or 4.4.0.
          </Alert>
        )}
      </Section>

      <Section
        title="Finalize upgrade"
        description="Raises metadata.version and the other features to the highest level supported by every node."
        actions={
          <GatedButton
            availability={finalizeAvailability}
            color="red"
            leftSection={<IconFlagCheck size={16} />}
            loading={planner.pendingKey === 'finalize-upgrade'}
            onClick={() => void planner.plan('finalize-upgrade', {})}
          >
            Plan finalization
          </GatedButton>
        }
      >
        <Stack gap="sm">
          <Alert color="red" icon={<IconAlertOctagon size={18} />} variant="light">
            Finalizing metadata.version cannot be undone: after it the cluster can no longer be downgraded to the previous
            Kafka version. Finalize only when every node runs the new version and the cluster is healthy. Confirmation
            requires typing the cluster name.
          </Alert>
          {!finalizable && snapshot && (
            <Text size="sm" c="dimmed">
              All features are already at the highest supported level.
            </Text>
          )}
          <FeaturesTable features={snapshot?.features ?? []} />
        </Stack>
      </Section>
    </Stack>
  );
}
