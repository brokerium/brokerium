// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

import { Alert, Badge, Group, Paper, SimpleGrid, Stack, Table, Text, Tooltip } from '@mantine/core';
import { IconArrowsTransferUp, IconCrown, IconInfoCircle, IconPlus, IconTrash } from '@tabler/icons-react';

import type { QuorumReplica } from '../../api/types';
import { CopyText } from '../../components/CopyText';
import { GatedButton } from '../../components/Gated';
import { usePlanner } from '../../components/operations/plannerContext';
import { RelativeTime } from '../../components/RelativeTime';
import { StatCard } from '../../components/StatCard';
import type { Availability } from '../../lib/capabilities';
import { formatNumber } from '../../lib/format';
import { canRemoveVoter, classifyObservers, faultTolerance, isDynamicQuorum, kraftVersionOf, quorumHint, quorumKindLabel } from '../../lib/quorum';
import { useClusterContext } from './clusterContext';
import { NoSnapshot, Section, TabNotAvailable } from './common';

function shortDirectoryId(id: string): string {
  return id.length > 12 ? `${id.slice(0, 8)}…` : id;
}

function LagText({ lag }: { lag: number }) {
  return (
    <Text size="sm" c={lag > 0 ? 'yellow.8' : undefined} fw={lag > 0 ? 600 : undefined}>
      {formatNumber(lag)}
    </Text>
  );
}

export default function ControllersTab() {
  const { snapshot, caps } = useClusterContext();
  const planner = usePlanner();

  if (!caps.describeQuorum) return <TabNotAvailable what="Describing the controller quorum" />;
  if (!snapshot) return <NoSnapshot />;
  const quorum = snapshot.quorum;
  if (!quorum) {
    return (
      <Alert color="gray" icon={<IconInfoCircle size={18} />}>
        The controller quorum could not be described in the last snapshot.
      </Alert>
    );
  }

  const voters = [...quorum.voters].sort((a, b) => a.nodeId - b.nodeId);
  const observers = classifyObservers(quorum.observers, snapshot.brokers);
  const kraftVersion = kraftVersionOf(snapshot.features);
  const dynamic = kraftVersion !== null && isDynamicQuorum(kraftVersion);
  const removable = canRemoveVoter(voters.length);
  const removeAvailability = (voter: QuorumReplica): Availability => {
    const base = planner.availability('remove-controller');
    if (!base.allowed) return base;
    if (!removable) return { allowed: false, reason: 'The last voter can never be removed' };
    return voter.role === 'leader'
      ? { allowed: true, reason: 'Removing the leader triggers a leader election' }
      : { allowed: true };
  };

  return (
    <Stack gap="md">
      <SimpleGrid cols={{ base: 2, sm: 3, lg: 5 }} spacing="sm">
        <StatCard
          label="Quorum leader"
          value={quorum.leaderId < 0 ? 'none' : quorum.leaderId}
          color={quorum.leaderId < 0 ? 'red' : undefined}
        />
        <StatCard label="Leader epoch" value={formatNumber(quorum.leaderEpoch)} />
        <StatCard label="High watermark" value={formatNumber(quorum.highWatermark)} />
        <StatCard
          label="Voters"
          value={voters.length}
          hint={`tolerates ${faultTolerance(voters.length)} failure${faultTolerance(voters.length) === 1 ? '' : 's'}`}
        />
        <StatCard label="kraft.version" value={kraftVersion ?? '—'} hint={quorumKindLabel(kraftVersion)} />
      </SimpleGrid>

      <Alert color="blue" variant="light" icon={<IconInfoCircle size={18} />}>
        {quorumHint(voters.length)} Change the quorum one controller at a time and add the new controller before
        removing an old one.
      </Alert>

      <Section
        title="Voters"
        actions={
          <Group gap="xs">
            <GatedButton
              availability={planner.availability('add-controller')}
              leftSection={<IconPlus size={16} />}
              loading={planner.pendingKey === 'add-controller'}
              onClick={() => void planner.plan('add-controller', {})}
            >
              Add controller
            </GatedButton>
            {!dynamic && (
              <GatedButton
                availability={planner.availability('migrate-quorum')}
                color="red"
                variant="light"
                leftSection={<IconArrowsTransferUp size={16} />}
                loading={planner.pendingKey === 'migrate-quorum'}
                onClick={() => void planner.plan('migrate-quorum', {})}
              >
                Migrate to dynamic quorum
              </GatedButton>
            )}
          </Group>
        }
      >
        <Table.ScrollContainer minWidth={900}>
          <Table verticalSpacing="sm" highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Node</Table.Th>
                <Table.Th>Directory ID</Table.Th>
                <Table.Th>Role</Table.Th>
                <Table.Th ta="right">Log end offset</Table.Th>
                <Table.Th ta="right">Lag</Table.Th>
                <Table.Th>Last caught up</Table.Th>
                <Table.Th>Last fetch</Table.Th>
                <Table.Th w={130} />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {voters.map((v) => {
                const availability = removeAvailability(v);
                return (
                  <Table.Tr key={`${v.nodeId}-${v.directoryId}`}>
                    <Table.Td>
                      <Group gap={6}>
                        <Text fw={700}>{v.nodeId}</Text>
                        {v.role === 'leader' && (
                          <Tooltip label="Quorum leader" withArrow>
                            <IconCrown size={16} color="var(--mantine-color-yellow-6)" />
                          </Tooltip>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <CopyText value={v.directoryId} display={shortDirectoryId(v.directoryId)} />
                    </Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant={v.role === 'leader' ? 'filled' : 'light'}>
                        {v.role}
                      </Badge>
                    </Table.Td>
                    <Table.Td ta="right">{formatNumber(v.logEndOffset)}</Table.Td>
                    <Table.Td ta="right">
                      <LagText lag={v.lag} />
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime value={v.lastCaughtUpTimestamp} />
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime value={v.lastFetchTimestamp} />
                    </Table.Td>
                    <Table.Td>
                      <GatedButton
                        availability={availability}
                        size="xs"
                        variant="subtle"
                        color="red"
                        leftSection={<IconTrash size={14} />}
                        loading={planner.pendingKey === `remove-${v.nodeId}`}
                        onClick={() =>
                          void planner.plan('remove-controller', { nodeId: v.nodeId }, { key: `remove-${v.nodeId}` })
                        }
                      >
                        Remove voter
                      </GatedButton>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Section>

      <Section title="Observers" description="Brokers replicate the metadata log as observers; controllers appear here until they become voters.">
        {observers.length === 0 ? (
          <Text size="sm" c="dimmed">
            No observers.
          </Text>
        ) : (
          <Table.ScrollContainer minWidth={760}>
            <Table verticalSpacing="sm" highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Node</Table.Th>
                  <Table.Th>Type</Table.Th>
                  <Table.Th>Directory ID</Table.Th>
                  <Table.Th ta="right">Log end offset</Table.Th>
                  <Table.Th ta="right">Lag</Table.Th>
                  <Table.Th>Last caught up</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {observers.map((o) => (
                  <Table.Tr key={`${o.nodeId}-${o.directoryId}`}>
                    <Table.Td>
                      <Text fw={700}>{o.nodeId}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Tooltip
                        label={o.kind === 'controller' ? 'Controller that is not a voter yet' : 'Broker'}
                        withArrow
                      >
                        <Badge size="sm" color={o.kind === 'controller' ? 'grape' : 'gray'} variant="light">
                          {o.kind}
                        </Badge>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>
                      <CopyText value={o.directoryId} display={shortDirectoryId(o.directoryId)} />
                    </Table.Td>
                    <Table.Td ta="right">{formatNumber(o.logEndOffset)}</Table.Td>
                    <Table.Td ta="right">
                      <LagText lag={o.lag} />
                    </Table.Td>
                    <Table.Td>
                      <RelativeTime value={o.lastCaughtUpTimestamp} />
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Section>

      {!dynamic && (
        <Paper withBorder p="sm" radius="md">
          <Text size="sm" c="dimmed">
            This cluster uses a static quorum (kraft.version 0): controllers cannot be added or removed until the quorum
            is migrated to a dynamic one (Kafka 4.1+). The migration is irreversible.
          </Text>
        </Paper>
      )}
    </Stack>
  );
}
